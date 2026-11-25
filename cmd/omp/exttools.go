package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
)

type externalToolDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
	ReadOnly    bool            `json:"read_only,omitempty"`
	Write       bool            `json:"write,omitempty"`
	OpenWorld   bool            `json:"open_world,omitempty"`
}

func loadExternalTools(path string) ([]agent.Tool, error) {
	if path == "" {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("--external-tools: %w", err)
	}
	var decls []externalToolDecl
	if err := json.Unmarshal(b, &decls); err != nil {
		return nil, fmt.Errorf("--external-tools: %s: %w", path, err)
	}
	out := make([]agent.Tool, 0, len(decls))
	for i, d := range decls {
		if d.Name == "" {
			return nil, fmt.Errorf("--external-tools: entry %d has no name", i+1)
		}
		if len(d.Schema) == 0 {
			d.Schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, &agent.RemoteTool{
			ToolName:  d.Name,
			ToolDesc:  d.Description,
			InputJSON: d.Schema,
			ReadOnly:  d.ReadOnly,
			WriteTier: d.Write,
			OpenWorld: d.OpenWorld,
		})
	}
	return out, nil
}
