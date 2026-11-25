package main

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

var ansiEscape = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-Z\\-_]`)

func runPrint(ctx context.Context, run agent.Runner, sess session.Session, prompts []string, stdout, stderr io.Writer) int {
	fmt.Fprintln(stderr, "Working...")
	var res *agent.Result
	for _, p := range prompts {
		for ev, err := range run.Run(ctx, sess, p) {
			if err != nil {
				fmt.Fprintln(stderr, sanitize(err.Error()))
				return 1
			}
			if ev.Kind == agent.EventResult {
				res = ev.Result
			}
		}
		if res == nil {
			fmt.Fprintln(stderr, "omp: the run produced no result")
			return 1
		}
		if res.Reason != agent.ReasonCompleted {
			break
		}
	}
	switch {
	case res.Reason == agent.ReasonCompleted:
		if res.Text != "" {
			fmt.Fprintln(stdout, sanitize(res.Text))
		}
		return 0
	case ctx.Err() != nil:
		return 1
	default:
		msg := res.Err
		if msg == "" {
			msg = "Request " + string(res.Reason)
		}
		fmt.Fprintln(stderr, sanitize(msg))
		return 1
	}
}

func sanitize(s string) string {
	s = ansiEscape.ReplaceAllString(strings.ToValidUTF8(s, ""), "")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || (r >= 0x20 && r < 0x7f) || r > 0x9f {
			return r
		}
		return -1
	}, s)
}
