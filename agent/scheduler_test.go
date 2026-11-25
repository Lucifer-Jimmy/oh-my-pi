package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

// Run 是整批投递的便捷写法，等价于逐个 Push 后 Close 再 Collect
func (s *scheduler) Run(uses []llm.ContentBlock) iter.Seq[Done] {
	return func(yield func(Done) bool) {
		for _, u := range uses {
			s.Push(u)
		}
		s.Close()
		for d := range s.Collect() {
			if !yield(d) {
				return
			}
		}
	}
}

func use(id, name string) llm.ContentBlock {
	return llm.ContentBlock{Type: llm.BlockToolUse, ID: id, Name: name, Input: json.RawMessage(`{}`)}
}

func textResult(text string) ToolResult {
	return ToolResult{Content: []llm.ContentBlock{llm.TextBlock(text)}}
}

func resultText(r ToolResult) string {
	var s string
	for _, c := range r.Content {
		s += c.Text
	}
	return s
}

func collectDone(seq func(func(Done) bool)) []Done {
	var out []Done
	for d := range seq {
		out = append(out, d)
	}
	return out
}

// 结果严格按调用序产出，即使物理完成顺序被人为打乱
func TestSchedulerResultOrder(t *testing.T) {
	// id 越小完成越晚：t0 最慢、t2 最快，强制乱序完成
	delays := map[string]time.Duration{"t0": 30 * time.Millisecond, "t1": 15 * time.Millisecond, "t2": 0}
	exec := func(ctx context.Context, u llm.ContentBlock) ToolResult {
		time.Sleep(delays[u.ID])
		return textResult(u.ID)
	}
	safe := func(u llm.ContentBlock) bool { return true } // 全并发
	sem := make(chan struct{}, 8)
	s := newScheduler(context.Background(), exec, safe, sem)

	got := collectDone(s.Run([]llm.ContentBlock{use("t0", "read"), use("t1", "read"), use("t2", "read")}))
	if len(got) != 3 {
		t.Fatalf("配对不完整: %d", len(got))
	}
	for i, want := range []string{"t0", "t1", "t2"} {
		if got[i].Use.ID != want || resultText(got[i].Result) != want {
			t.Errorf("位置 %d = %q/%q, 期望 %q（结果未按调用序）", i, got[i].Use.ID, resultText(got[i].Result), want)
		}
	}
}

// 安全工具真并发执行（峰值并发 > 1）
func TestSchedulerConcurrent(t *testing.T) {
	var cur, peak int64
	var mu sync.Mutex
	exec := func(ctx context.Context, u llm.ContentBlock) ToolResult {
		n := atomic.AddInt64(&cur, 1)
		mu.Lock()
		if n > peak {
			peak = n
		}
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		atomic.AddInt64(&cur, -1)
		return textResult(u.ID)
	}
	safe := func(u llm.ContentBlock) bool { return true }
	s := newScheduler(context.Background(), exec, safe, make(chan struct{}, 8))
	collectDone(s.Run([]llm.ContentBlock{use("a", "read"), use("b", "read"), use("c", "read")}))
	if peak < 2 {
		t.Errorf("安全工具未并发，峰值并发 = %d", peak)
	}
}

// 独占工具执行期间无其他工具在跑，且挡住其后队列
func TestSchedulerExclusive(t *testing.T) {
	var cur int64
	var violated atomic.Bool
	exec := func(ctx context.Context, u llm.ContentBlock) ToolResult {
		n := atomic.AddInt64(&cur, 1)
		if u.Name == "write" && n != 1 {
			violated.Store(true) // 独占工具跑时有别人在跑
		}
		time.Sleep(5 * time.Millisecond)
		atomic.AddInt64(&cur, -1)
		return textResult(u.ID)
	}
	safe := func(u llm.ContentBlock) bool { return u.Name == "read" }
	s := newScheduler(context.Background(), exec, safe, make(chan struct{}, 8))
	// [read read write read]：前两 read 并发，write 独占，末 read 等 write
	collectDone(s.Run([]llm.ContentBlock{
		use("r1", "read"), use("r2", "read"), use("w", "write"), use("r3", "read"),
	}))
	if violated.Load() {
		t.Error("独占工具执行期间有其他工具并发")
	}
}

// 取消时未执行的工具按调用序合成错误结果，配对完整
func TestSchedulerCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	exec := func(ctx context.Context, u llm.ContentBlock) ToolResult {
		if u.ID == "w1" {
			cancel() // 第一个独占工具跑时就取消，其后的 w2/w3 不该执行
		}
		return textResult(u.ID)
	}
	safe := func(u llm.ContentBlock) bool { return false } // 全独占，逐段串行
	s := newScheduler(ctx, exec, safe, make(chan struct{}, 8))

	got := collectDone(s.Run([]llm.ContentBlock{use("w1", "write"), use("w2", "write"), use("w3", "write")}))
	if len(got) != 3 {
		t.Fatalf("配对不完整: %d", len(got))
	}
	if got[0].Use.ID != "w1" || got[1].Use.ID != "w2" || got[2].Use.ID != "w3" {
		t.Errorf("合成未按调用序: %v", []string{got[0].Use.ID, got[1].Use.ID, got[2].Use.ID})
	}
	if resultText(got[0].Result) != "w1" {
		t.Errorf("取消前已完成的 w1 应保留真结果, 得 %q", resultText(got[0].Result))
	}
	if !got[1].Result.IsError || resultText(got[1].Result) != toolAborted {
		t.Errorf("未执行的 w2 应合成 aborted, 得 %+v", got[1].Result)
	}
}

// sem 容量为 1 时全局串行，不死锁
func TestSchedulerSemaphoreBound(t *testing.T) {
	var cur, peak int64
	exec := func(ctx context.Context, u llm.ContentBlock) ToolResult {
		n := atomic.AddInt64(&cur, 1)
		if n > atomic.LoadInt64(&peak) {
			atomic.StoreInt64(&peak, n)
		}
		time.Sleep(2 * time.Millisecond)
		atomic.AddInt64(&cur, -1)
		return textResult(u.ID)
	}
	safe := func(u llm.ContentBlock) bool { return true }
	s := newScheduler(context.Background(), exec, safe, make(chan struct{}, 1)) // 闸门=1
	uses := make([]llm.ContentBlock, 5)
	for i := range uses {
		uses[i] = use(fmt.Sprintf("t%d", i), "read")
	}
	got := collectDone(s.Run(uses))
	if len(got) != 5 {
		t.Fatalf("配对不完整: %d", len(got))
	}
	if peak > 1 {
		t.Errorf("sem=1 应全局串行, 峰值并发 = %d", peak)
	}
}

// 空输入安全，零产出
func TestSchedulerEmpty(t *testing.T) {
	s := newScheduler(context.Background(),
		func(ctx context.Context, u llm.ContentBlock) ToolResult { return ToolResult{} },
		func(u llm.ContentBlock) bool { return true },
		make(chan struct{}, 4),
	)
	if got := collectDone(s.Run(nil)); len(got) != 0 {
		t.Errorf("空输入应零产出, 得 %d", len(got))
	}
}
