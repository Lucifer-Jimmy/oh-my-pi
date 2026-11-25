package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Control struct {
	Store    Store
	Pipeline *Pipeline
	Snapshot string
	Context  context.Context

	running atomic.Bool
	wg      sync.WaitGroup
}

func (c *Control) Start(force bool) bool {
	if c.Pipeline == nil || !c.running.CompareAndSwap(false, true) {
		return false
	}
	ctx := c.Context
	if ctx == nil {
		ctx = context.Background()
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer c.running.Store(false)
		_, _ = c.Pipeline.Run(ctx, force)
	}()
	return true
}

func (c *Control) Command(args string) (string, error) {
	switch sub := strings.ToLower(strings.TrimSpace(args)); sub {
	case "", "view", "show":
		return c.view(), nil
	case "sync":
		if c.Pipeline == nil {
			return "", errors.New("memory updates need a saved session; this one is not saved")
		}
		if !c.Start(true) {
			return "", errors.New("a memory update is already running")
		}
		return "Memory update started in the background: every past session of this project not read yet is read now and the summary is rebuilt. /memory shows the result; it applies from the next session.", nil
	case "clear", "reset":
		if c.running.Load() {
			return "", errors.New("a memory update is running; clear the memory when it finishes")
		}
		if err := c.Store.Clear(); err != nil {
			return "", err
		}
		return "Deleted this project's memory (" + c.Store.Dir + "). This session keeps the memory it started with.", nil
	default:
		return "", fmt.Errorf("unknown /memory command %q: use /memory, /memory sync or /memory clear", sub)
	}
}

func (c *Control) view() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Memory for this project: %s\n", c.Store.Dir)
	fmt.Fprintf(&b, "- Lessons saved with learn: %d\n", c.Store.LessonCount())
	if st, ok := c.Store.status(); ok {
		fmt.Fprintf(&b, "- Last update %s: %d sessions read, %d with something durable", time.UnixMilli(st.FinishedAt).Format("2006-01-02 15:04"), st.Read, st.Useful)
		if st.Failed > 0 {
			fmt.Fprintf(&b, ", %d failed", st.Failed)
		}
		if st.Parked > 0 {
			fmt.Fprintf(&b, ", %d skipped after %d failures", st.Parked, maxExtractFailures)
		}
		if st.Pending > 0 {
			fmt.Fprintf(&b, ", %d left for the next update", st.Pending)
		}
		switch {
		case st.Summarized:
			fmt.Fprintf(&b, "; summary rebuilt from %d sessions", st.Sessions)
		case st.Sessions == 0 && c.Store.Summary() == "":
			b.WriteString("; no summary yet")
		}
		b.WriteString("\n")
		if st.Error != "" {
			fmt.Fprintf(&b, "- Last error: %s\n", firstLine(st.Error))
		}
	} else {
		b.WriteString("- No update has run yet. Past sessions are read at startup once they have been idle for 12 hours; /memory sync reads them now.\n")
	}
	if c.running.Load() {
		b.WriteString("- An update is running now.\n")
	}
	if c.Snapshot == "" {
		b.WriteString("This session started with no memory.")
	} else {
		b.WriteString("This session started with this memory:\n" + c.Snapshot)
	}
	return b.String()
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}
