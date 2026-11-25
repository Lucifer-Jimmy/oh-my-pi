package agent

import (
	"context"
	"iter"
	"sync"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

const toolAborted = "Tool execution was aborted."

type jobState int

const (
	jobQueued jobState = iota
	jobRunning
	jobDone
)

type job struct {
	use     llm.ContentBlock
	safe    bool
	state   jobState
	result  ToolResult
	elapsed time.Duration
}

// 已完成的工具结果，供 Loop emit 事件
type Done struct {
	Use     llm.ContentBlock
	Result  ToolResult
	Elapsed time.Duration
}

// 边到边派发：安全工具在参数块结束时立刻开跑，独占工具自成一段挡住其后调用，
// 结果仍严格按调用序产出。调度状态由 Push / Close / Collect 三个动作驱动。
type scheduler struct {
	ctx  context.Context
	exec func(ctx context.Context, use llm.ContentBlock) ToolResult
	safe func(use llm.ContentBlock) bool
	sem  chan struct{}

	mu        sync.Mutex
	cond      *sync.Cond
	jobs      []*job
	running   int
	exclusive bool
	closed    bool
}

func newScheduler(
	ctx context.Context,
	exec func(ctx context.Context, use llm.ContentBlock) ToolResult,
	safe func(use llm.ContentBlock) bool,
	sem chan struct{},
) *scheduler {
	s := &scheduler{ctx: ctx, exec: exec, safe: safe, sem: sem}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// Push 收下一个已完成参数的工具调用，能跑就立刻在后台开跑
func (s *scheduler) Push(use llm.ContentBlock) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.jobs = append(s.jobs, &job{use: use, safe: s.safe(use)})
	s.pump()
}

// Close 宣布不会再有新的调用，Collect 可以在队列耗尽后正常结束
func (s *scheduler) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.pump()
	s.cond.Broadcast()
}

// Collect 按调用序逐条产出结果，等齐一个再进下一个
func (s *scheduler) Collect() iter.Seq[Done] {
	return func(yield func(Done) bool) {
		i := 0
		s.mu.Lock()
		for {
			for {
				if i < len(s.jobs) && s.jobs[i].state == jobDone {
					break
				}
				if i >= len(s.jobs) && s.closed {
					s.mu.Unlock()
					return
				}
				s.cond.Wait()
			}
			d := Done{Use: s.jobs[i].use, Result: s.jobs[i].result, Elapsed: s.jobs[i].elapsed}
			i++
			s.mu.Unlock()
			ok := yield(d)
			s.mu.Lock()
			if !ok {
				s.mu.Unlock()
				return
			}
		}
	}
}

// pump 在持有锁时决定谁可以开跑，必须满足：独占工具跑时无人并发，队列里排在前面的独占调用不被跳过
func (s *scheduler) pump() {
	if s.ctx.Err() != nil {
		for _, j := range s.jobs {
			if j.state == jobQueued {
				j.state = jobDone
				j.result = ToolResult{Content: []llm.ContentBlock{llm.TextBlock(toolAborted)}, IsError: true}
			}
		}
		s.cond.Broadcast()
		return
	}
	for _, j := range s.jobs {
		switch j.state {
		case jobDone:
			continue
		case jobRunning:
			if s.exclusive {
				return
			}
			continue
		}
		if s.exclusive {
			return
		}
		if j.safe {
			s.start(j)
			continue
		}
		if s.running > 0 {
			return
		}
		s.exclusive = true
		s.start(j)
		return
	}
}

func (s *scheduler) start(j *job) {
	j.state = jobRunning
	s.running++
	go s.run(j)
}

func (s *scheduler) run(j *job) {
	if s.sem != nil {
		s.sem <- struct{}{}
	}
	start := time.Now()
	res := s.exec(s.ctx, j.use)
	elapsed := time.Since(start)
	if s.sem != nil {
		<-s.sem
	}
	s.mu.Lock()
	j.result = res
	j.elapsed = elapsed
	j.state = jobDone
	s.running--
	if !j.safe {
		s.exclusive = false
	}
	s.pump()
	s.cond.Broadcast()
	s.mu.Unlock()
}
