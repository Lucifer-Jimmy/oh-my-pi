package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

func (r *run) exec(ctx context.Context, use llm.ContentBlock) ToolResult {
	t, ok := r.loop.reg.Get(use.Name)
	if !ok {
		return ToolResult{
			Content: []llm.ContentBlock{llm.TextBlock(r.loop.reg.NotFound(use.Name))},
			IsError: true,
		}
	}
	input, blocked := r.preTool(ctx, use)
	if blocked != nil {
		return *blocked
	}
	use.Input = input
	tc := &toolCtx{useID: use.ID, cwd: r.loop.opts.CWD, workspaces: r.loop.opts.Workspaces, em: r.em, host: r.loop.host, sess: r.sess}
	res := runTool(ctx, t, tc, use)
	res = clampResult(t, use.Input, res)
	if rewritten, ok := r.postTool(ctx, use, res); ok {
		res = rewritten
	}
	if res.IsError && !hasSubstance(res.Content) {
		res.Content = []llm.ContentBlock{llm.TextBlock(emptyErrorText)}
	}
	// 只返回工具产出的内层内容，套成工具结果块的动作统一在 drive 里做
	return res
}

func (r *run) preTool(ctx context.Context, use llm.ContentBlock) (json.RawMessage, *ToolResult) {
	hook := r.loop.opts.Hooks.PreTool
	if hook == nil {
		return use.Input, nil
	}
	d, err := hook.BeforeToolCall(ctx, use.Name, use.Input)
	if err != nil {
		r.em.send(Event{Kind: EventNotification, Message: "pre-tool hook failed: " + err.Error()})
		return use.Input, nil
	}
	if d.Block {
		reason := d.Reason
		if strings.TrimSpace(reason) == "" {
			reason = "blocked by a pre-tool hook"
		}
		return use.Input, &ToolResult{Content: []llm.ContentBlock{llm.TextBlock(reason)}, IsError: true}
	}
	if len(d.NewInput) > 0 && isJSONObject(d.NewInput) {
		return d.NewInput, nil
	}
	return use.Input, nil
}

func (r *run) postTool(ctx context.Context, use llm.ContentBlock, res ToolResult) (ToolResult, bool) {
	hook := r.loop.opts.Hooks.PostTool
	if hook == nil {
		return res, false
	}
	next, err := hook.AfterToolCall(ctx, use.Name, res)
	if err != nil || next == nil {
		return res, false
	}
	return *next, true
}

// settle 收掉还在跑的工具并把已完成的结果送出，不追加会话，用于中途退出
func (r *run) settle(sched *scheduler) {
	sched.Close()
	for d := range sched.Collect() {
		res := d.Result
		r.em.send(Event{Kind: EventToolResult, ToolUseID: d.Use.ID, ToolResult: &res, DurationMs: d.Elapsed.Milliseconds()})
	}
}

// settleTurn 收齐本轮结果并按调用序组装，缺结果的调用（参数没送达就截断了）补一条拒绝执行
func (r *run) settleTurn(sched *scheduler, uses []llm.ContentBlock, pushed, rejected map[string]bool) ([]llm.ContentBlock, []llm.Message) {
	sched.Close()
	done := map[string]Done{}
	for d := range sched.Collect() {
		done[d.Use.ID] = d
	}
	var results []llm.ContentBlock
	var extras []llm.Message
	for _, use := range uses {
		d, ok := done[use.ID]
		res := d.Result
		switch {
		case ok:
		case rejected[use.ID] || !pushed[use.ID]:
			res = ToolResult{Content: []llm.ContentBlock{llm.TextBlock(truncatedToolMsg)}, IsError: true}
			r.em.send(r.loop.callEvent(use))
		default:
			res = ToolResult{Content: []llm.ContentBlock{llm.TextBlock(emptyErrorText)}, IsError: true}
		}
		out := res
		r.em.send(Event{Kind: EventToolResult, ToolUseID: use.ID, ToolResult: &out, DurationMs: d.Elapsed.Milliseconds()})
		results = append(results, wrapToolResult(use.ID, res))
		extras = append(extras, res.Extra...)
	}
	return results, extras
}

func isJSONObject(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return strings.HasPrefix(s, "{") && json.Valid([]byte(s))
}

// 工具派发事件 带出它声明的审批档位 截断未执行的调用也填
func (l *Loop) callEvent(use llm.ContentBlock) Event {
	return Event{
		Kind:      EventToolUse,
		ToolUseID: use.ID,
		ToolName:  use.Name,
		ToolInput: use.Input,
		Approval:  l.approvalOf(use),
	}
}

// 未声明的工具按最高档处理 声明本身崩溃时同样按最高档
func (l *Loop) approvalOf(use llm.ContentBlock) *Approval {
	t, ok := l.reg.Get(use.Name)
	if !ok {
		return nil
	}
	a := Approval{Tier: TierExec}
	if d, ok := t.(ApprovalDeclarer); ok {
		if got, ok := safeApproval(d, use.Input); ok {
			a = got
		}
	}
	return &a
}

func safeApproval(d ApprovalDeclarer, input json.RawMessage) (a Approval, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	return d.Approval(input), true
}

// 内层内容套上调用编号 包成模型认得的工具结果块 全仓库只此一处
func wrapToolResult(id string, r ToolResult) llm.ContentBlock {
	return llm.ContentBlock{Type: llm.BlockToolResult, ToolUseID: id, Content: r.Content, IsError: r.IsError}
}

func runTool(ctx context.Context, t Tool, tc ToolContext, use llm.ContentBlock) (res ToolResult) { // 工具崩溃时在这里接住，把返回值 res 改成一条错误结果，整个程序不会退出
	defer func() {
		if r := recover(); r != nil {
			res = ToolResult{Content: []llm.ContentBlock{llm.TextBlock(fmt.Sprintf("Tool %s panicked: %v", use.Name, r))}, IsError: true}
		}
	}()
	args, problems := validateArgs(t.Schema(), use.Input)
	if len(problems) > 0 {
		return ToolResult{Content: []llm.ContentBlock{llm.TextBlock(use.Name + ": invalid arguments: " + strings.Join(problems, "; "))}, IsError: true}
	}
	return t.Execute(ctx, tc, args)
}

func hasSubstance(blocks []llm.ContentBlock) bool {
	for _, b := range blocks {
		if b.Type != llm.BlockText || strings.TrimSpace(b.Text) != "" {
			return true
		}
	}
	return false
}

func (l *Loop) makeSafe() func(llm.ContentBlock) bool {
	return func(use llm.ContentBlock) bool {
		t, ok := l.reg.Get(use.Name)
		if !ok {
			return false
		}
		s, ok := t.(Schedulable)
		if !ok {
			return false
		}
		return isSafe(s, use)
	}
}

func isSafe(s Schedulable, use llm.ContentBlock) (safe bool) {
	// 工具自己的判断代码崩溃时在这里接住，按不能并行处理
	defer func() {
		if recover() != nil {
			safe = false
		}
	}()
	return s.IsReadOnly() && s.IsConcurrencySafe(use.Input)
}

type toolCtx struct {
	useID      string
	cwd        string
	workspaces []string
	em         *emitter
	host       Host
	sess       session.Session
}

func (t *toolCtx) CWD() string          { return t.cwd }
func (t *toolCtx) Workspaces() []string { return t.workspaces }

// 只能在工具执行期间调用，工具都结束后 events 随时可能关闭，再发会崩溃
func (t *toolCtx) Emit(ev Event) {
	if ev.ToolUseID == "" {
		ev.ToolUseID = t.useID
	}
	t.em.send(ev)
}
func (t *toolCtx) Ask(ctx context.Context, req AskRequest) (AskResponse, error) {
	if t.host == nil {
		return AskResponse{}, ErrNoUI
	}
	return t.host.Ask(ctx, req)
}
func (t *toolCtx) RecentMessages() []llm.Message { return t.sess.Messages() }

func (t *toolCtx) CallTool(ctx context.Context, req ExtRequest) (ExtResult, error) {
	ext, ok := t.host.(Ext)
	if !ok {
		return ExtResult{}, ErrNoToolHost
	}
	return ext.CallTool(ctx, req)
}
