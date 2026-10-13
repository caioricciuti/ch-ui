// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package github

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/models"
)

// ModelFrontmatter holds the YAML metadata from a model file header.
type ModelFrontmatter struct {
	Materialization string `yaml:"materialization"`
	TargetDatabase  string `yaml:"target_database"`
	TableEngine     string `yaml:"table_engine"`
	OrderBy         string `yaml:"order_by"`
	Description     string `yaml:"description"`

	PartitionBy         string `yaml:"partition_by"`
	EngineArgs          string `yaml:"engine_args"`
	IncrementalStrategy string `yaml:"incremental_strategy"`
	UniqueKey           string `yaml:"unique_key"`
	WatermarkColumn     string `yaml:"watermark_column"`

	// Tests are the model's data tests. The file is the source of truth: a
	// sync replaces the model's tests with this list.
	Tests []FrontmatterTest `yaml:"tests"`
}

// FrontmatterTest is one entry of the tests: list.
//
//	tests:
//	  - kind: not_null
//	    column: user_id
//	  - kind: accepted_values
//	    column: status
//	    values: [paid, refunded]
//	    severity: warn
//	  - kind: relationship
//	    column: user_id
//	    to: users
//	    field: id
//	  - kind: expression
//	    expression: amount >= 0
type FrontmatterTest struct {
	Kind            string `yaml:"kind"`
	Column          string `yaml:"column"`
	Severity        string `yaml:"severity"`
	models.TestArgs `yaml:",inline"`
}

// ParsedModel is the result of parsing a .sql model file with YAML frontmatter.
type ParsedModel struct {
	Name        string
	Frontmatter ModelFrontmatter
	SQLBody     string
	// Tests are the frontmatter tests, validated and normalized.
	Tests []database.ModelTest
}

// Model returns the parsed file as a model (name, settings and SQL).
func (p *ParsedModel) Model() database.Model {
	fm := p.Frontmatter
	return database.Model{
		Name: p.Name, Description: fm.Description, TargetDatabase: fm.TargetDatabase,
		Materialization: fm.Materialization, SQLBody: p.SQLBody,
		TableEngine: fm.TableEngine, OrderBy: fm.OrderBy,
		PartitionBy: fm.PartitionBy, EngineArgs: fm.EngineArgs,
		IncrementalStrategy: fm.IncrementalStrategy, UniqueKey: fm.UniqueKey, WatermarkColumn: fm.WatermarkColumn,
	}
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

	if sqlBody == "" {
		return nil, fmt.Errorf("model %s has no SQL body", filename)
	}

	p := &ParsedModel{Name: name, Frontmatter: fm, SQLBody: sqlBody}
	m := p.Model()
	if err := models.NormalizeModel(&m); err != nil {
		return nil, fmt.Errorf("file %s: %w", filename, err)
	}
	p.Frontmatter.Materialization, p.Frontmatter.TableEngine, p.Frontmatter.OrderBy = m.Materialization, m.TableEngine, m.OrderBy
	p.Frontmatter.PartitionBy, p.Frontmatter.EngineArgs = m.PartitionBy, m.EngineArgs
	p.Frontmatter.IncrementalStrategy, p.Frontmatter.UniqueKey, p.Frontmatter.WatermarkColumn = m.IncrementalStrategy, m.UniqueKey, m.WatermarkColumn

	for i, ft := range fm.Tests {
		args, _ := json.Marshal(ft.TestArgs)
		t := database.ModelTest{Kind: ft.Kind, ColumnName: ft.Column, Severity: ft.Severity, Args: string(args)}
		if err := models.NormalizeTest(&t); err != nil {
			return nil, fmt.Errorf("file %s: test %d: %w", filename, i+1, err)
		}
		p.Tests = append(p.Tests, t)
	}
	return p, nil
}
