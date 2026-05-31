package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func RegisterSchedules(r *Registry) {
	r.Register(scheduleModel)
}

var scheduleModel = Tool{
	Name:             "schedule_model",
	Description:      "Schedule a model (or its downstream chain) to materialize on a cron. The model_id you pass is treated as the anchor — every model in its downstream chain runs together. Pass cron in standard 5-field format (e.g. '0 6 * * *' = daily at 06:00 UTC).",
	RequiresApproval: true,
	Parameters: mustJSON(map[string]any{
		"type":     "object",
		"required": []string{"model_id", "cron"},
		"properties": map[string]any{
			"model_id": map[string]any{"type": "string", "description": "Anchor model id (its downstream chain runs together)."},
			"cron":     map[string]any{"type": "string", "description": "Standard 5-field cron expression in UTC."},
		},
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var in struct {
			ModelID string `json:"model_id"`
			Cron    string `json:"cron"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		in.ModelID = strings.TrimSpace(in.ModelID)
		in.Cron = strings.TrimSpace(in.Cron)
		if in.ModelID == "" || in.Cron == "" {
			return nil, errors.New("model_id and cron are required")
		}
		now := time.Now().UTC().Format(time.RFC3339)
		id, err := tctx.DB.UpsertModelSchedule(tctx.ConnectionID, in.ModelID, in.Cron, now, tctx.Username)
		if err != nil {
			return nil, fmt.Errorf("schedule model: %w", err)
		}
		return map[string]any{
			"scheduled":   true,
			"schedule_id": id,
			"model_id":    in.ModelID,
			"cron":        in.Cron,
			"open_url":    tctx.AbsURL("/schedules"),
		}, nil
	},
}
