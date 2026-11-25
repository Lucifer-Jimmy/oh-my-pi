package openai

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

type Config struct {
	APIKey  string
	BaseURL string
	Model   string
	Api     llm.Protocol
}

type Provider struct {
	client openai.Client
	model  string
	api    llm.Protocol
}

func New(cfg Config) *Provider {
	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	} else {
		opts = append(opts, option.WithEnvironmentProduction())
	}
	return &Provider{client: openai.NewClient(opts...), model: cfg.Model, api: cfg.Api}
}

func (p *Provider) buildParams(req llm.CompletionRequest) (openai.ChatCompletionNewParams, error) {
	messages, err := convertMessages(req.System, req.Messages)
	if err != nil {
		return openai.ChatCompletionNewParams{}, err
	}
	tools, err := convertTools(req.Tools)
	if err != nil {
		return openai.ChatCompletionNewParams{}, err
	}
	params := openai.ChatCompletionNewParams{
		Model:         openai.ChatModel(p.model),
		Messages:      messages,
		Tools:         tools,
		StreamOptions: openai.ChatCompletionStreamOptionsParam{IncludeUsage: openai.Bool(true)},
	}
	applyMaxTokens(&params, req.MaxTokens)
	applyThinking(&params, req.Thinking)
	return params, nil
}

func applyMaxTokens(params *openai.ChatCompletionNewParams, maxTokens int) {
	if maxTokens <= 0 {
		return
	}
	params.MaxCompletionTokens = openai.Int(int64(maxTokens))
}

func applyThinking(params *openai.ChatCompletionNewParams, level llm.ThinkingLevel) {
	switch level {
	case llm.ThinkingOff, "":
	default:
		params.ReasoningEffort = openai.ReasoningEffort(level)
	}
}

func (p *Provider) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	if p.api == llm.ProtocolOpenAIResponses {
		return p.streamResponses(ctx, req)
	}
	return p.streamCompletions(ctx, req)
}

func (p *Provider) streamCompletions(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		params, err := p.buildParams(req)
		if err != nil {
			yield(llm.StreamEvent{}, err)
			return
		}
		stream := p.client.Chat.Completions.NewStreaming(ctx, params)
		defer stream.Close()
		synth := newCompletionSynth()
		for stream.Next() {
			events, err := synth.feed(stream.Current())
			if err != nil {
				yield(llm.StreamEvent{}, err)
				return
			}
			for _, ev := range events {
				if !yield(ev, nil) {
					return
				}
			}
		}
		if err := stream.Err(); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				for _, ev := range synth.abort() {
					if !yield(ev, nil) {
						return
					}
				}
				return
			}
			yield(llm.StreamEvent{}, classifyHTTP(withServerMessage(err)))
			return
		}
		for _, ev := range synth.finalize() {
			if !yield(ev, nil) {
				return
			}
		}
	}
}

// SDK 的错误文字只有状态码，把服务器给的原因补在后面，排查时才看得到
func classifyHTTP(err error) error {
	var apiErr *openai.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode != 0 {
		return &llm.HTTPError{Status: apiErr.StatusCode, Err: err, RetryAfter: retryAfter(apiErr)}
	}
	return err
}

func retryAfter(e *openai.Error) time.Duration {
	if e.Response == nil {
		return 0
	}
	v := strings.TrimSpace(e.Response.Header.Get("retry-after"))
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		return time.Until(t)
	}
	return 0
}

func withServerMessage(err error) error {
	var apiErr *openai.Error
	if errors.As(err, &apiErr) && apiErr.Message != "" {
		return fmt.Errorf("%w: %s", err, apiErr.Message)
	}
	return err
}

func (p *Provider) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, llm.StopReason, llm.Usage, error) {
	acc := llm.NewAccumulator()
	for ev, err := range p.Stream(ctx, req) {
		if err != nil {
			return llm.Message{}, llm.StopReasonError, llm.Usage{}, err
		}
		if err := acc.Add(ev); err != nil {
			return llm.Message{}, llm.StopReasonError, llm.Usage{}, err
		}
	}
	return acc.Message(), acc.StopReason, acc.Usage, nil
}
