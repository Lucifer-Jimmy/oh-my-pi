package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

// Anthropic 接口要求必须给输出上限，调用方没给时用 omp 对未知模型的兜底值
const unknownModelMaxTokens = 64000

type Config struct {
	APIKey  string
	BaseURL string
	Model   string
}

type Provider struct {
	client anthropic.Client
	model  string
}

func New(cfg Config) *Provider {
	opts := []option.RequestOption{option.WithoutEnvironmentDefaults()} // 凭证由我们控制，不读环境变量获取
	if cfg.APIKey != "" {
		opts = append(opts, option.WithAPIKey(cfg.APIKey))
	}
	if base := normalizeBaseURL(cfg.BaseURL); base != "" {
		opts = append(opts, option.WithBaseURL(base))
	}
	return &Provider{client: anthropic.NewClient(opts...), model: cfg.Model}
}

func normalizeBaseURL(s string) string {
	return strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(s), "/"), "/v1")
}

// 将 CompletionRequest 装配成 Anthropic wire
func (p *Provider) buildParams(req llm.CompletionRequest) (anthropic.MessageNewParams, error) {
	messages, err := convertMessages(req.Messages)
	if err != nil {
		return anthropic.MessageNewParams{}, err
	}
	applyTailBreakpoint(messages)

	system, err := convertSystem(req.System, req.CacheBoundary)
	if err != nil {
		return anthropic.MessageNewParams{}, err
	}

	tools, err := convertTools(req.Tools)
	if err != nil {
		return anthropic.MessageNewParams{}, err
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = unknownModelMaxTokens
	}
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(p.model),
		MaxTokens: int64(maxTokens),
		Messages:  messages,
		System:    system,
		Tools:     tools,
	}
	applyThinking(&params, req.Thinking)
	return params, nil
}

func convertTools(tools []llm.ToolSchema) ([]anthropic.ToolUnionParam, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		var schema map[string]any
		if len(t.InputSchema) > 0 {
			if err := json.Unmarshal(t.InputSchema, &schema); err != nil {
				return nil, fmt.Errorf("anthropic: 工具 %q 的 InputSchema 解析失败: %w", t.Name, err)
			}
		}
		in := anthropic.ToolInputSchemaParam{Properties: map[string]any{}}
		for k, v := range schema {
			switch k {
			case "type":
			case "properties":
				in.Properties = v
			case "required":
				for _, r := range asSlice(v) {
					if s, ok := r.(string); ok {
						in.Required = append(in.Required, s)
					}
				}
			default:
				if in.ExtraFields == nil {
					in.ExtraFields = map[string]any{}
				}
				in.ExtraFields[k] = v
			}
		}
		tool := anthropic.ToolUnionParamOfTool(in, t.Name)
		if t.Description != "" {
			tool.OfTool.Description = anthropic.String(t.Description)
		}
		out = append(out, tool)
	}
	return out, nil
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

// 中立 ThinkingLevel 映射到 effort，off 省略、minimal 降为 low
func applyThinking(params *anthropic.MessageNewParams, level llm.ThinkingLevel) {
	switch level {
	case llm.ThinkingOff, "":
	case llm.ThinkingMinimal:
		params.OutputConfig.Effort = anthropic.OutputConfigEffort(llm.ThinkingLow)
	default:
		params.OutputConfig.Effort = anthropic.OutputConfigEffort(level)
	}
}

// Stream 跑流式请求并产出中性事件，ctx 取消作为 aborted 终态产出，不作传输错误
func (p *Provider) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		params, err := p.buildParams(req)
		if err != nil {
			yield(llm.StreamEvent{}, err)
			return
		}
		stream := p.client.Messages.NewStreaming(ctx, params)
		defer stream.Close()
		for stream.Next() {
			if !emit(stream.Current(), yield) {
				return // 消费者 break
			}
		}
		if err := stream.Err(); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				yield(llm.StreamEvent{Type: llm.SEMessageDelta, StopReason: llm.StopReasonAborted}, nil)
				return
			}
			yield(llm.StreamEvent{}, classify(err))
		}
	}
}

func classify(err error) error {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode != 0 {
		return &llm.HTTPError{Status: apiErr.StatusCode, RequestID: apiErr.RequestID, Err: err, RetryAfter: retryAfter(apiErr)}
	}
	return err
}

func retryAfter(e *anthropic.Error) time.Duration {
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

// Complete 非流式，复用 Stream + Accumulator，避开 SDK Messages.New 的 maxTokens 自动超时
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
