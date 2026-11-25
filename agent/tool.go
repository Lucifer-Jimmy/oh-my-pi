package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage
	Execute(ctx context.Context, tc ToolContext, args json.RawMessage) ToolResult
}

type ToolResult struct {
	Content []llm.ContentBlock
	IsError bool
	Extra   []llm.Message
}

// 声明调度安全属性
type Schedulable interface {
	IsReadOnly() bool
	IsConcurrencySafe(input json.RawMessage) bool
}

// 声明审批档位
type ApprovalDeclarer interface {
	Approval(input json.RawMessage) Approval
}

type Approval struct {
	Tier        Tier
	ForcePrompt bool
	Reason      string
}

// Metadata 描述档位表达不了的三个维度 审批界面和评测报告读取它
type Metadata struct {
	// 可能删除、覆盖或不可逆地改动外部状态
	Destructive bool
	// 能通过网络、远程服务或任意进程接触到工作区之外
	OpenWorld bool
	// 结果字节上限 声明值必须与工具的实际输出一致
	MaxResultSize int
}

// 声明元数据 没实现该接口的工具按最保守的值处理
type MetadataDeclarer interface {
	Metadata(input json.RawMessage) Metadata
}

const defaultMaxResultSize = 64 * 1024

// 未声明、声明崩溃、或整份声明为零值的工具都退回最保守的元数据 零值无法与"没声明"区分 所以按没声明处理
func MetadataOf(t Tool, input json.RawMessage) Metadata {
	conservative := Metadata{Destructive: true, OpenWorld: true, MaxResultSize: defaultMaxResultSize}
	d, ok := t.(MetadataDeclarer)
	if !ok {
		return conservative
	}
	m, ok := safeMetadata(d, input)
	if !ok || m == (Metadata{}) {
		return conservative
	}
	if m.MaxResultSize <= 0 {
		m.MaxResultSize = defaultMaxResultSize
	}
	return m
}

func safeMetadata(d MetadataDeclarer, input json.RawMessage) (m Metadata, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	return d.Metadata(input), true
}

const truncNotice = "\n... (result truncated at %d bytes; the tool's own notice above says how to ask for less)"

func clampResult(t Tool, input json.RawMessage, res ToolResult) ToolResult {
	limit := MetadataOf(t, input).MaxResultSize
	if limit <= 0 {
		return res
	}
	n := 0
	for _, b := range res.Content {
		n += len(b.Text) + len(b.Thinking) + len(b.Name) + len(b.Input)
		for _, c := range b.Content {
			n += len(c.Text)
		}
	}
	if n <= limit {
		return res
	}
	out := ToolResult{IsError: res.IsError, Extra: res.Extra}
	budget := limit
	for _, b := range res.Content {
		if budget <= 0 {
			break
		}
		if b.Type == llm.BlockText {
			if len(b.Text) > budget {
				out.Content = append(out.Content, llm.TextBlock(b.Text[:budget]+fmt.Sprintf(truncNotice, limit)))
				budget = 0
				continue
			}
			budget -= len(b.Text)
		}
		out.Content = append(out.Content, b)
	}
	if budget == 0 && len(out.Content) == 0 {
		out.Content = append(out.Content, llm.TextBlock(fmt.Sprintf(truncNotice, limit)))
	}
	return out
}

type Tier string

const (
	TierRead  Tier = "read"
	TierWrite Tier = "write"
	TierExec  Tier = "exec"
)

type ToolContext interface {
	CWD() string
	Workspaces() []string
	Emit(ev Event)
	Ask(ctx context.Context, req AskRequest) (AskResponse, error)
	RecentMessages() []llm.Message
}
