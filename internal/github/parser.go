// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package github

import (
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/caioricciuti/ch-ui/internal/models"
)

// ModelFrontmatter holds the YAML metadata from a model file header.
type ModelFrontmatter struct {
	Materialization string `yaml:"materialization"`
	TargetDatabase  string `yaml:"target_database"`
	TableEngine     string `yaml:"table_engine"`
	OrderBy         string `yaml:"order_by"`
	Description     string `yaml:"description"`
}

// ParsedModel is the result of parsing a .sql model file with YAML frontmatter.
type ParsedModel struct {
	Name        string
	Frontmatter ModelFrontmatter
	SQLBody     string
}

// ParseModelFile parses a .sql file with optional YAML frontmatter.
func ParseModelFile(filename, content string) (*ParsedModel, error) {
	// The file name (without .sql) becomes the model and table name, so it
	// must be a valid identifier.
	base := filepath.Base(filename)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	if err := models.ValidateModelName(name); err != nil {
		return nil, fmt.Errorf("file %s: %w", filename, err)
	}

	fm := ModelFrontmatter{
		Materialization: "view",
		TargetDatabase:  "default",
		TableEngine:     "MergeTree",
		OrderBy:         "tuple()",
	}

	trimmed := strings.TrimSpace(content)
	sqlBody := trimmed

	if strings.HasPrefix(trimmed, "---") {
		rest := trimmed[3:]
		endIdx := strings.Index(rest, "---")
		if endIdx >= 0 {
			yamlBlock := rest[:endIdx]
			sqlBody = strings.TrimSpace(rest[endIdx+3:])

			if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
				return nil, fmt.Errorf("parse YAML frontmatter in %s: %w", filename, err)
			}

			if fm.Materialization == "" {
				fm.Materialization = models.MaterializationView
			}
			if fm.TargetDatabase == "" {
				fm.TargetDatabase = "default"
			}
		}
	}

	if err := models.ValidateMaterialization(fm.Materialization); err != nil {
		return nil, fmt.Errorf("file %s: %w", filename, err)
	}
	if fm.Materialization == models.MaterializationTable {
		fm.TableEngine, fm.OrderBy = models.TableDefaults(fm.TableEngine, fm.OrderBy)
	}

	if sqlBody == "" {
		return nil, fmt.Errorf("model %s has no SQL body", filename)
	}

	return &ParsedModel{
		Name:        name,
		Frontmatter: fm,
		SQLBody:     sqlBody,
	}, nil
}
