package llm

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsContextOverflow(t *testing.T) {
	overflow := []string{
		`POST "https://api.anthropic.com/v1/messages": 400 Bad Request {"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 208310 tokens > 200000 maximum"}}`,
		`POST "https://api.openai.com/v1/chat/completions": 400 Bad Request: This model's maximum context length is 128000 tokens. However, your messages resulted in 130512 tokens.`,
		`openai: responses 流错误 context_length_exceeded Your input exceeds the context window of this model.`,
		`400 Bad Request: Input is too long for requested model.`,
		`request (70000 tokens) exceeds the available context size (65536 tokens), try increasing it`,
		`Trying to keep the first 9000 tokens when context the overflows. However, the model is loaded with context length of 8192 which is greater than the context length`,
		`invalid params, context window exceeds limit (2013)`,
		`Too many tokens: the request has 140000 tokens`,
	}
	for _, s := range overflow {
		if !IsContextOverflow(errors.New(s)) {
			t.Errorf("not recognised as an overflow: %s", s)
		}
	}
	other := []string{
		`POST "https://api.anthropic.com/v1/messages": 429 Too Many Requests {"type":"error","error":{"type":"rate_limit_error","message":"Number of request tokens has exceeded your per-minute rate limit"}}`,
		`401 Unauthorized: invalid x-api-key`,
		`529 {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`,
		`open C:\very\long\path: The filename or extension is too long.`,
		`max_tokens: 200000 > 64000, which is the maximum allowed number of output tokens`,
	}
	for _, s := range other {
		if IsContextOverflow(errors.New(s)) {
			t.Errorf("wrongly recognised as an overflow: %s", s)
		}
	}
	if IsContextOverflow(nil) {
		t.Error("nil is not an overflow")
	}
	wrapped := fmt.Errorf("stream: %w", errors.New("prompt is too long: 1 tokens > 0 maximum"))
	if !IsContextOverflow(wrapped) {
		t.Error("a wrapped overflow is still an overflow")
	}
}
