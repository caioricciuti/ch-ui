// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Package schemacompare compares ClickHouse metadata. It never executes DDL.
package schemacompare

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

type Column struct {
	Table             string `json:"table"`
	Name              string `json:"name"`
	Type              string `json:"type"`
	Position          uint64 `json:"position"`
	DefaultKind       string `json:"default_kind"`
	DefaultExpression string `json:"default_expression"`
	CompressionCodec  string `json:"compression_codec"`
}

type Table struct {
	Name         string   `json:"name"`
	Engine       string   `json:"engine"`
	SortingKey   string   `json:"sorting_key"`
	PartitionKey string   `json:"partition_key"`
	PrimaryKey   string   `json:"primary_key"`
	SamplingKey  string   `json:"sampling_key"`
	CreateSQL    string   `json:"-"`
	Columns      []Column `json:"columns"`
}

type Difference struct {
	Table     string `json:"table"`
	Kind      string `json:"kind"`
	Field     string `json:"field"`
	Source    string `json:"source"`
	Target    string `json:"target"`
	ReviewSQL string `json:"review_sql"`
}

type Result struct {
	Differences    []Difference `json:"differences"`
	SourceTables   int          `json:"source_tables"`
	TargetTables   int          `json:"target_tables"`
	MatchingTables int          `json:"matching_tables"`
	ReviewSQL      string       `json:"review_sql"`
}

func QuoteIdentifier(s string) string {
	return "`" + strings.ReplaceAll(strings.ReplaceAll(s, "\\", "\\\\"), "`", "\\`") + "`"
}

// ReviewOnly comments every line, including newlines originating from untrusted
// metadata. Exported plans cannot execute by being pasted into a SQL editor.
func ReviewOnly(sql string) string {
	sql = strings.ReplaceAll(sql, "\r", "\n")
	return "-- " + strings.ReplaceAll(sql, "\n", "\n-- ")
}

func Compare(source, target []Table, targetDatabase string) Result {
	result := Result{SourceTables: len(source), TargetTables: len(target), Differences: []Difference{}}
	sources, targets := map[string]Table{}, map[string]Table{}
	names := map[string]bool{}
	for _, t := range source {
		sources[t.Name] = t
		names[t.Name] = true
	}
	for _, t := range target {
		targets[t.Name] = t
		names[t.Name] = true
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	add := func(name, kind, field, before, after, sql string) {
		result.Differences = append(result.Differences, Difference{Table: name, Kind: kind, Field: field, Source: before, Target: after, ReviewSQL: ReviewOnly(sql)})
	}
	for _, name := range ordered {
		s, sourceExists := sources[name]
		t, targetExists := targets[name]
		qualified := QuoteIdentifier(targetDatabase) + "." + QuoteIdentifier(name)
		if !sourceExists {
			add(name, "target_only", "table", "", t.Engine, "Target-only object: "+qualified+". Keep unless a reviewer explicitly approves removal.")
			continue
		}
		if !targetExists {
			// Engine arguments can contain credentials/cluster-specific paths. Do
			// not export source CREATE statements as a target migration.
			cols := make([]string, 0, len(s.Columns))
			for _, c := range s.Columns {
				cols = append(cols, "    "+columnSQL(c))
			}
			proposal := "Create missing " + qualified + " after reviewing engine settings and dependencies.\nCREATE TABLE " + qualified + " (\n" + strings.Join(cols, ",\n") + "\n) ENGINE = " + s.Engine + " /* supply reviewed engine arguments */"
			if strings.Contains(s.Engine, "View") {
				viewKind := "VIEW"
				if s.Engine == "MaterializedView" {
					viewKind = "MATERIALIZED VIEW"
				}
				proposal = "Create missing view " + qualified + " after reviewing its SELECT dependencies and target destination.\nCREATE " + viewKind + " " + qualified + " " + viewDefinition(s.CreateSQL) + ";"
			} else {
				if s.PartitionKey != "" {
					proposal += "\nPARTITION BY " + s.PartitionKey
				}
				if s.SortingKey != "" {
					proposal += "\nORDER BY " + s.SortingKey
				}
				if s.PrimaryKey != "" {
					proposal += "\nPRIMARY KEY " + s.PrimaryKey
				}
				if s.SamplingKey != "" {
					proposal += "\nSAMPLE BY " + s.SamplingKey
				}
				if ttl := topLevelClause(s.CreateSQL, "TTL", "SETTINGS", "COMMENT", "AS"); ttl != "" {
					proposal += "\nTTL " + ttl
				}
			}
			add(name, "source_only", "table", s.Engine, "", proposal)
			continue
		}
		start := len(result.Differences)
		for _, f := range []struct{ name, source, target string }{
			{"engine", s.Engine, t.Engine}, {"sorting_key", s.SortingKey, t.SortingKey},
			{"partition_key", s.PartitionKey, t.PartitionKey}, {"primary_key", s.PrimaryKey, t.PrimaryKey}, {"sampling_key", s.SamplingKey, t.SamplingKey},
			{"ttl", topLevelClause(s.CreateSQL, "TTL", "SETTINGS", "COMMENT", "AS"), topLevelClause(t.CreateSQL, "TTL", "SETTINGS", "COMMENT", "AS")},
		} {
			if f.source == f.target {
				continue
			}
			proposal := "Review " + f.name + " for " + qualified + ". This change may require rebuilding the table. Desired: " + f.source
			if f.name == "ttl" {
				if f.source == "" {
					proposal = "ALTER TABLE " + qualified + " REMOVE TTL;"
				} else {
					proposal = "ALTER TABLE " + qualified + " MODIFY TTL " + f.source + ";\nTTL changes can expire existing data; review retention before applying."
				}
			}
			if f.name == "sorting_key" && f.source != "" {
				proposal = "ALTER TABLE " + qualified + " MODIFY ORDER BY " + f.source + ";\nCheck engine restrictions and whether existing data needs a rebuild."
			}
			add(name, "changed", f.name, f.source, f.target, proposal)
		}
		if strings.Contains(s.Engine, "View") || strings.Contains(t.Engine, "View") {
			sv, tv := viewDefinition(s.CreateSQL), viewDefinition(t.CreateSQL)
			if sv != tv {
				add(name, "changed", "view_definition", sv, tv, "Review view definition and destination dependencies for "+qualified+".\n"+sv)
			}
		}
		sc, tc := map[string]Column{}, map[string]Column{}
		for _, c := range s.Columns {
			sc[c.Name] = c
		}
		for _, c := range t.Columns {
			tc[c.Name] = c
		}
		for _, c := range s.Columns {
			other, exists := tc[c.Name]
			field := "column." + c.Name
			if !exists {
				add(name, "source_only", field, columnSQL(c), "", "ALTER TABLE "+qualified+" ADD COLUMN "+columnSQL(c)+";")
				continue
			}
			if columnSQL(c) != columnSQL(other) {
				add(name, "changed", field, columnSQL(c), columnSQL(other), "ALTER TABLE "+qualified+" MODIFY COLUMN "+columnSQL(c)+";\nReview type conversions, default/codec removal and dependent views before applying.")
			}
			if c.Position != other.Position {
				add(name, "changed", field+".position", fmt.Sprint(c.Position), fmt.Sprint(other.Position), "Review column ordering for "+qualified+"."+QuoteIdentifier(c.Name)+"; positional INSERT statements may depend on it.")
			}
		}
		for _, c := range t.Columns {
			if _, ok := sc[c.Name]; !ok {
				add(name, "target_only", "column."+c.Name, "", columnSQL(c), "Target-only column "+qualified+"."+QuoteIdentifier(c.Name)+" retained. Removal requires explicit review.")
			}
		}
		if len(result.Differences) == start {
			result.MatchingTables++
		}
	}
	parts := []string{ReviewOnly("SCHEMA REVIEW — source definitions compared with target.\nAll statements are commented and require manual review. No SQL has been executed.\nMetadata reflects only objects visible to the supplied accounts; engine arguments are excluded.")}
	for _, d := range result.Differences {
		parts = append(parts, ReviewOnly(d.Table+" · "+d.Field)+"\n"+d.ReviewSQL)
	}
	result.ReviewSQL = strings.Join(parts, "\n\n")
	return result
}

func columnSQL(c Column) string {
	s := QuoteIdentifier(c.Name) + " " + c.Type
	if c.DefaultKind != "" {
		s += " " + c.DefaultKind + " " + c.DefaultExpression
	}
	if c.CompressionCodec != "" {
		s += " " + c.CompressionCodec
	}
	return s
}

type token struct {
	word       string
	start, end int
}

// topLevelWords skips quoted literals/identifiers, comments and nested
// expressions. A TTL keyword inside a column/string is not a table TTL.
func topLevelWords(sql string) []token {
	out := []token{}
	depth := 0
	for i := 0; i < len(sql); {
		c := sql[i]
		if c == '\'' || c == '"' || c == '`' {
			quote := c
			i++
			for i < len(sql) {
				if sql[i] == '\\' {
					i += 2
					continue
				}
				if sql[i] == quote {
					i++
					if i < len(sql) && sql[i] == quote {
						i++
						continue
					}
					break
				}
				i++
			}
			continue
		}
		if i+1 < len(sql) && sql[i:i+2] == "--" {
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			continue
		}
		if i+1 < len(sql) && sql[i:i+2] == "/*" {
			i += 2
			for i+1 < len(sql) && sql[i:i+2] != "*/" {
				i++
			}
			i += 2
			continue
		}
		if c == '(' {
			depth++
			i++
			continue
		}
		if c == ')' {
			depth--
			i++
			continue
		}
		if unicode.IsLetter(rune(c)) || c == '_' {
			start := i
			for i < len(sql) && (unicode.IsLetter(rune(sql[i])) || unicode.IsDigit(rune(sql[i])) || sql[i] == '_') {
				i++
			}
			if depth == 0 {
				out = append(out, token{strings.ToUpper(sql[start:i]), start, i})
			}
			continue
		}
		i++
	}
	return out
}

func topLevelClause(sql, keyword string, stops ...string) string {
	words := topLevelWords(sql)
	for i, word := range words {
		if word.word != keyword {
			continue
		}
		end := len(sql)
		for _, next := range words[i+1:] {
			stop := false
			for _, term := range stops {
				if next.word == term {
					stop = true
					break
				}
			}
			if stop {
				end = next.start
				break
			}
		}
		return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(sql[word.end:end]), ";"))
	}
	return ""
}

func viewDefinition(sql string) string {
	// Include materialized-view destinations, excluding the source view's own
	// database/name and UUID. SELECT text remains literal so semantic changes in
	// string constants are not hidden by whitespace normalization.
	query := topLevelClause(sql, "AS")
	to := topLevelClause(sql, "TO", "AS", "ENGINE")
	if to != "" {
		return "TO " + to + " AS " + query
	}
	return "AS " + query
}
