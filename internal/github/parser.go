package github

import (
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
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
	name := strings.TrimSuffix(filepath.Base(filename), ".sql")
	if name == "" {
		return nil, fmt.Errorf("empty model name from filename %q", filename)
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
				fm.Materialization = "view"
			}
			if fm.TargetDatabase == "" {
				fm.TargetDatabase = "default"
			}
			if fm.Materialization == "table" {
				if fm.TableEngine == "" {
					fm.TableEngine = "MergeTree"
				}
				if fm.OrderBy == "" {
					fm.OrderBy = "tuple()"
				}
			}
		}
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
