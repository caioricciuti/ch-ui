// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package github

import (
	"fmt"
	"log/slog"
	"path"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
)

// Syncer orchestrates pulling models from a GitHub repo into CH-UI.
type Syncer struct {
	db     *database.DB
	secret string
	// apiBase overrides the GitHub API URL (tests only).
	apiBase string
}

// SyncResult holds the outcome of a sync operation.
type SyncResult struct {
	Created   int
	Updated   int
	Deleted   int
	Unchanged int
	CommitSHA string
	Errors    []string
}

// NewSyncer creates a new GitHub sync orchestrator.
func NewSyncer(db *database.DB, appSecret string) *Syncer {
	return &Syncer{db: db, secret: appSecret}
}

// SyncConnection pulls models from GitHub for the given connection.
func (s *Syncer) SyncConnection(connectionID, triggeredBy string) (*SyncResult, error) {
	repo, branch, path, encryptedPAT, _, lastSHA := s.db.GetGitHubRawSettings(connectionID)
	if repo == "" || encryptedPAT == "" {
		return nil, fmt.Errorf("GitHub integration not configured for this connection")
	}

	pat, err := crypto.Decrypt(encryptedPAT, s.secret)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt GitHub PAT: %w", err)
	}

	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("invalid repo format %q, expected owner/repo", repo)
	}
	owner, repoName := parts[0], parts[1]

	logID, _ := s.db.CreateGitHubSyncLog(connectionID, triggeredBy)

	result, syncErr := s.doSync(connectionID, owner, repoName, branch, path, pat, lastSHA)
	if syncErr != nil {
		if logID != "" {
			_ = s.db.FinalizeGitHubSyncLog(logID, "error", 0, 0, 0, 0, "", syncErr.Error())
		}
		return nil, syncErr
	}

	if logID != "" {
		errMsg := ""
		if len(result.Errors) > 0 {
			errMsg = strings.Join(result.Errors, "; ")
		}
		status := "success"
		if errMsg != "" {
			status = "partial"
		}
		_ = s.db.FinalizeGitHubSyncLog(logID, status, result.Created, result.Updated, result.Deleted, result.Unchanged, result.CommitSHA, errMsg)
	}

	return result, nil
}

func (s *Syncer) doSync(connectionID, owner, repoName, branch, path, pat, lastSHA string) (*SyncResult, error) {
	client := NewClient(pat)
	if s.apiBase != "" {
		client.baseURL = s.apiBase
	}

	commitSHA, err := client.GetBranchSHA(owner, repoName, branch)
	if err != nil {
		return nil, fmt.Errorf("get branch SHA: %w", err)
	}

	if commitSHA == lastSHA && lastSHA != "" {
		return &SyncResult{CommitSHA: commitSHA}, nil
	}

	// Read the tree and every file at the commit SHA, not the branch name, so
	// a push during the sync cannot mix two commits.
	tree, err := client.GetTree(owner, repoName, commitSHA)
	if err != nil {
		return nil, fmt.Errorf("get repo tree: %w", err)
	}

	sqlFiles := FilterSQLFiles(tree, path)
	slog.Info("GitHub sync: found SQL files", "count", len(sqlFiles), "repo", owner+"/"+repoName, "branch", branch)

	parsed := make([]*ParsedModel, 0, len(sqlFiles))
	var parseErrors []string
	// Files that could not be fetched or parsed still exist in the repo:
	// their models must not be deleted below.
	failedNames := make(map[string]bool)
	for _, f := range sqlFiles {
		content, err := client.GetFileContent(owner, repoName, f.Path, commitSHA)
		if err != nil {
			parseErrors = append(parseErrors, fmt.Sprintf("fetch %s: %v", f.Path, err))
			failedNames[modelNameFromPath(f.Path)] = true
			continue
		}
		model, err := ParseModelFile(f.Path, content)
		if err != nil {
			parseErrors = append(parseErrors, fmt.Sprintf("parse %s: %v", f.Path, err))
			failedNames[modelNameFromPath(f.Path)] = true
			continue
		}
		parsed = append(parsed, model)
	}

	existing, err := s.db.GetModelsByConnectionAndSource(connectionID, "github")
	if err != nil {
		return nil, fmt.Errorf("get existing github models: %w", err)
	}

	existingByName := make(map[string]database.Model, len(existing))
	for _, m := range existing {
		existingByName[m.Name] = m
	}

	result := &SyncResult{CommitSHA: commitSHA, Errors: parseErrors}

	seen := make(map[string]bool, len(parsed))
	for _, p := range parsed {
		seen[p.Name] = true
		ex, exists := existingByName[p.Name]

		pm := p.Model()
		if !exists {
			// The model, its options and its tests are written in one
			// transaction: a failure leaves no half-created model.
			pm.ConnectionID, pm.Source = connectionID, "github"
			if _, err := s.db.SaveModel(database.ModelSave{Model: pm, ReplaceTests: true, Tests: p.Tests}); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("create %s: %v", p.Name, err))
				continue
			}
			result.Created++
			continue
		}

		testsChanged, err := s.testsChanged(ex.ID, p.Tests)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("tests of %s: %v", p.Name, err))
			continue
		}
		if modelChanged(ex, p) || testsChanged {
			pm.ID = ex.ID
			if _, err := s.db.SaveModel(database.ModelSave{Model: pm, ReplaceTests: testsChanged, Tests: p.Tests}); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("update %s: %v", p.Name, err))
				continue
			}
			result.Updated++
		} else {
			result.Unchanged++
		}
	}

	for _, ex := range existing {
		if !seen[ex.Name] && !failedNames[ex.Name] {
			if err := s.db.DeleteModel(ex.ID); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("delete %s: %v", ex.Name, err))
				continue
			}
			result.Deleted++
		}
	}

	// Record the commit only when everything in it applied. A sync with
	// errors keeps the previous SHA, so the next sync retries this commit
	// instead of skipping it as already synced.
	if len(result.Errors) == 0 {
		if err := s.db.SetGitHubLastSyncSHA(connectionID, commitSHA); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("record synced commit: %v", err))
		}
	}
	return result, nil
}

func modelNameFromPath(p string) string {
	base := path.Base(p)
	return strings.TrimSuffix(base, path.Ext(base))
}

// testsChanged reports whether the file's tests differ from the stored ones.
func (s *Syncer) testsChanged(modelID string, want []database.ModelTest) (bool, error) {
	have, err := s.db.GetModelTests(modelID)
	if err != nil {
		return false, err
	}
	if len(have) != len(want) {
		return true, nil
	}
	for i := range want {
		h, w := have[i], want[i]
		if h.Kind != w.Kind || h.ColumnName != w.ColumnName || h.Args != w.Args || h.Severity != w.Severity {
			return true, nil
		}
	}
	return false, nil
}

func modelChanged(existing database.Model, parsed *ParsedModel) bool {
	if existing.Options() != parsed.Model().Options() {
		return true
	}
	if existing.SQLBody != parsed.SQLBody {
		return true
	}
	if existing.Description != parsed.Frontmatter.Description {
		return true
	}
	if existing.Materialization != parsed.Frontmatter.Materialization {
		return true
	}
	if existing.TargetDatabase != parsed.Frontmatter.TargetDatabase {
		return true
	}
	if existing.TableEngine != parsed.Frontmatter.TableEngine {
		return true
	}
	if existing.OrderBy != parsed.Frontmatter.OrderBy {
		return true
	}
	return false
}
