package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

var ErrNotSessionFile = errors.New("not a session file")

func checkSessionPath(path string) error {
	if !strings.HasSuffix(strings.ToLower(path), FileSuffix) {
		return fmt.Errorf("%s: %w", path, ErrNotSessionFile)
	}
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if st.IsDir() {
		return fmt.Errorf("%s: %w", path, ErrNotSessionFile)
	}
	return nil
}

func DeleteByPath(path string) error {
	if err := checkSessionPath(path); err != nil {
		return err
	}
	return os.Remove(path)
}

func Markdown(msgs []llm.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		text := strings.TrimSpace(messageText(m))
		if text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "**%s**\n\n%s\n", m.Role, text)
	}
	return b.String()
}

func messageText(m llm.Message) string {
	var parts []string
	for _, c := range m.Content {
		switch c.Type {
		case llm.BlockText:
			parts = append(parts, c.Text)
		case llm.BlockToolUse:
			parts = append(parts, fmt.Sprintf("[tool %s]", c.Name))
		case llm.BlockToolResult:
			var t strings.Builder
			for _, inner := range c.Content {
				t.WriteString(inner.Text)
			}
			body := strings.TrimSpace(t.String())
			if len(body) > 4000 {
				body = body[:4000] + "\n... (truncated)"
			}
			parts = append(parts, "[tool result]\n"+body)
		}
	}
	return strings.Join(parts, "\n\n")
}

func WriteExport(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o644)
}
