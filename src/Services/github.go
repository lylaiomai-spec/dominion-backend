package Services

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type GitHubConfig struct {
	Token  string
	Owner  string
	Repo   string
	Branch string
}

type GitHubFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func GetGitHubConfig(db *sql.DB) (GitHubConfig, error) {
	cfg := GitHubConfig{}
	rows, err := db.Query(
		`SELECT setting_name, setting_value FROM global_settings
		 WHERE setting_name IN ('github_token','github_owner','github_repo','github_branch')`,
	)
	if err != nil {
		return cfg, err
	}
	defer rows.Close()
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			continue
		}
		switch name {
		case "github_token":
			cfg.Token = value
		case "github_owner":
			cfg.Owner = value
		case "github_repo":
			cfg.Repo = value
		case "github_branch":
			cfg.Branch = value
		}
	}
	if cfg.Token == "" || cfg.Owner == "" || cfg.Repo == "" || cfg.Branch == "" {
		return cfg, fmt.Errorf("github config incomplete: token, owner, repo, and branch are all required")
	}
	return cfg, nil
}

type GitHubError struct {
	StatusCode int
	Body       string
}

func (e *GitHubError) Error() string {
	return fmt.Sprintf("github API error %d: %s", e.StatusCode, e.Body)
}

var githubHTTPClient = &http.Client{Timeout: 15 * time.Second}

func githubRequest(method, url, token string, body any) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := githubHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, &GitHubError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}
	return respBody, nil
}

// GitHubGetFile fetches a file's decoded content from the repository.
func GitHubGetFile(cfg GitHubConfig, path string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s", cfg.Owner, cfg.Repo, path, cfg.Branch)
	data, err := githubRequest("GET", url, cfg.Token, nil)
	if err != nil {
		return "", err
	}
	var resp struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("parse file response: %w", err)
	}
	if resp.Encoding != "base64" {
		return "", fmt.Errorf("unexpected encoding: %s", resp.Encoding)
	}
	// GitHub wraps base64 content with newlines
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(resp.Content, "\n", ""))
	if err != nil {
		return "", fmt.Errorf("decode content: %w", err)
	}
	return string(decoded), nil
}

// GitHubCommit creates a single commit on the configured branch containing all provided files.
func GitHubCommit(cfg GitHubConfig, message string, files []GitHubFile) error {
	base := fmt.Sprintf("https://api.github.com/repos/%s/%s", cfg.Owner, cfg.Repo)

	// 1. Get current branch HEAD commit SHA.
	refData, err := githubRequest("GET", base+"/git/ref/heads/"+cfg.Branch, cfg.Token, nil)
	if err != nil {
		return fmt.Errorf("get ref: %w", err)
	}
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := json.Unmarshal(refData, &ref); err != nil {
		return fmt.Errorf("parse ref: %w", err)
	}
	headSHA := ref.Object.SHA

	// 2. Get the tree SHA of the HEAD commit.
	commitData, err := githubRequest("GET", base+"/git/commits/"+headSHA, cfg.Token, nil)
	if err != nil {
		return fmt.Errorf("get commit: %w", err)
	}
	var commit struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := json.Unmarshal(commitData, &commit); err != nil {
		return fmt.Errorf("parse commit: %w", err)
	}
	baseTreeSHA := commit.Tree.SHA

	// 3. Create a blob for each file.
	type treeEntry struct {
		Path string `json:"path"`
		Mode string `json:"mode"`
		Type string `json:"type"`
		SHA  string `json:"sha"`
	}
	entries := make([]treeEntry, 0, len(files))
	for _, f := range files {
		blobData, err := githubRequest("POST", base+"/git/blobs", cfg.Token, map[string]string{
			"content":  base64.StdEncoding.EncodeToString([]byte(f.Content)),
			"encoding": "base64",
		})
		if err != nil {
			return fmt.Errorf("create blob for %s: %w", f.Path, err)
		}
		var blob struct {
			SHA string `json:"sha"`
		}
		if err := json.Unmarshal(blobData, &blob); err != nil {
			return fmt.Errorf("parse blob for %s: %w", f.Path, err)
		}
		entries = append(entries, treeEntry{
			Path: f.Path,
			Mode: "100644",
			Type: "blob",
			SHA:  blob.SHA,
		})
	}

	// 4. Create a new tree on top of the base tree.
	newTreeData, err := githubRequest("POST", base+"/git/trees", cfg.Token, map[string]any{
		"base_tree": baseTreeSHA,
		"tree":      entries,
	})
	if err != nil {
		return fmt.Errorf("create tree: %w", err)
	}
	var newTree struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(newTreeData, &newTree); err != nil {
		return fmt.Errorf("parse tree: %w", err)
	}

	// 5. Create the commit.
	newCommitData, err := githubRequest("POST", base+"/git/commits", cfg.Token, map[string]any{
		"message": message,
		"tree":    newTree.SHA,
		"parents": []string{headSHA},
	})
	if err != nil {
		return fmt.Errorf("create commit: %w", err)
	}
	var newCommit struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(newCommitData, &newCommit); err != nil {
		return fmt.Errorf("parse new commit: %w", err)
	}

	// 6. Advance the branch ref.
	_, err = githubRequest("PATCH", base+"/git/refs/heads/"+cfg.Branch, cfg.Token, map[string]any{
		"sha": newCommit.SHA,
	})
	if err != nil {
		return fmt.Errorf("update ref: %w", err)
	}

	return nil
}
