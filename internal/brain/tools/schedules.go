// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/cronexpr"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/models"
)

func RegisterSchedules(r *Registry) {
	r.Register(scheduleModel)
}

var scheduleModel = Tool{
	Name:             "schedule_model",
	Description:      "Schedule a model to build on a cron, with its upstream and downstream models (and their data tests). Creates or updates the named model pipeline called after the model, with selector +<model>+. Pass cron in standard 5-field format (e.g. '0 6 * * *' = daily at 06:00) and an optional IANA timezone (default UTC).",
	RequiresApproval: true,
	Parameters: mustJSON(map[string]any{
		"type":     "object",
		"required": []string{"model_id", "cron"},
		"properties": map[string]any{
			"model_id": map[string]any{"type": "string", "description": "The model to schedule; its upstream and downstream models run with it."},
			"cron":     map[string]any{"type": "string", "description": "Standard 5-field cron expression."},
			"timezone": map[string]any{"type": "string", "description": "IANA timezone the cron is read in, e.g. Europe/Lisbon (default UTC)."},
		},
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var in struct {
			ModelID  string `json:"model_id"`
			Cron     string `json:"cron"`
			Timezone string `json:"timezone"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		in.ModelID = strings.TrimSpace(in.ModelID)
		in.Cron = strings.TrimSpace(in.Cron)
		in.Timezone = strings.TrimSpace(in.Timezone)
		if in.ModelID == "" || in.Cron == "" {
			return nil, errors.New("model_id and cron are required")
		}
		if in.Timezone == "" {
			in.Timezone = "UTC"
		}
		if !cronexpr.ValidateCron(in.Cron) {
			return nil, fmt.Errorf("invalid cron expression %q", in.Cron)
		}
		m, err := tctx.DB.GetModelByID(in.ModelID)
		if err != nil {
			return nil, fmt.Errorf("lookup model: %w", err)
		}
		if m == nil || m.ConnectionID != tctx.ConnectionID {
			return nil, fmt.Errorf("model %s not found", in.ModelID)
		}
		next := models.NextRuns(in.Cron, in.Timezone, time.Now(), 1)
		if len(next) == 0 {
			return nil, fmt.Errorf("invalid cron or timezone")
		}
		nextAt := next[0].Format(time.RFC3339)
		p := database.ModelPipeline{
			ConnectionID: tctx.ConnectionID, Name: m.Name, Selector: "+" + m.Name + "+",
			Cron: in.Cron, Enabled: true, Timezone: in.Timezone, NextRunAt: &nextAt, CreatedBy: &tctx.Username,
		}
		existing, err := tctx.DB.GetModelPipelineByName(tctx.ConnectionID, m.Name)
		if err != nil {
			return nil, fmt.Errorf("schedule model: %w", err)
		}
		id := ""
		if existing != nil {
			p.ID = existing.ID
			id = existing.ID
			err = tctx.DB.UpdateModelPipeline(p)
		} else {
			id, err = tctx.DB.CreateModelPipeline(p)
		}
		if err != nil {
			return nil, fmt.Errorf("schedule model: %w", err)
		}
		return map[string]any{
			"scheduled":   true,
			"pipeline_id": id,
			"model_id":    in.ModelID,
			"selector":    p.Selector,
			"cron":        in.Cron,
			"timezone":    in.Timezone,
			"next_run_at": nextAt,
			"open_url":    tctx.AbsURL("/models"),
		}, nil
	},
}
