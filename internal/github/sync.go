package github

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
)

// Syncer orchestrates pulling models from a GitHub repo into CH-UI.
type Syncer struct {
	db     *database.DB
	secret string
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
		return nil, fmt.Errorf("invalid repo format %q — expected owner/repo", repo)
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

	commitSHA, err := client.GetBranchSHA(owner, repoName, branch)
	if err != nil {
		return nil, fmt.Errorf("get branch SHA: %w", err)
	}

	if commitSHA == lastSHA && lastSHA != "" {
		return &SyncResult{CommitSHA: commitSHA}, nil
	}

	tree, err := client.GetTree(owner, repoName, branch)
	if err != nil {
		return nil, fmt.Errorf("get repo tree: %w", err)
	}

	sqlFiles := FilterSQLFiles(tree, path)
	slog.Info("GitHub sync: found SQL files", "count", len(sqlFiles), "repo", owner+"/"+repoName, "branch", branch)

	parsed := make([]*ParsedModel, 0, len(sqlFiles))
	var parseErrors []string
	for _, f := range sqlFiles {
		content, err := client.GetFileContent(owner, repoName, f.Path, branch)
		if err != nil {
			parseErrors = append(parseErrors, fmt.Sprintf("fetch %s: %v", f.Path, err))
			continue
		}
		model, err := ParseModelFile(f.Path, content)
		if err != nil {
			parseErrors = append(parseErrors, fmt.Sprintf("parse %s: %v", f.Path, err))
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

		if !exists {
			id, err := s.db.CreateModel(
				connectionID, p.Name, p.Frontmatter.Description,
				p.Frontmatter.TargetDatabase, p.Frontmatter.Materialization,
				p.SQLBody, p.Frontmatter.TableEngine, p.Frontmatter.OrderBy, "",
			)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("create %s: %v", p.Name, err))
				continue
			}
			_ = s.db.SetModelSource(id, "github")
			result.Created++
			continue
		}

		if modelChanged(ex, p) {
			err := s.db.UpdateModel(
				ex.ID, p.Name, p.Frontmatter.Description,
				p.Frontmatter.TargetDatabase, p.Frontmatter.Materialization,
				p.SQLBody, p.Frontmatter.TableEngine, p.Frontmatter.OrderBy,
			)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("update %s: %v", p.Name, err))
				continue
			}
			result.Updated++
		} else {
			result.Unchanged++
		}
	}

	for _, ex := range existing {
		if !seen[ex.Name] {
			if err := s.db.DeleteModel(ex.ID); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("delete %s: %v", ex.Name, err))
				continue
			}
			result.Deleted++
		}
	}

	_ = s.db.SetGitHubLastSyncSHA(connectionID, commitSHA)
	return result, nil
}

func modelChanged(existing database.Model, parsed *ParsedModel) bool {
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
