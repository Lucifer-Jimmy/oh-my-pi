package agent

import (
	"context"
	"iter"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

const truncatedToolMsg = "Tool call was not executed because the assistant hit its output token limit (stop_reason: length) " +
	"before the arguments could complete; the recorded arguments are truncated and unsafe to run. " +
	"Do NOT retry by re-emitting the same large payload — split the work into several smaller tool calls."

const defaultMaxTruncRetries = 3

const (
	emptyErrorText = "Tool failed with no output."
)

type RunDeps struct {
	CallModel func(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error]
	NewID     func() string
	Compactor Compactor
	Steer     func() []string
	Notify    func() []Notification
	// 记录消息和事件时刻用 留空则用 time.Now 测试可注入固定值
	Now func() time.Time
}

type Notification struct {
	ID   string
	Text string
}

type Options struct {
	System []string
	// System 里可缓存的前缀块数 0 表示不打缓存标记
	CacheBoundary int
	MaxTurns      int
	MaxTokens     int
	TokenBudget   int
	Thinking      llm.ThinkingLevel
	// 填 0 表示同时运行的工具数不设上限
	Concurrency     int
	CWD             string
	Workspaces      []string
	MaxTruncRetries int
	Hooks           Hooks
}

type Hooks struct {
	PreTool  PreToolCallHook
	PostTool PostToolCallHook
	Stop     StopHook
	Prompt   PromptInjectHook
}

type Runner interface {
	Run(ctx context.Context, sess session.Session, input string) iter.Seq2[Event, error]
}

type RunnerFunc func(ctx context.Context, sess session.Session, input string) iter.Seq2[Event, error]

func (f RunnerFunc) Run(ctx context.Context, sess session.Session, input string) iter.Seq2[Event, error] {
	return f(ctx, sess, input)
}

type Loop struct {
	reg  *Registry
	deps RunDeps
	host Host
	opts Options
}

func NewLoop(reg *Registry, deps RunDeps, host Host, opts Options) *Loop {
	return &Loop{reg: reg, deps: deps, host: host, opts: opts}
}

func (l *Loop) SetHost(h Host) { l.host = h }

func (l *Loop) Run(ctx context.Context, sess session.Session, userInput string) iter.Seq2[Event, error] {
	// 返回一个迭代器，调用方用 for range 逐个拿事件，事件由后台运行的 drive 通过 events 送来
	return eventSeq(func(events chan<- Event) { l.newRun(ctx, sess, events).drive(userInput) })
}

func eventSeq(produce func(chan<- Event)) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		events := make(chan Event)
		go func() {
			// 放在最前面，保证从任何一个 return 离开都会关闭 events，Run 里的接收循环才能结束
			defer close(events)
			produce(events)
		}()
		for ev := range events {
			if !yield(ev, nil) {
				for range events {
				}
				return
			}
		}
	}
}

// 事件统一从这里出去 顺序号在并发工具里也保证唯一
type emitter struct {
	ch  chan<- Event
	seq atomic.Int64
	now func() time.Time
}

func (e *emitter) send(ev Event) {
	ev.Seq = e.seq.Add(1)
	ev.Timestamp = e.now().UnixMilli()
	e.ch <- ev
}

func (l *Loop) clock() time.Time {
	if l.deps.Now != nil {
		return l.deps.Now()
	}
	return time.Now()
}

type run struct {
	loop         *Loop
	ctx          context.Context
	sess         session.Session
	em           *emitter
	sem          chan struct{}
	usage        llm.Usage
	turns        int
	truncRetries int
	compacting   bool
}

func (l *Loop) newRun(ctx context.Context, sess session.Session, events chan<- Event) *run {
	r := &run{
		loop:       l,
		ctx:        ctx,
		sess:       sess,
		em:         &emitter{ch: events, now: l.clock},
		compacting: l.deps.Compactor != nil,
	}
	if l.opts.Concurrency > 0 {
		r.sem = make(chan struct{}, l.opts.Concurrency)
	}
	return r
}

// 追加消息时补上时刻 调用方已经打过时间戳就保留
func (r *run) append(m llm.Message) {
	if m.Timestamp == 0 {
		m.Timestamp = r.loop.clock().UnixMilli()
	}
	r.sess.Append(m)
}

func (r *run) drive(userInput string) {
	r.append(llm.UserText(userInput))
	for {
		if r.halted() {
			return
		}
		r.feed()
		req := r.request()
		if r.compacting {
			if _, err := r.compact(&req, session.TriggerAuto, ""); err != nil {
				r.compacting = false
			}
		}
		if !r.turn(req) {
			return
		}
	}
}

func (r *run) halted() bool {
	if err := r.ctx.Err(); err != nil {
		r.finish(ReasonAborted, err.Error())
		return true
	}
	if budget := r.loop.opts.TokenBudget; budget > 0 && r.usage.Total() >= budget {
		r.finish(ReasonTokenBudget, "")
		return true
	}
	return false
}

func (r *run) feed() {
	r.steer()
	r.drainNotify()
	r.injectPrompt()
}

// turn 跑一轮：流式收消息并在参数块结束时派发工具，然后按调用序收结果。
// next 为 false 表示整个运行结束（已完成、已取消或已失败），调用方直接返回。
func (r *run) turn(req llm.CompletionRequest) (next bool) {
	turnCtx, cancelTurn := context.WithCancel(r.ctx)
	defer cancelTurn()

	sched := newScheduler(turnCtx, r.exec, r.loop.makeSafe(), r.sem)
	pushed := map[string]bool{}
	rejected := map[string]bool{}
	onTool := func(use llm.ContentBlock) {
		pushed[use.ID] = true
		// 参数不是合法 JSON 对象说明它在流里被截断了 不上路 交回模型拆小重发
		if !isJSONObject(use.Input) {
			rejected[use.ID] = true
			return
		}
		r.em.send(r.loop.callEvent(use))
		sched.Push(use)
	}

	acc, elapsed, streamErr := r.respond(turnCtx, req, onTool)
	// 取消有两种表现，流直接报错，或者 adapter 把停止原因标成 aborted 正常收尾
	if acc.StopReason == llm.StopReasonAborted || (streamErr != nil && r.ctx.Err() != nil) {
		cancelTurn()
		r.settle(sched)
		msg := string(llm.StopReasonAborted)
		if err := r.ctx.Err(); err != nil {
			msg = err.Error()
		}
		r.finish(ReasonAborted, msg)
		return false
	}
	if streamErr != nil {
		cancelTurn()
		r.settle(sched)
		r.finish(ReasonError, streamErr.Error())
		return false
	}

	asst := r.record(acc, elapsed)
	uses := asst.ToolUses()
	if len(uses) == 0 {
		sched.Close()
		return r.keepGoing()
	}

	truncated := acc.StopReason == llm.StopReasonLength || len(rejected) > 0
	if truncated {
		maxTrunc := r.loop.opts.MaxTruncRetries
		if maxTrunc <= 0 {
			maxTrunc = defaultMaxTruncRetries
		}
		r.truncRetries++
		if r.truncRetries > maxTrunc {
			r.settle(sched)
			r.finish(ReasonError, "length truncation retries exhausted")
			return false
		}
	} else {
		// 这轮没被截断就清零，只有连续截断才会触发次数上限
		r.truncRetries = 0
	}

	results, extras := r.settleTurn(sched, uses, pushed, rejected)
	r.append(llm.Message{Role: llm.RoleUser, Content: results})
	// 额外消息只能排在工具结果后面，因为模型接口要求工具结果紧跟在调用之后
	for _, m := range extras {
		r.append(m)
	}
	// 被截断的一轮不是完整的一步 交回模型重发，不算轮次
	if truncated {
		return true
	}

	r.turns++
	if limit := r.loop.opts.MaxTurns; limit > 0 && r.turns >= limit {
		r.finish(ReasonMaxTurns, "")
		return false
	}
	return true
}

func (r *run) record(acc *llm.Accumulator, elapsed time.Duration) llm.Message {
	turn := acc.Usage
	asst := acc.Message()
	recorded := turn
	asst.Usage = &recorded
	r.append(asst)
	r.usage.Add(turn)
	u := turn
	r.em.send(Event{Kind: EventUsage, Usage: &u, StopReason: acc.StopReason, DurationMs: elapsed.Milliseconds()})
	return asst
}

func (r *run) keepGoing() bool {
	if r.steer() > 0 || r.drainNotify() > 0 || r.stopHook() {
		return true
	}
	r.finish(ReasonCompleted, "")
	return false
}

func (r *run) drainNotify() int {
	if r.loop.deps.Notify == nil {
		return 0
	}
	n := 0
	for _, note := range r.loop.deps.Notify() {
		text := strings.TrimSpace(note.Text)
		if text == "" {
			continue
		}
		r.append(llm.UserText("<background-task id=\"" + note.ID + "\">\n" + text + "\n</background-task>"))
		r.em.send(Event{Kind: EventNotification, Message: "background task " + note.ID + ": " + firstLine(text)})
		n++
	}
	return n
}

func (r *run) steer() int {
	if r.loop.deps.Steer == nil {
		return 0
	}
	n := 0
	for _, text := range r.loop.deps.Steer() {
		if strings.TrimSpace(text) == "" {
			continue
		}
		r.append(llm.UserText(text))
		r.em.send(Event{Kind: EventNotification, Message: "steering: " + text})
		n++
	}
	return n
}

func (r *run) stopHook() bool {
	hook := r.loop.opts.Hooks.Stop
	if hook == nil {
		return false
	}
	d, err := hook.OnStop(context.Background())
	if err != nil {
		r.em.send(Event{Kind: EventNotification, Message: "stop hook failed: " + err.Error()})
		return false
	}
	if !d.Prevent {
		return false
	}
	msg := d.Reason
	if strings.TrimSpace(msg) == "" {
		msg = "The stop hook says the task is not finished yet. Continue."
	}
	r.append(llm.UserText(msg))
	r.em.send(Event{Kind: EventNotification, Message: "stop hook: " + firstLine(msg)})
	return true
}

func (r *run) injectPrompt() {
	hook := r.loop.opts.Hooks.Prompt
	if hook == nil {
		return
	}
	msgs, err := hook.OnPrompt(context.Background())
	for _, m := range msgs {
		r.append(m)
	}
	if err != nil {
		r.em.send(Event{Kind: EventNotification, Message: "prompt hook failed: " + err.Error()})
	}
}

func (r *run) finish(reason StopReason, errMsg string) {
	msgs := r.sess.Messages()
	r.em.send(Event{Kind: EventResult, Result: &Result{
		Reason: reason, Err: errMsg, Usage: r.usage, Turns: r.turns, Messages: msgs, Text: lastAssistantText(msgs),
	}})
}

func lastAssistantText(msgs []llm.Message) string {
	for _, m := range slices.Backward(msgs) {
		if m.Role == llm.RoleAssistant {
			return m.Text()
		}
	}
	return ""
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
