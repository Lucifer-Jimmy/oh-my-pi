package gui

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/markdown"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
	"github.com/Lucifer-Jimmy/oh-my-pi/presentation"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

//go:embed web
var web embed.FS

const (
	cookieName       = "omp_gui"
	maxProjectedText = 16 << 10
	maxProjectedArg  = 8 << 10
)

type Runner = agent.Runner

type CompactFunc func(ctx context.Context, sess session.Session, instructions string) iter.Seq2[agent.Event, error]

type ModeControl interface {
	Mode() permission.Mode
	SetMode(permission.Mode)
}

type Options struct {
	Runner       Runner
	Compact      CompactFunc
	Session      session.Session
	NewSession   func() (session.Session, error)
	Resume       func(arg string) (session.Session, string, error)
	ListSessions func() ([]session.Info, error)
	SwitchModel  func(selector string) (string, error)
	Host         *Host
	Policy       ModeControl
	Window       func() int
	Steer        func(text string)
	Model        string
	CWD          string
	Prompts      []string
	Projectors   map[string]presentation.ToolProjector
	Note         string
	Addr         string
	Open         func(url string) error
	Out          io.Writer
}

type server struct {
	ctx    context.Context
	opts   Options
	cancel context.CancelFunc

	mu    sync.Mutex
	state presentation.State
	sess  session.Session
	model string
	cwd   string
	subs  map[chan []byte]struct{}
	run   *activeRun
	sent  int

	host  *Host
	token string
	addr  string
}

type activeRun struct {
	cancel context.CancelFunc
}

func Run(ctx context.Context, opts Options) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s, err := newServer(ctx, opts)
	if err != nil {
		return err
	}
	s.cancel = cancel
	addr := opts.Addr
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("starting the gui server: %w", err)
	}
	s.addr = ln.Addr().String()
	url := s.url()
	fmt.Fprintln(s.out(), "omp gui: "+url)
	go func() {
		if err := s.open(url); err != nil {
			fmt.Fprintln(s.out(), "omp gui: open "+url+" in a browser:", err)
		}
	}()
	srv := &http.Server{Handler: s.handler(), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	s.begin()
	select {
	case <-ctx.Done():
		shutCtx, shutdown := context.WithTimeout(context.Background(), 3*time.Second)
		defer shutdown()
		_ = srv.Shutdown(shutCtx)
		<-errc
		return nil
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func newServer(ctx context.Context, opts Options) (*server, error) {
	if opts.Runner == nil {
		return nil, errors.New("the gui needs a runner")
	}
	if opts.Session == nil {
		opts.Session = session.NewMemory(nil)
	}
	token, err := newToken()
	if err != nil {
		return nil, err
	}
	s := &server{
		ctx:   ctx,
		opts:  opts,
		sess:  opts.Session,
		model: opts.Model,
		cwd:   opts.CWD,
		subs:  make(map[chan []byte]struct{}),
		host:  opts.Host,
		token: token,
	}
	s.state.Projectors = opts.Projectors
	if s.host != nil {
		s.host.SetSender(s.sendAsk)
	}
	return s, nil
}

func (s *server) url() string {
	return "http://" + s.addr + "/?token=" + s.token
}

func (s *server) open(url string) error {
	if s.opts.Open != nil {
		return s.opts.Open(url)
	}
	return openBrowser(url)
}

func (s *server) out() io.Writer {
	if s.opts.Out != nil {
		return s.opts.Out
	}
	return os.Stderr
}

func (s *server) begin() {
	if s.opts.Note != "" {
		s.notice(presentation.LevelInfo, s.opts.Note)
	}
	if len(s.opts.Prompts) == 0 {
		return
	}
	if err := s.prompt(strings.Join(s.opts.Prompts, "\n\n")); err != nil {
		s.notice(presentation.LevelWarn, err.Error())
	}
}

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("GET /app.js", s.asset("app.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /style.css", s.asset("style.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET /api/events", s.events)
	mux.HandleFunc("POST /api/command", s.command)
	return s.guard(mux)
}

func (s *server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopback(r.Host) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if s.exchange(w, r) {
			return
		}
		if !s.authorized(r) {
			http.Error(w, "Open the URL that omp printed in the terminal.", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) exchange(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet || r.URL.Path != "/" || !r.URL.Query().Has("token") {
		return false
	}
	if !s.tokenOK(r.URL.Query().Get("token")) {
		http.Error(w, "bad token", http.StatusUnauthorized)
		return true
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    s.token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/", http.StatusSeeOther)
	return true
}

func (s *server) authorized(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	return err == nil && s.tokenOK(c.Value)
}

func (s *server) tokenOK(v string) bool {
	return subtle.ConstantTimeCompare([]byte(v), []byte(s.token)) == 1
}

func (s *server) index(w http.ResponseWriter, r *http.Request) {
	body, err := web.ReadFile("web/index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	_, _ = w.Write(body)
}

func (s *server) asset(name, contentType string) http.HandlerFunc {
	body, err := web.ReadFile("web/" + name)
	return func(w http.ResponseWriter, r *http.Request) {
		if err != nil {
			http.NotFound(w, r)
			return
		}
		h := w.Header()
		h.Set("Content-Type", contentType)
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(body)
	}
}

func (s *server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is not supported", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	h.Set("X-Content-Type-Options", "nosniff")

	ch := make(chan []byte, 256)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	for _, f := range []frame{{Type: "ready"}, s.snapshotLocked()} {
		if msg := encode(f); msg != nil {
			ch <- msg
		}
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if _, live := s.subs[ch]; live {
			delete(s.subs, ch)
			close(ch)
		}
		s.mu.Unlock()
	}()
	flusher.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.ctx.Done():
			return
		case msg, open := <-ch:
			if !open {
				return
			}
			if _, err := w.Write(msg); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *server) command(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
		http.Error(w, "cross-origin command refused", http.StatusForbidden)
		return
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		http.Error(w, "content type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var c command
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeReply(w, http.StatusBadRequest, reply{Error: "bad command: " + err.Error()})
		return
	}
	writeReply(w, http.StatusOK, s.execute(c))
}

func writeReply(w http.ResponseWriter, status int, rep reply) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(rep)
}

func (s *server) execute(c command) reply {
	var err error
	switch c.Type {
	case "prompt":
		err = s.prompt(c.Text)
	case "steer":
		err = s.steer(c.Text)
	case "abort":
		err = s.abort()
	case "answer":
		if !s.answer(c) {
			return reply{Error: "that approval is no longer open"}
		}
	case "compact":
		err = s.compact(c.Text)
	case "new_session":
		err = s.newSession()
	case "resume":
		err = s.resume(c.Text)
	case "list_sessions":
		infos, lerr := s.listSessions()
		if lerr != nil {
			return reply{Error: lerr.Error()}
		}
		return reply{OK: true, Result: infos}
	case "set_model":
		err = s.setModel(c.Text)
	case "set_mode":
		err = s.setMode(c.Mode)
	case "quit":
		if s.cancel != nil {
			s.cancel()
		}
		return reply{OK: true}
	default:
		return reply{Error: "unknown command " + c.Type}
	}
	if err != nil {
		return reply{Error: err.Error()}
	}
	return reply{OK: true}
}

func (s *server) prompt(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("the prompt is empty")
	}
	s.mu.Lock()
	if s.run != nil {
		s.mu.Unlock()
		return errors.New("a turn is running; steer it or stop it first")
	}
	s.state.Submit(text, s.now())
	sess := s.sess
	ctx, cancel := context.WithCancel(s.ctx)
	s.run = &activeRun{cancel: cancel}
	s.broadcastLocked()
	s.mu.Unlock()
	go s.drive(ctx, func(ctx context.Context) iter.Seq2[agent.Event, error] {
		return s.opts.Runner.Run(ctx, sess, text)
	})
	return nil
}

func (s *server) steer(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("the message is empty")
	}
	if s.opts.Steer == nil {
		return errors.New("this session cannot steer a running turn")
	}
	if !s.busy() {
		return errors.New("no turn is running")
	}
	s.opts.Steer(text)
	return nil
}

func (s *server) abort() error {
	s.mu.Lock()
	run := s.run
	s.mu.Unlock()
	if run == nil {
		return errors.New("no turn is running")
	}
	run.cancel()
	return nil
}

func (s *server) answer(c command) bool {
	if s.host == nil {
		return false
	}
	if !s.host.Answer(c.ID, agent.AskResponse{Value: c.Value, Confirmed: c.Confirmed, Cancelled: c.Cancelled}) {
		return false
	}
	s.mu.Lock()
	s.broadcastLocked()
	s.mu.Unlock()
	return true
}

func (s *server) compact(focus string) error {
	if s.opts.Compact == nil {
		return errors.New("compaction is not available in this session")
	}
	s.mu.Lock()
	if s.run != nil {
		s.mu.Unlock()
		return errors.New("a turn is running; steer it or stop it first")
	}
	sess := s.sess
	ctx, cancel := context.WithCancel(s.ctx)
	s.run = &activeRun{cancel: cancel}
	s.mu.Unlock()
	go s.drive(ctx, func(ctx context.Context) iter.Seq2[agent.Event, error] {
		return s.opts.Compact(ctx, sess, focus)
	})
	return nil
}

func (s *server) newSession() error {
	if s.busy() {
		return errors.New("a turn is running; steer it or stop it first")
	}
	if s.opts.NewSession == nil {
		return errors.New("this session cannot start a new conversation")
	}
	next, err := s.opts.NewSession()
	if err != nil {
		return err
	}
	s.switchSession(next, "Started a new conversation")
	return nil
}

func (s *server) resume(arg string) error {
	if s.busy() {
		return errors.New("a turn is running; steer it or stop it first")
	}
	if s.opts.Resume == nil {
		return errors.New("no session store in this session")
	}
	next, id, err := s.opts.Resume(strings.TrimSpace(arg))
	if err != nil {
		return err
	}
	note := "Resumed session " + id
	if n := len(next.Messages()); n > 0 {
		note += fmt.Sprintf(" with %d messages", n)
	}
	s.switchSession(next, note)
	return nil
}

func (s *server) switchSession(next session.Session, note string) {
	s.mu.Lock()
	s.sess = next
	s.state.Reset()
	s.sent = 0
	s.state.Notice(presentation.LevelInfo, note, s.now())
	s.broadcastLocked()
	s.mu.Unlock()
}

func (s *server) listSessions() ([]session.Info, error) {
	if s.opts.ListSessions == nil {
		return nil, errors.New("no session store in this session")
	}
	return s.opts.ListSessions()
}

func (s *server) setModel(selector string) error {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return errors.New("the model selector is empty")
	}
	if s.busy() {
		return errors.New("a turn is running; steer it or stop it first")
	}
	if s.opts.SwitchModel == nil {
		return errors.New("this session cannot switch models")
	}
	label, err := s.opts.SwitchModel(selector)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.model = label
	s.broadcastLocked()
	s.mu.Unlock()
	s.notice(presentation.LevelInfo, "Model: "+label)
	return nil
}

func (s *server) setMode(name string) error {
	if s.opts.Policy == nil {
		return errors.New("this session has no approval policy")
	}
	m, err := permission.ParseMode(name)
	if err != nil {
		return err
	}
	s.opts.Policy.SetMode(m)
	s.mu.Lock()
	s.broadcastLocked()
	s.mu.Unlock()
	return nil
}

func (s *server) drive(ctx context.Context, seq func(context.Context) iter.Seq2[agent.Event, error]) {
	defer s.endRun()
	for ev, err := range seq(ctx) {
		if err != nil {
			s.notice(presentation.LevelError, err.Error())
			return
		}
		s.apply(ev)
	}
}

func (s *server) apply(ev agent.Event) {
	s.mu.Lock()
	s.state.Apply(ev)
	s.broadcastLocked()
	s.mu.Unlock()
}

func (s *server) endRun() {
	s.mu.Lock()
	run := s.run
	s.run = nil
	s.mu.Unlock()
	if run != nil {
		run.cancel()
	}
	if s.host != nil {
		s.host.CancelAll()
	}
	s.mu.Lock()
	if s.state.Running {
		s.state.Apply(agent.Event{Kind: agent.EventResult, Timestamp: s.now(), Result: &agent.Result{
			Reason: agent.ReasonError, Err: "the run ended without a result",
		}})
	}
	s.broadcastLocked()
	s.mu.Unlock()
}

func (s *server) notice(level presentation.Level, text string) {
	s.mu.Lock()
	s.state.Notice(level, text, s.now())
	s.broadcastLocked()
	s.mu.Unlock()
}

func (s *server) busy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.run != nil
}

func (s *server) sendAsk(f frame) error {
	s.mu.Lock()
	s.publishLocked(f)
	s.mu.Unlock()
	return nil
}

func (s *server) broadcastLocked() {
	st := s.stateFrameAtLocked(s.sent)
	s.sent = s.firstOpenLocked()
	s.publishLocked(frame{Type: "state", State: &st})
}

func (s *server) snapshotLocked() frame {
	st := s.stateFrameAtLocked(0)
	return frame{Type: "state", State: &st}
}

func (s *server) firstOpenLocked() int {
	for i := range s.state.Items {
		if !s.state.Items[i].Done {
			return i
		}
	}
	return len(s.state.Items)
}

func (s *server) stateFrameAtLocked(from int) stateFrame {
	items := s.state.Items
	if from > len(items) {
		from = len(items)
	}
	if from < 0 {
		from = 0
	}
	out := make([]item, 0, len(items)-from)
	for _, it := range items[from:] {
		out = append(out, projectItem(it))
	}
	st := stateFrame{
		From:    from,
		Items:   out,
		Running: s.state.Running,
		Usage:   s.state.Usage,
		Context: s.state.Context,
		Reason:  s.state.Reason,
		Todos:   append([]presentation.TodoItem(nil), s.state.Todos...),
		Model:   s.model,
		CWD:     s.cwd,
	}
	if s.opts.Policy != nil {
		st.Mode = string(s.opts.Policy.Mode())
	}
	if s.opts.Window != nil {
		st.Window = s.opts.Window()
	}
	if s.host != nil {
		st.Asks = s.host.Pending()
	}
	return st
}

func (s *server) publishLocked(f frame) {
	msg := encode(f)
	if msg == nil {
		return
	}
	for ch := range s.subs {
		select {
		case ch <- msg:
		default:
			delete(s.subs, ch)
			close(ch)
		}
	}
}

func encode(f frame) []byte {
	f.V = version
	b, err := json.Marshal(f)
	if err != nil {
		return nil
	}
	out := make([]byte, 0, len(b)+8)
	out = append(out, "data: "...)
	out = append(out, b...)
	return append(out, '\n', '\n')
}

func projectItem(it presentation.Item) item {
	out := item{Item: it}
	if it.Kind == presentation.KindText && it.Text != "" {
		out.HTML = markdown.HTML(it.Text)
	}
	if it.Tool != nil {
		t := *it.Tool
		t.Output = clip(t.Output)
		t.Input = clipInput(t.Input)
		out.Tool = &t
	}
	return out
}

func clip(s string) string {
	if len(s) <= maxProjectedText {
		return s
	}
	cut := runeCut(s, maxProjectedText)
	return s[:cut] + fmt.Sprintf("\n… %d bytes truncated", len(s)-cut)
}

func clipInput(raw json.RawMessage) json.RawMessage {
	if len(raw) <= maxProjectedText {
		return raw
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return json.RawMessage(`{"truncated":true}`)
	}
	for k, v := range obj {
		str, ok := v.(string)
		if ok && len(str) > maxProjectedArg {
			obj[k] = str[:runeCut(str, maxProjectedArg)] + "\n… truncated"
		}
	}
	out, err := json.Marshal(obj)
	if err != nil || len(out) > 4*maxProjectedText {
		return json.RawMessage(`{"truncated":true}`)
	}
	return out
}

func runeCut(s string, n int) int {
	if n >= len(s) {
		return len(s)
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return n
}

func loopback(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	switch strings.ToLower(strings.Trim(host, "[]")) {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating the gui token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *server) now() int64 { return time.Now().UnixMilli() }
