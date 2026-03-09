package models

import (
	"fmt"
	"regexp"
	"strings"
)

// stripSQLComments removes single-line (-- ...) and block (/* ... */) comments
// so that $ref() inside comments is not treated as a real reference.
func stripSQLComments(sql string) string {
	// Remove block comments first (non-greedy, handles multiline)
	blockRe := regexp.MustCompile(`(?s)/\*.*?\*/`)
	sql = blockRe.ReplaceAllString(sql, "")
	// Remove single-line comments
	lineRe := regexp.MustCompile(`--[^\n]*`)
	sql = lineRe.ReplaceAllString(sql, "")
	return sql
}

// refPattern matches $ref(model_name) in SQL.
// Model names follow ClickHouse identifier rules: [a-zA-Z_][a-zA-Z0-9_]*
var refPattern = regexp.MustCompile(`\$ref\(\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\)`)

// modelNamePattern validates model names as valid ClickHouse identifiers.
var modelNamePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// ValidateModelName checks if a name is a valid ClickHouse identifier.
func ValidateModelName(name string) error {
	if name == "" {
		return fmt.Errorf("model name cannot be empty")
	}
	if !modelNamePattern.MatchString(name) {
		return fmt.Errorf("model name %q must be a valid identifier (letters, digits, underscores, starting with letter or underscore)", name)
	}
	return nil
}

// ExtractRefs returns all model names referenced via $ref() in the SQL body.
// $ref() occurrences inside SQL comments are ignored.
func ExtractRefs(sqlBody string) []string {
	matches := refPattern.FindAllStringSubmatch(stripSQLComments(sqlBody), -1)
	seen := make(map[string]bool)
	var refs []string
	for _, m := range matches {
		name := strings.TrimSpace(m[1])
		if !seen[name] {
			seen[name] = true
			refs = append(refs, name)
		}
	}
	return refs
}

// ResolveRefs replaces all $ref(model_name) with `target_database`.`model_name`.
// modelTargets maps model_name -> target_database.
// $ref() occurrences inside SQL comments are ignored (comments are stripped first).
func ResolveRefs(sqlBody string, modelTargets map[string]string) (string, error) {
	sqlBody = stripSQLComments(sqlBody)
	var resolveErr error
	resolved := refPattern.ReplaceAllStringFunc(sqlBody, func(match string) string {
		sub := refPattern.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		name := strings.TrimSpace(sub[1])
		db, ok := modelTargets[name]
		if !ok {
			resolveErr = fmt.Errorf("unresolved reference: $ref(%s)", name)
			return match
		}
		return fmt.Sprintf("`%s`.`%s`", db, name)
	})
	return resolved, resolveErr
}
