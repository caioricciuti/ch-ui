package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// Context carries everything a tool handler needs to do its job.
type Context struct {
	Ctx context.Context

	ConnectionID string
	Username     string
	CHUser       string
	CHPassword   string

	ChatID    string
	MessageID string

	// WorkspaceURL is the absolute origin of the user's workspace app
	// (e.g. "https://localhost:5521"). Tools concatenate paths onto this
	// when returning open_url so the LLM emits clickable links.
	WorkspaceURL string

	DB      *database.DB
	Gateway *tunnel.Gateway

	RunModel      func(modelID string) (runID string, err error)
	BuildModel    func(modelID string) (runID string, err error)
	StartPipeline func(pipelineID string) error
}

// AbsURL joins a path onto the workspace origin.
func (c Context) AbsURL(path string) string {
	if path == "" {
		return c.WorkspaceURL
	}
	if c.WorkspaceURL == "" {
		return path
	}
	if path[0] != '/' {
		path = "/" + path
	}
	return c.WorkspaceURL + path
}

// Tool is a single function Brain can invoke.
type Tool struct {
	Name             string
	Description      string
	Parameters       json.RawMessage
	RequiresApproval bool
	Handler          func(tctx Context, args json.RawMessage) (any, error)
}

// Registry maps tool names to definitions.
type Registry struct {
	tools map[string]Tool
}

func New() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

func (r *Registry) Register(t Tool) {
	r.tools[t.Name] = t
}

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Names returns tool names in deterministic order.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Definitions returns OpenAI-style function definitions for every tool.
func (r *Registry) Definitions() []Definition {
	out := make([]Definition, 0, len(r.tools))
	for _, name := range r.Names() {
		t := r.tools[name]
		out = append(out, Definition{
			Type: "function",
			Function: FunctionDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}
	return out
}

type Definition struct {
	Type     string      `json:"type"`
	Function FunctionDef `json:"function"`
}

type FunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Execute runs a tool by name and returns its result as JSON.
func (r *Registry) Execute(tctx Context, name string, args json.RawMessage) (json.RawMessage, error) {
	t, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
	result, err := t.Handler(tctx, args)
	if err != nil {
		payload, _ := json.Marshal(map[string]string{"error": err.Error()})
		return payload, err
	}
	payload, mErr := json.Marshal(result)
	if mErr != nil {
		return nil, fmt.Errorf("marshal tool result: %w", mErr)
	}
	return payload, nil
}
