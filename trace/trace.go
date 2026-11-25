package trace

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

const Version = 1

type header struct {
	Type      string `json:"type"`
	Version   int    `json:"version"`
	ID        string `json:"id"`
	StartedAt int64  `json:"started_at"`
	CWD       string `json:"cwd,omitempty"`
	Model     string `json:"model,omitempty"`
	Session   string `json:"session,omitempty"`
	OMP       string `json:"omp,omitempty"`
}

type eventLine struct {
	Type  string      `json:"type"`
	Event agent.Event `json:"event"`
}

type toolStats struct {
	Calls   int   `json:"calls"`
	Errors  int   `json:"errors,omitempty"`
	TotalMs int64 `json:"total_ms"`
}

type summary struct {
	Type            string               `json:"type"`
	Reason          string               `json:"reason,omitempty"`
	Err             string               `json:"err,omitempty"`
	Turns           int                  `json:"turns"`
	Events          int                  `json:"events"`
	Usage           llm.Usage            `json:"usage"`
	Requests        int                  `json:"requests"`
	RequestMs       int64                `json:"request_ms"`
	Truncations     int                  `json:"truncations"`
	Compactions     int                  `json:"compactions"`
	Tools           map[string]toolStats `json:"tools,omitempty"`
	PeakContext     int                  `json:"peak_context_tokens"`
	ContextWindow   int                  `json:"context_window,omitempty"`
	PeakUtilization float64              `json:"peak_context_utilization,omitempty"`
	StartedAt       int64                `json:"started_at"`
	FinishedAt      int64                `json:"finished_at"`
}

type Recorder struct {
	mu      sync.Mutex
	f       *os.File
	w       *bufio.Writer
	err     error
	events  int
	start   time.Time
	now     func() time.Time
	window  func() int
	names   map[string]string
	summary summary
	closed  bool
}

type Meta struct {
	CWD     string
	Model   string
	Session string
	Window  func() int
}

func New(path string, meta Meta) (*Recorder, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	r := &Recorder{
		f:      f,
		w:      bufio.NewWriter(f),
		start:  time.Now(),
		now:    time.Now,
		window: meta.Window,
		names:  map[string]string{},
	}
	h := header{
		Type:      "header",
		Version:   Version,
		ID:        filepath.Base(path),
		StartedAt: r.start.UnixMilli(),
		CWD:       meta.CWD,
		Model:     meta.Model,
		Session:   meta.Session,
	}
	if err := r.writeLine(h); err != nil {
		_ = f.Close()
		return nil, err
	}
	r.summary.StartedAt = r.start.UnixMilli()
	r.summary.Tools = map[string]toolStats{}
	return r, nil
}

func (r *Recorder) Write(ev agent.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.err != nil {
		return
	}
	if err := r.writeLine(eventLine{Type: "event", Event: ev}); err != nil {
		return
	}
	r.events++
	r.fold(ev)
}

func (r *Recorder) fold(ev agent.Event) {
	s := &r.summary
	switch ev.Kind {
	case agent.EventToolUse:
		r.names[ev.ToolUseID] = ev.ToolName
	case agent.EventToolResult:
		name, ok := r.names[ev.ToolUseID]
		if !ok {
			return
		}
		delete(r.names, ev.ToolUseID)
		st := s.Tools[name]
		st.Calls++
		st.TotalMs += ev.DurationMs
		if ev.ToolResult != nil && ev.ToolResult.IsError {
			st.Errors++
		}
		s.Tools[name] = st
	case agent.EventUsage:
		s.Requests++
		s.RequestMs += ev.DurationMs
		if ev.StopReason == llm.StopReasonLength {
			s.Truncations++
		}
		if ev.Usage != nil {
			r.observeContext(ev.Usage.Total())
		}
	case agent.EventCompaction:
		if ev.Compaction != nil {
			s.Compactions++
		}
	case agent.EventResult:
		if ev.Result != nil {
			s.Reason = string(ev.Result.Reason)
			s.Err = ev.Result.Err
			s.Turns += ev.Result.Turns
			s.Usage.Add(ev.Result.Usage)
		}
	}
}

func (r *Recorder) observeContext(tokens int) {
	s := &r.summary
	if tokens <= s.PeakContext {
		return
	}
	s.PeakContext = tokens
	if r.window == nil {
		return
	}
	if w := r.window(); w > 0 {
		s.ContextWindow = w
		s.PeakUtilization = math.Round(float64(tokens)/float64(w)*10000) / 10000
	}
}

func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return r.err
	}
	r.closed = true
	s := r.summary
	s.Type = "summary"
	s.Events = r.events
	s.FinishedAt = r.now().UnixMilli()
	if s.StartedAt == 0 {
		s.StartedAt = r.start.UnixMilli()
	}
	if r.err == nil {
		_ = r.writeLine(s)
	}
	if r.err == nil {
		r.err = r.w.Flush()
	}
	if cerr := r.f.Close(); r.err == nil {
		r.err = cerr
	}
	return r.err
}

func (r *Recorder) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

func (r *Recorder) writeLine(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		if r.err == nil {
			r.err = err
		}
		return err
	}
	if _, err := r.w.Write(append(b, '\n')); err != nil {
		if r.err == nil {
			r.err = err
		}
		return err
	}
	return nil
}
