package github

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a minimal GitHub API client using a Personal Access Token.
type Client struct {
	token   string
	baseURL string
	http    *http.Client
}

// TreeEntry represents a file in a GitHub repo tree.
type TreeEntry struct {
	Path string `json:"path"`
	SHA  string `json:"sha"`
	Type string `json:"type"`
}

// NewClient creates a GitHub API client with the given PAT.
func NewClient(pat string) *Client {
	return &Client{
		token:   pat,
		baseURL: "https://api.github.com",
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) do(method, path string) ([]byte, int, error) {
	url := c.baseURL + path
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("github request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}
	return body, resp.StatusCode, nil
}

// ValidateAccess checks that the PAT can access the given repo.
func (c *Client) ValidateAccess(owner, repo string) error {
	_, status, err := c.do("GET", fmt.Sprintf("/repos/%s/%s", owner, repo))
	if err != nil {
		return err
	}
	if status == 401 || status == 403 {
		return fmt.Errorf("authentication failed — check your Personal Access Token")
	}
	if status == 404 {
		return fmt.Errorf("repository %s/%s not found — check the name and token permissions", owner, repo)
	}
	if status != 200 {
		return fmt.Errorf("unexpected status %d from GitHub API", status)
	}
	return nil
}

// GetBranchSHA returns the latest commit SHA for a branch.
func (c *Client) GetBranchSHA(owner, repo, branch string) (string, error) {
	body, status, err := c.do("GET", fmt.Sprintf("/repos/%s/%s/branches/%s", owner, repo, branch))
	if err != nil {
		return "", err
	}
	if status != 200 {
		return "", fmt.Errorf("failed to get branch %s: HTTP %d", branch, status)
	}
	var result struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse branch response: %w", err)
	}
	return result.Commit.SHA, nil
}

// GetTree returns all file entries in the repo tree for a given branch.
func (c *Client) GetTree(owner, repo, branch string) ([]TreeEntry, error) {
	body, status, err := c.do("GET", fmt.Sprintf("/repos/%s/%s/git/trees/%s?recursive=1", owner, repo, branch))
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("failed to get tree: HTTP %d", status)
	}
	var result struct {
		Tree []TreeEntry `json:"tree"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse tree response: %w", err)
	}
	return result.Tree, nil
}

// FilterSQLFiles returns tree entries that are .sql blobs under the given path prefix.
func FilterSQLFiles(entries []TreeEntry, pathPrefix string) []TreeEntry {
	pathPrefix = strings.TrimSuffix(pathPrefix, "/") + "/"
	var filtered []TreeEntry
	for _, e := range entries {
		if e.Type != "blob" {
			continue
		}
		if !strings.HasPrefix(e.Path, pathPrefix) {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(e.Path), ".sql") {
			continue
		}
		// Skip files in subdirectories (only top-level .sql files in the path)
		rel := strings.TrimPrefix(e.Path, pathPrefix)
		if strings.Contains(rel, "/") {
			continue
		}
		filtered = append(filtered, e)
	}
	return filtered
}

// GetFileContent fetches and decodes a file's content from the repo.
func (c *Client) GetFileContent(owner, repo, path, ref string) (string, error) {
	body, status, err := c.do("GET", fmt.Sprintf("/repos/%s/%s/contents/%s?ref=%s", owner, repo, path, ref))
	if err != nil {
		return "", err
	}
	if status != 200 {
		return "", fmt.Errorf("failed to get file %s: HTTP %d", path, status)
	}
	var result struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse file response: %w", err)
	}
	if result.Encoding != "base64" {
		return "", fmt.Errorf("unsupported encoding: %s", result.Encoding)
	}
	clean := strings.ReplaceAll(result.Content, "\n", "")
	decoded, err := base64.StdEncoding.DecodeString(clean)
	if err != nil {
		return "", fmt.Errorf("decode base64 content: %w", err)
	}
	return string(decoded), nil
}
