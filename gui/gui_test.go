package gui

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"go/build"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
	"github.com/Lucifer-Jimmy/oh-my-pi/presentation"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

func scripted(events ...agent.Event) agent.RunnerFunc {
	return func(ctx context.Context, sess session.Session, input string) iter.Seq2[agent.Event, error] {
		return func(yield func(agent.Event, error) bool) {
			for _, ev := range events {
				if !yield(ev, nil) {
					return
				}
			}
		}
	}
}

type harness struct {
	t      *testing.T
	ctx    context.Context
	s      *server
	ts     *httptest.Server
	cancel context.CancelFunc
}

func newHarness(t *testing.T, opts Options) *harness {
	t.Helper()
	if opts.Host == nil {
		opts.Host = NewHost()
	}
	if opts.Session == nil {
		opts.Session = session.NewMemory(nil)
	}
	if opts.Window == nil {
		opts.Window = func() int { return 200000 }
	}
	if opts.CWD == "" {
		opts.CWD = t.TempDir()
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s, err := newServer(ctx, opts)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	s.cancel = cancel
	h := &harness{t: t, ctx: ctx, s: s, cancel: cancel}
	h.ts = httptest.NewServer(s.handler())
	t.Cleanup(h.ts.Close)
	return h
}

func (h *harness) do(req *http.Request) *http.Response {
	h.t.Helper()
	resp, err := h.ts.Client().Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}
	return resp
}

func (h *harness) authed(req *http.Request) *http.Request {
	req.AddCookie(&http.Cookie{Name: cookieName, Value: h.s.token})
	return req
}

func (h *harness) stream() *stream {
	h.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, h.ts.URL+"/api/events", nil)
	h.authed(req)
	resp := h.do(req)
	if resp.StatusCode != http.StatusOK {
		h.t.Fatalf("events status %d", resp.StatusCode)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	h.t.Cleanup(func() { resp.Body.Close() })
	return &stream{t: h.t, sc: sc}
}

func (h *harness) command(c command) reply {
	h.t.Helper()
	rep, _ := decodeReply(h.t, h.post(c))
	return rep
}

func (h *harness) post(c command) []byte {
	h.t.Helper()
	body, err := json.Marshal(c)
	if err != nil {
		h.t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, h.ts.URL+"/api/command", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.authed(req)
	resp := h.do(req)
	defer resp.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		h.t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		return []byte(`{"ok":false,"error":"status ` + http.StatusText(resp.StatusCode) + `"}`)
	}
	return buf.Bytes()
}

func decodeReply(t *testing.T, raw []byte) (reply, map[string]any) {
	t.Helper()
	var rep reply
	var all map[string]any
	if err := json.Unmarshal(raw, &all); err != nil {
		t.Fatalf("decoding a reply: %v (%s)", err, raw)
	}
	rep.OK, _ = all["ok"].(bool)
	rep.Error, _ = all["error"].(string)
	return rep, all
}

func commandAs[T any](t *testing.T, h *harness, c command) (T, reply) {
	t.Helper()
	var out struct {
		OK     bool   `json:"ok"`
		Error  string `json:"error"`
		Result T      `json:"result"`
	}
	raw := h.post(c)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decoding a reply: %v (%s)", err, raw)
	}
	return out.Result, reply{OK: out.OK, Error: out.Error}
}

type stream struct {
	t  *testing.T
	sc *bufio.Scanner
}

type readResult struct {
	f   frame
	err error
}

func (st *stream) next() frame {
	st.t.Helper()
	ch := make(chan readResult, 1)
	go func() {
		for st.sc.Scan() {
			line := st.sc.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var f frame
			err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &f)
			ch <- readResult{f: f, err: err}
			return
		}
		ch <- readResult{err: errors.New("stream closed")}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			st.t.Fatalf("reading a frame: %v", r.err)
		}
		return r.f
	case <-time.After(5 * time.Second):
		st.t.Fatal("timed out waiting for a frame")
		return frame{}
	}
}

func (st *stream) waitState(what string, pred func(stateFrame) bool) stateFrame {
	st.t.Helper()
	for i := 0; i < 500; i++ {
		f := st.next()
		if f.Type == "state" && f.State != nil && pred(*f.State) {
			return *f.State
		}
	}
	st.t.Fatalf("no state frame matching %s", what)
	return stateFrame{}
}

func mergeItems(items []item, st stateFrame) []item {
	if st.From > len(items) || st.From < 0 {
		return append([]item(nil), st.Items...)
	}
	out := append([]item(nil), items[:st.From]...)
	return append(out, st.Items...)
}

func (st *stream) collectRun(items []item) (stateFrame, []item) {
	st.t.Helper()
	for i := 0; i < 500; i++ {
		f := st.next()
		if f.Type == "error" {
			st.t.Fatalf("server error frame: %s", f.Error)
		}
		if f.Type != "state" || f.State == nil {
			continue
		}
		items = mergeItems(items, *f.State)
		if !f.State.Running && f.State.Reason != "" {
			return *f.State, items
		}
	}
	st.t.Fatal("the run did not finish")
	return stateFrame{}, nil
}

func findItem(items []item, kind presentation.Kind) *item {
	for i := range items {
		if items[i].Kind == kind {
			return &items[i]
		}
	}
	return nil
}

func TestGuards(t *testing.T) {
	h := newHarness(t, Options{Runner: scripted()})

	resp := h.do(mustRequest(t, http.MethodGet, h.ts.URL+"/", nil))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no cookie: status %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()

	resp = h.do(mustRequest(t, http.MethodGet, h.ts.URL+"/?token=wrong", nil))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("bad token: status %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()

	client := h.ts.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Get(h.ts.URL + "/?token=" + h.s.token)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("token exchange: status %d, want 303", resp.StatusCode)
	}
	cookie := resp.Header.Get("Set-Cookie")
	if !strings.Contains(cookie, cookieName+"=") || !strings.Contains(cookie, "HttpOnly") || !strings.Contains(cookie, "SameSite=Strict") {
		t.Errorf("cookie = %q, want %s with HttpOnly and SameSite=Strict", cookie, cookieName)
	}

	resp = h.do(h.authed(mustRequest(t, http.MethodGet, h.ts.URL+"/", nil)))
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("index: status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("index content type = %q", ct)
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "script-src 'self'") {
		t.Errorf("CSP = %q", csp)
	}
	if !strings.Contains(body, "app.js") {
		t.Errorf("index does not load app.js")
	}

	resp = h.do(h.authed(mustRequest(t, http.MethodGet, h.ts.URL+"/app.js", nil)))
	readBody(t, resp)
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Errorf("app.js content type = %q", ct)
	}

	req := h.authed(mustRequest(t, http.MethodPost, h.ts.URL+"/api/command", strings.NewReader(`{"type":"quit"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://evil.example")
	resp = h.do(req)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("foreign origin: status %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()

	req = h.authed(mustRequest(t, http.MethodPost, h.ts.URL+"/api/command", strings.NewReader(`{"type":"quit"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "evil.example"
	resp = h.do(req)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("foreign host: status %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()

	req = h.authed(mustRequest(t, http.MethodPost, h.ts.URL+"/api/command", strings.NewReader(`{"type":"quit"}`)))
	req.Header.Set("Content-Type", "text/plain")
	resp = h.do(req)
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("plain content type: status %d, want 415", resp.StatusCode)
	}
	resp.Body.Close()
}

func mustRequest(t *testing.T, method, url string, body *strings.Reader) *http.Request {
	t.Helper()
	var req *http.Request
	var err error
	if body == nil {
		req, err = http.NewRequest(method, url, nil)
	} else {
		req, err = http.NewRequest(method, url, body)
	}
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestPromptStreamsStateDeltas(t *testing.T) {
	input := json.RawMessage(`{"path":"a.txt"}`)
	h := newHarness(t, Options{Runner: scripted(
		agent.Event{Kind: agent.EventTextDelta, Text: "hel"},
		agent.Event{Kind: agent.EventTextDelta, Text: "lo"},
		agent.Event{Kind: agent.EventToolUse, ToolUseID: "t1", ToolName: "read", ToolInput: input},
		agent.Event{Kind: agent.EventToolResult, ToolUseID: "t1", ToolResult: &agent.ToolResult{Content: []llm.ContentBlock{llm.TextBlock("1\tline")}}},
		agent.Event{Kind: agent.EventUsage, Usage: &llm.Usage{InputTokens: 100, OutputTokens: 20}},
		agent.Event{Kind: agent.EventResult, Result: &agent.Result{Reason: agent.ReasonCompleted}},
	)})
	st := h.stream()
	if f := st.next(); f.Type != "ready" {
		t.Fatalf("first frame = %q, want ready", f.Type)
	}
	if f := st.next(); f.Type != "state" || f.State == nil || f.State.From != 0 || len(f.State.Items) != 0 {
		t.Fatalf("initial snapshot = %+v", f)
	}

	if rep := h.command(command{Type: "prompt", Text: "hi"}); !rep.OK {
		t.Fatalf("prompt refused: %s", rep.Error)
	}
	final, items := st.collectRun(nil)
	if final.Reason != agent.ReasonCompleted || final.Running {
		t.Errorf("final state = %+v", final)
	}
	if final.Usage.InputTokens != 100 || final.Usage.OutputTokens != 20 {
		t.Errorf("usage = %+v", final.Usage)
	}
	if final.Context != 120 {
		t.Errorf("context = %d, want 120", final.Context)
	}
	user := findItem(items, presentation.KindUser)
	if user == nil || user.Text != "hi" {
		t.Fatalf("user item = %+v", user)
	}
	text := findItem(items, presentation.KindText)
	if text == nil || text.Text != "hello" || !text.Done {
		t.Fatalf("text item = %+v", text)
	}
	if !strings.Contains(text.HTML, "hello") {
		t.Errorf("text item HTML = %q", text.HTML)
	}
	tool := findItem(items, presentation.KindTool)
	if tool == nil || tool.Tool == nil || tool.Tool.Status != presentation.StatusDone || tool.Tool.Output != "1\tline" {
		t.Fatalf("tool item = %+v", tool)
	}
}

type fakeEdit struct{ calls int }

func (f *fakeEdit) Name() string        { return "edit" }
func (f *fakeEdit) Description() string { return "fake edit" }
func (f *fakeEdit) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"old_string":{"type":"string"},"new_string":{"type":"string"}},"required":["path","old_string","new_string"],"additionalProperties":false}`)
}
func (f *fakeEdit) IsReadOnly() bool                       { return false }
func (f *fakeEdit) IsConcurrencySafe(json.RawMessage) bool { return false }
func (f *fakeEdit) Approval(json.RawMessage) agent.Approval {
	return agent.Approval{Tier: agent.TierWrite}
}
func (f *fakeEdit) Metadata(json.RawMessage) agent.Metadata {
	return agent.Metadata{MaxResultSize: 4096}
}
func (f *fakeEdit) Execute(context.Context, agent.ToolContext, json.RawMessage) agent.ToolResult {
	f.calls++
	return agent.ToolResult{Content: []llm.ContentBlock{llm.TextBlock("edited a.txt")}}
}

type modelTurn struct {
	tool  string
	id    string
	input map[string]any
	text  string
}

type scriptModel struct {
	turns []modelTurn
	calls int
}

func (m *scriptModel) call(_ context.Context, _ llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		if m.calls >= len(m.turns) {
			yield(llm.StreamEvent{}, errors.New("script exhausted"))
			return
		}
		turn := m.turns[m.calls]
		m.calls++
		events := []llm.StreamEvent{{Type: llm.SEMessageStart}}
		if turn.tool != "" {
			raw, _ := json.Marshal(turn.input)
			events = append(events,
				llm.StreamEvent{Type: llm.SEBlockStart, Block: llm.ContentBlock{Type: llm.BlockToolUse, ID: turn.id, Name: turn.tool, Input: raw}},
				llm.StreamEvent{Type: llm.SEBlockStop},
				llm.StreamEvent{Type: llm.SEMessageDelta, StopReason: llm.StopReasonToolUse},
			)
		} else {
			events = append(events,
				llm.StreamEvent{Type: llm.SEBlockStart, Block: llm.ContentBlock{Type: llm.BlockText}},
				llm.StreamEvent{Type: llm.SETextDelta, Text: turn.text},
				llm.StreamEvent{Type: llm.SEBlockStop},
				llm.StreamEvent{Type: llm.SEMessageDelta, StopReason: llm.StopReasonStop},
			)
		}
		events = append(events, llm.StreamEvent{Type: llm.SEMessageStop})
		for _, ev := range events {
			if !yield(ev, nil) {
				return
			}
		}
	}
}

func TestApprovalRoundTripThroughTheLoop(t *testing.T) {
	policy := permission.NewPolicy(permission.ModeDefault, permission.Rules{})
	tool := new(fakeEdit)
	reg := agent.NewRegistry()
	reg.Register(permission.Wrap(tool, policy))
	model := &scriptModel{turns: []modelTurn{
		{tool: "edit", id: "e1", input: map[string]any{"path": "a.txt", "old_string": "a", "new_string": "b"}},
		{text: "done"},
	}}
	host := NewHost()
	loop := agent.NewLoop(reg, agent.RunDeps{CallModel: model.call}, host, agent.Options{CWD: t.TempDir()})
	h := newHarness(t, Options{Runner: loop, Host: host, Policy: policy})
	st := h.stream()
	st.next()
	st.next()

	if rep := h.command(command{Type: "prompt", Text: "change it"}); !rep.OK {
		t.Fatalf("prompt refused: %s", rep.Error)
	}

	var ask *askFrame
	var items []item
	for i := 0; i < 100 && ask == nil; i++ {
		f := st.next()
		if f.Type == "state" && f.State != nil {
			items = mergeItems(items, *f.State)
		}
		if f.Type == "ask" {
			ask = f.Ask
		}
	}
	if ask == nil {
		t.Fatal("the approval never reached the browser")
	}
	if ask.Tool != "edit" {
		t.Errorf("ask tool = %q, want edit", ask.Tool)
	}
	if !strings.Contains(string(ask.Input), `"old_string":"a"`) {
		t.Errorf("ask input = %s", ask.Input)
	}
	if ask.HTML != "" {
		t.Errorf("only exit_plan_mode asks carry rendered HTML, got %q", ask.HTML)
	}
	if rep := h.command(command{Type: "answer", ID: ask.ID, Value: "Yes"}); !rep.OK {
		t.Fatalf("answer refused: %s", rep.Error)
	}
	final, items := st.collectRun(items)
	if final.Reason != agent.ReasonCompleted {
		t.Fatalf("final state = %+v", final)
	}
	if tool.calls != 1 {
		t.Errorf("tool ran %d times, want 1", tool.calls)
	}
	edit := findItem(items, presentation.KindTool)
	if edit == nil || edit.Tool.Status != presentation.StatusDone || !strings.Contains(edit.Tool.Output, "edited a.txt") {
		t.Fatalf("edit item = %+v", edit)
	}
	if text := findItem(items, presentation.KindText); text == nil || text.Text != "done" {
		t.Fatalf("final text = %+v", text)
	}
}

func TestAbortCancelsTheRun(t *testing.T) {
	h := newHarness(t, Options{Runner: agent.RunnerFunc(func(ctx context.Context, sess session.Session, input string) iter.Seq2[agent.Event, error] {
		return func(yield func(agent.Event, error) bool) {
			if !yield(agent.Event{Kind: agent.EventTextDelta, Text: "working"}, nil) {
				return
			}
			<-ctx.Done()
			yield(agent.Event{Kind: agent.EventResult, Result: &agent.Result{Reason: agent.ReasonAborted}}, nil)
		}
	})})
	st := h.stream()
	st.next()
	st.next()
	if rep := h.command(command{Type: "prompt", Text: "go"}); !rep.OK {
		t.Fatalf("prompt refused: %s", rep.Error)
	}
	st.waitState("running", func(s stateFrame) bool { return s.Running })
	if rep := h.command(command{Type: "abort"}); !rep.OK {
		t.Fatalf("abort refused: %s", rep.Error)
	}
	final, items := st.collectRun(nil)
	if final.Reason != agent.ReasonAborted {
		t.Errorf("reason = %q, want aborted", final.Reason)
	}
	notice := findItem(items, presentation.KindNotice)
	if notice == nil || notice.Level != presentation.LevelWarn || notice.Text != "Interrupted" {
		t.Errorf("abort notice = %+v", notice)
	}
}

func TestSessionModelAndModeCommands(t *testing.T) {
	policy := permission.NewPolicy(permission.ModeDefault, permission.Rules{})
	titled := session.NewMemory(nil)
	titled.Append(llm.UserText("earlier"))
	h := newHarness(t, Options{
		Runner: scripted(),
		Policy: policy,
		ListSessions: func() ([]session.Info, error) {
			return []session.Info{{ID: "abc123", Messages: 3, Title: "demo"}}, nil
		},
		Resume: func(arg string) (session.Session, string, error) {
			if arg != "abc123" {
				t.Errorf("resume arg = %q", arg)
			}
			return titled, "abc123", nil
		},
		NewSession: func() (session.Session, error) { return session.NewMemory(nil), nil },
		SwitchModel: func(selector string) (string, error) {
			if selector != "prov/m:high" {
				t.Errorf("model selector = %q", selector)
			}
			return "prov/m:high", nil
		},
	})

	infos, rep := commandAs[[]session.Info](t, h, command{Type: "list_sessions"})
	if !rep.OK || len(infos) != 1 || infos[0].ID != "abc123" || infos[0].Title != "demo" {
		t.Fatalf("list_sessions = %+v (%s)", infos, rep.Error)
	}

	st := h.stream()
	st.next()
	st.next()

	if rep := h.command(command{Type: "set_mode", Mode: "plan"}); !rep.OK {
		t.Fatalf("set_mode refused: %s", rep.Error)
	}
	if policy.Mode() != permission.ModePlan {
		t.Errorf("policy mode = %q, want plan", policy.Mode())
	}
	st.waitState("plan mode", func(s stateFrame) bool { return s.Mode == string(permission.ModePlan) })

	if rep := h.command(command{Type: "set_model", Text: "prov/m:high"}); !rep.OK {
		t.Fatalf("set_model refused: %s", rep.Error)
	}
	st.waitState("new model", func(s stateFrame) bool { return s.Model == "prov/m:high" })

	if rep := h.command(command{Type: "new_session"}); !rep.OK {
		t.Fatalf("new_session refused: %s", rep.Error)
	}
	reset := st.waitState("reset", func(s stateFrame) bool {
		return s.From == 0 && len(s.Items) == 1 && strings.Contains(s.Items[0].Text, "Started a new conversation")
	})
	if !strings.Contains(reset.Items[0].Text, "Started a new conversation") {
		t.Errorf("new session notice = %q", reset.Items[0].Text)
	}

	if rep := h.command(command{Type: "resume", Text: "abc123"}); !rep.OK {
		t.Fatalf("resume refused: %s", rep.Error)
	}
	resumed := st.waitState("resume", func(s stateFrame) bool {
		return s.From == 0 && len(s.Items) == 1 && strings.Contains(s.Items[0].Text, "Resumed session abc123")
	})
	if !strings.Contains(resumed.Items[0].Text, "1 messages") {
		t.Errorf("resume notice = %q", resumed.Items[0].Text)
	}

	if rep := h.command(command{Type: "compact"}); rep.OK {
		t.Error("compact without a compactor must fail")
	}
}

func TestDeltasNeverSkipFinishedItems(t *testing.T) {
	h := newHarness(t, Options{Runner: scripted()})
	ch := make(chan []byte, 64)
	h.s.mu.Lock()
	h.s.subs[ch] = struct{}{}
	h.s.mu.Unlock()

	take := func() []frame {
		var out []frame
		for {
			select {
			case msg := <-ch:
				var f frame
				raw := bytes.TrimSuffix(bytes.TrimPrefix(msg, []byte("data: ")), []byte("\n\n"))
				if err := json.Unmarshal(raw, &f); err != nil {
					t.Fatalf("decode: %v", err)
				}
				out = append(out, f)
			default:
				return out
			}
		}
	}

	h.s.apply(agent.Event{Kind: agent.EventToolUse, ToolUseID: "a", ToolName: "read"})
	frames := take()
	if len(frames) != 1 || frames[0].State.From != 0 || len(frames[0].State.Items) != 1 {
		t.Fatalf("first delta = %+v", frames)
	}

	h.s.apply(agent.Event{Kind: agent.EventToolUse, ToolUseID: "b", ToolName: "read"})
	frames = take()
	if len(frames) != 1 || frames[0].State.From != 0 || len(frames[0].State.Items) != 2 {
		t.Fatalf("second delta = %+v", frames)
	}

	h.s.apply(agent.Event{Kind: agent.EventToolResult, ToolUseID: "b", ToolResult: &agent.ToolResult{Content: []llm.ContentBlock{llm.TextBlock("b done")}}})
	frames = take()
	items := mergeItems(nil, *frames[0].State)
	if len(items) != 2 || items[1].Tool.Status != presentation.StatusDone {
		t.Fatalf("after b finished: %+v", items)
	}

	h.s.apply(agent.Event{Kind: agent.EventToolResult, ToolUseID: "a", ToolResult: &agent.ToolResult{Content: []llm.ContentBlock{llm.TextBlock("a done")}}})
	frames = take()
	if frames[0].State.From != 0 {
		t.Fatalf("the tail was still open, from = %d", frames[0].State.From)
	}

	h.s.notice(presentation.LevelInfo, "after everything finished")
	frames = take()
	if len(frames) != 1 || frames[0].State.From != 2 || len(frames[0].State.Items) != 1 {
		t.Fatalf("a finished item appended after a closed tail must still be sent: %+v", frames[0].State)
	}
	if frames[0].State.Items[0].Text != "after everything finished" {
		t.Errorf("notice = %q", frames[0].State.Items[0].Text)
	}

	snapshot := h.s.snapshotLocked()
	if snapshot.State.From != 0 || len(snapshot.State.Items) != 3 {
		t.Fatalf("snapshot = %+v", snapshot.State)
	}
	h.s.notice(presentation.LevelInfo, "again")
	frames = take()
	if frames[0].State.From != 3 {
		t.Errorf("a snapshot must not move the marker, from = %d, want 3", frames[0].State.From)
	}
}

func TestProjectionRendersHTMLAndClips(t *testing.T) {
	text := projectItem(presentation.Item{Kind: presentation.KindText, Text: "**hi** 中文"})
	if !strings.Contains(text.HTML, "<strong>hi</strong>") {
		t.Errorf("HTML = %q", text.HTML)
	}

	big := strings.Repeat("a", maxProjectedText+10)
	raw, err := json.Marshal(map[string]any{"path": "a.txt", "content": big})
	if err != nil {
		t.Fatal(err)
	}
	tool := projectItem(presentation.Item{Kind: presentation.KindTool, Tool: &presentation.Tool{
		Name: "write", Input: raw, Output: big, Status: presentation.StatusDone,
	}})
	if !strings.Contains(tool.Tool.Output, "bytes truncated") {
		t.Errorf("output was not clipped: %d bytes", len(tool.Tool.Output))
	}
	var args map[string]any
	if err := json.Unmarshal(tool.Tool.Input, &args); err != nil {
		t.Fatalf("clipped input is not JSON: %v", err)
	}
	content, _ := args["content"].(string)
	if len(content) >= len(big) || !strings.Contains(content, "truncated") {
		t.Errorf("input content was not clipped: %d bytes", len(content))
	}
	if args["path"] != "a.txt" {
		t.Errorf("clipping dropped path: %+v", args)
	}

	wide := strings.Repeat("中", maxProjectedText)
	clipped := clip(wide)
	if !utf8.ValidString(clipped) {
		t.Error("clipping split a rune")
	}
}

func TestSlowSubscriberIsDropped(t *testing.T) {
	h := newHarness(t, Options{Runner: scripted()})
	ch := make(chan []byte, 1)
	h.s.mu.Lock()
	h.s.subs[ch] = struct{}{}
	h.s.mu.Unlock()

	h.s.notice(presentation.LevelInfo, "one")
	h.s.notice(presentation.LevelInfo, "two")

	h.s.mu.Lock()
	_, live := h.s.subs[ch]
	h.s.mu.Unlock()
	if live {
		t.Error("a subscriber that cannot keep up must be dropped")
	}
	<-ch
	if _, open := <-ch; open {
		t.Error("the dropped subscriber was not closed")
	}
}

func TestQuitStopsTheServer(t *testing.T) {
	h := newHarness(t, Options{Runner: scripted()})
	if rep := h.command(command{Type: "quit"}); !rep.OK {
		t.Fatalf("quit refused: %s", rep.Error)
	}
	select {
	case <-h.ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("quit did not cancel the server context")
	}
}

func TestGUIImportsNoShellPackage(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range pkg.Imports {
		if strings.HasSuffix(imp, "/tui") || strings.HasSuffix(imp, "/cmd/omp") {
			t.Errorf("the gui must not import the shell: %s", imp)
		}
	}
}
