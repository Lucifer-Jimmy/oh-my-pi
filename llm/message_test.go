package llm

import (
	"encoding/json"
	"testing"
)

func TestToolUses(t *testing.T) {
	msg := Message{Role: RoleAssistant, Content: []ContentBlock{
		TextBlock("我来读取文件"),
		{Type: BlockThinking, Thinking: "先读文件", Signature: "sig"},
		{Type: BlockToolUse, ID: "test1", Name: "Read", Input: json.RawMessage(`{"filepath": "a.go"}`)},
		TextBlock("顺便读取关联文件"),
		{Type: BlockToolUse, ID: "test2", Name: "Bash", Input: json.RawMessage(`{"command": "ls"}`)},
	}}

	got := msg.ToolUses()

	if len(got) != 2 {
		t.Fatalf("ToolUses Number = %d, want 2", len(got))
	}
	if got[0].ID != "test1" || got[0].Name != "Read" {
		t.Fatalf("got[0] = %s/%s, want test1/Read", got[0].ID, got[0].Name)
	}
	if got[1].ID != "test2" || got[1].Name != "Bash" {
		t.Fatalf("got[1] = %s/%s, want test2/Bash", got[1].ID, got[1].Name)
	}
}

func TestToolUsesNone(t *testing.T) {
	if got := UserText("你好").ToolUses(); len(got) != 0 {
		t.Fatalf("无工具调用时 want 0, got %d", len(got))
	}
}

func TestContentBlockSerialization(t *testing.T) {
	data, err := json.Marshal(ToolResultText("t1", "ok", false))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"ok"}]}`
	if string(data) != want {
		t.Fatalf("序列化 = %s, want %s", data, want)
	}
}

func TestText(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want string
	}{
		{
			name: "多个 text 拼接测试",
			msg: Message{
				Content: []ContentBlock{
					TextBlock("第一段"),
					{Type: BlockToolUse, ID: "test1", Name: "Read"},
					TextBlock("第二段"),
				},
			},
			want: "第一段第二段",
		},
		{
			name: "需要忽略 thinking 和 tool_use",
			msg: Message{
				Content: []ContentBlock{
					{Type: BlockThinking, Thinking: "思考内容"},
					TextBlock("可见内容"),
				},
			},
			want: "可见内容",
		},
		{name: "空消息", msg: Message{}, want: ""},
		{
			name: "无 text 块",
			msg: Message{
				Content: []ContentBlock{
					{Type: BlockThinking, Thinking: ""},
				},
			},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.msg.Text(); got != tt.want {
				t.Fatalf("Text() = %q, want = %q", got, tt.want)
			}
		})
	}
}
