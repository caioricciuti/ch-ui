// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

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

	// Role is the CH-UI role of the person chatting (admin, analyst or
	// viewer). Tools that change the workspace need a writer role, the same
	// rule the REST routes enforce with RequireWriter.
	Role string

	// Guardrail runs the governance policies on sql before it executes and
	// returns an error when a policy blocks it. Nil means no policies apply.
	Guardrail func(sql string) error
	// RecordQuery writes a user-facing query to the person's query history.
	// Nil skips recording.
	RecordQuery func(sql, status, errMsg string, elapsed time.Duration, rows int64)

	ChatID    string
	MessageID string

	// WorkspaceURL is the absolute origin of the user's workspace app
	// (e.g. "https://localhost:5521"). Tools concatenate paths onto this
	// when returning open_url so the LLM emits clickable links.
	WorkspaceURL string

	DB      *database.DB
	Gateway *tunnel.Gateway

	RunModel      func(modelID string) (runID string, err error)
	BuildModel    func(modelID string, fullRefresh bool) (runID string, err error)
	StartPipeline func(pipelineID string) error
}

// ErrWriteForbidden is returned when a viewer asks for a tool that changes
// the workspace.
var ErrWriteForbidden = errors.New("this action changes the workspace and needs the admin or analyst role; viewers have read-only access")

// CanWrite reports whether the person may run tools that change the
// workspace, matching middleware.RequireWriter.
func (c Context) CanWrite() bool {
	return c.Role == "admin" || c.Role == "analyst"
}

// checkGuardrail runs the governance policies on sql when configured.
func (c Context) checkGuardrail(sql string) error {
	if c.Guardrail == nil {
		return nil
	}
	return c.Guardrail(sql)
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
	Name        string
	Description string
	Parameters  json.RawMessage
	// RequiresApproval marks a tool that changes the workspace (dashboards,
	// models, saved queries, pipelines, schedules). Each call waits for the
	// person to approve it, and only writers may propose or run one.
	RequiresApproval bool
	Handler          func(tctx Context, args json.RawMessage) (any, error)
}

// IsWrite reports whether the tool changes the workspace.
func (t Tool) IsWrite() bool { return t.RequiresApproval }

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
	return r.DefinitionsFor(true)
}

// DefinitionsFor returns the definitions offered to the model. Viewers are
// not offered tools that change the workspace.
func (r *Registry) DefinitionsFor(canWrite bool) []Definition {
	out := make([]Definition, 0, len(r.tools))
	for _, name := range r.Names() {
		t := r.tools[name]
		if t.IsWrite() && !canWrite {
			continue
		}
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
	if t.IsWrite() && !tctx.CanWrite() {
		payload, _ := json.Marshal(map[string]string{"error": ErrWriteForbidden.Error()})
		return payload, ErrWriteForbidden
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
