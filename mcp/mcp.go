package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

const (
	namePrefix   = "mcp__"
	maxNameLen   = 64
	resultCap    = 64 * 1024
	clientName   = "omp"
	clientVer    = "0.1.0"
	separatorLen = 1
)

type ServerConfig struct {
	Command string
	Args    []string
	Env     map[string]string
	URL     string
	SSE     bool
}

type Server struct {
	Name    string
	Session *sdk.ClientSession
	tools   []*sdk.Tool
}

func Connect(ctx context.Context, name string, cfg ServerConfig) (*Server, error) {
	url := strings.TrimSpace(cfg.URL)
	command := strings.TrimSpace(cfg.Command)
	switch {
	case url != "" && command != "":
		return nil, fmt.Errorf("mcp server %q: set either command or url, not both", name)
	case url == "" && command == "":
		return nil, fmt.Errorf("mcp server %q: command or url is required", name)
	case url != "":
		httpClient := &http.Client{Timeout: 0}
		if cfg.SSE {
			return connectTransport(ctx, name, &sdk.SSEClientTransport{Endpoint: url, HTTPClient: httpClient})
		}
		return connectTransport(ctx, name, &sdk.StreamableClientTransport{Endpoint: url, HTTPClient: httpClient})
	}
	cmd := exec.Command(command, cfg.Args...)
	for k, v := range cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	return connectTransport(ctx, name, &sdk.CommandTransport{Command: cmd})
}

func connectTransport(ctx context.Context, name string, transport sdk.Transport) (*Server, error) {
	client := sdk.NewClient(&sdk.Implementation{Name: clientName, Version: clientVer}, nil)
	sess, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp server %q: connect: %w", name, err)
	}
	s := &Server{Name: name, Session: sess}
	tools, err := s.listTools(ctx)
	if err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("mcp server %q: list tools: %w", name, err)
	}
	s.tools = tools
	return s, nil
}

func (s *Server) listTools(ctx context.Context) ([]*sdk.Tool, error) {
	var out []*sdk.Tool
	params := &sdk.ListToolsParams{}
	for {
		res, err := s.Session.ListTools(ctx, params)
		if err != nil {
			return nil, err
		}
		out = append(out, res.Tools...)
		if res.NextCursor == "" {
			return out, nil
		}
		params.Cursor = res.NextCursor
	}
}

func (s *Server) Tools() []agent.Tool {
	out := make([]agent.Tool, 0, len(s.tools))
	for i := range s.tools {
		out = append(out, &tool{server: s, remote: s.tools[i]})
	}
	return out
}

func (s *Server) Close() error { return s.Session.Close() }

func ToolName(server, remote string) string {
	s := sanitize(server)
	r := sanitize(remote)
	if len(s) > 16 {
		s = s[:16]
	}
	budget := maxNameLen - len(namePrefix) - len(s) - separatorLen
	if len(r) > budget {
		r = r[:max(budget, 1)]
	}
	return namePrefix + s + "_" + r
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "tool"
	}
	return out
}

type tool struct {
	server *Server
	remote *sdk.Tool
}

func (t *tool) Name() string { return ToolName(t.server.Name, t.remote.Name) }

func (t *tool) Description() string {
	d := strings.TrimSpace(t.remote.Description)
	if d == "" {
		d = "MCP tool " + t.remote.Name + " from " + t.server.Name
	}
	return d
}

func (t *tool) Schema() json.RawMessage {
	b, err := json.Marshal(t.remote.InputSchema)
	if err != nil || len(b) == 0 || string(b) == "null" {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	return b
}

func (t *tool) Approval(json.RawMessage) agent.Approval {
	return agent.Approval{Tier: agent.TierWrite}
}

func (t *tool) Metadata(json.RawMessage) agent.Metadata {
	return agent.Metadata{OpenWorld: true, MaxResultSize: resultCap}
}

func (t *tool) Execute(ctx context.Context, tc agent.ToolContext, raw json.RawMessage) agent.ToolResult {
	var args map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return result("mcp: arguments are not a JSON object: "+err.Error(), true)
		}
	}
	res, err := t.server.Session.CallTool(ctx, &sdk.CallToolParams{
		Name:      t.remote.Name,
		Arguments: args,
	})
	if err != nil {
		return result(fmt.Sprintf("mcp: %s.%s failed: %v", t.server.Name, t.remote.Name, err), true)
	}
	text := strings.TrimSpace(flatten(res))
	if text == "" {
		text = "(no output)"
	}
	if len(text) > resultCap {
		text = text[:resultCap] + "\n... (mcp result truncated)"
	}
	return result(text, res.IsError)
}

func flatten(res *sdk.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		switch v := c.(type) {
		case *sdk.TextContent:
			parts = append(parts, v.Text)
		default:
			b, err := json.Marshal(v)
			if err == nil {
				parts = append(parts, string(b))
			}
		}
	}
	if len(parts) == 0 && res.StructuredContent != nil {
		b, err := json.Marshal(res.StructuredContent)
		if err == nil {
			parts = append(parts, string(b))
		}
	}
	return strings.Join(parts, "\n")
}

func result(text string, isErr bool) agent.ToolResult {
	return agent.ToolResult{Content: []llm.ContentBlock{{Type: llm.BlockText, Text: text}}, IsError: isErr}
}

type Pool struct {
	mu      sync.Mutex
	servers []*Server
}

func (p *Pool) Connect(ctx context.Context, name string, cfg ServerConfig) (*Server, error) {
	s, err := Connect(ctx, name, cfg)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.servers = append(p.servers, s)
	p.mu.Unlock()
	return s, nil
}

func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range p.servers {
		_ = s.Close()
	}
	p.servers = nil
}

type StdioTool struct {
	Name        string
	Description string
	Schema      map[string]any
	Run         func(ctx context.Context, args map[string]any) (string, error)
}

func ServeStdio(ctx context.Context, name, version string, tools []StdioTool) error {
	srv := sdk.NewServer(&sdk.Implementation{Name: name, Version: version}, nil)
	for _, t := range tools {
		t := t
		srv.AddTool(&sdk.Tool{Name: t.Name, Description: t.Description, InputSchema: t.Schema},
			func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
				var args map[string]any
				if len(req.Params.Arguments) > 0 {
					if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
						return nil, fmt.Errorf("arguments are not a JSON object: %w", err)
					}
				}
				text, err := t.Run(ctx, args)
				if err != nil {
					return &sdk.CallToolResult{
						IsError: true,
						Content: []sdk.Content{&sdk.TextContent{Text: err.Error()}},
					}, nil
				}
				return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: text}}}, nil
			})
	}
	return srv.Run(ctx, &sdk.StdioTransport{})
}
