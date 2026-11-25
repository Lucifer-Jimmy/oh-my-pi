package llm

import (
	"regexp"
	"strings"
)

var contextOverflow = regexp.MustCompile(`(?i)` + strings.Join([]string{
	`prompt is too long`,
	`input is too long for requested model`,
	`exceeds the context window`,
	`input token count.*exceeds the maximum`,
	`maximum prompt length is \d+`,
	`reduce the length of the messages`,
	`maximum context length is \d+ tokens`,
	`context[_ ]length[_ ]exceeded`,
	`model_context_window_exceeded`,
	`exceeds the available context size`,
	`greater than the context length`,
	`context window exceeds limit`,
	`exceeded model token limit`,
	`prompt filled the context window`,
	`requested tokens?.*exceeds?.*context (window|length|size)`,
	`(prompt|input).*(too long|too large).*(context|n_ctx)`,
	`exceeds the limit of \d+ tokens`,
	`too many tokens`,
	`token limit exceeded`,
}, "|"))

func IsContextOverflow(err error) bool {
	return err != nil && contextOverflow.MatchString(err.Error())
}
