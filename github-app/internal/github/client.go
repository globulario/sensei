package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const checkRunName = "Sensei Architectural Briefing"

// PullRequestFile is the immutable file metadata returned for one PR revision.
type PullRequestFile struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Changes   int    `json:"changes"`
}

// Client performs installation-scoped GitHub API operations.
type Client struct {
	auth       *Authenticator
	baseURL    string
	httpClient *http.Client
}

func NewClient(auth *Authenticator, baseURL string, httpClient *http.Client) (*Client, error) {
	if auth == nil {
		return nil, errors.New("GitHub authenticator is required")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		auth:       auth,
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}, nil
}

func (c *Client) ListPullRequestFiles(ctx context.Context, installationID int64, owner, repo string, number int) ([]PullRequestFile, error) {
	var all []PullRequestFile
	for page := 1; ; page++ {
		path := fmt.Sprintf("/repos/%s/%s/pulls/%d/files?per_page=100&page=%d", escape(owner), escape(repo), number, page)
		var batch []PullRequestFile
		if err := c.do(ctx, installationID, http.MethodGet, path, nil, &batch); err != nil {
			return nil, fmt.Errorf("list pull request files: %w", err)
		}
		all = append(all, batch...)
		if len(batch) < 100 {
			break
		}
	}
	return all, nil
}

// UpsertIssueComment updates the existing marker comment or creates it once.
func (c *Client) UpsertIssueComment(ctx context.Context, installationID int64, owner, repo string, number int, marker, body string) error {
	if marker == "" || !strings.Contains(body, marker) {
		return errors.New("sticky comment body must contain its marker")
	}

	for page := 1; ; page++ {
		path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments?per_page=100&page=%d", escape(owner), escape(repo), number, page)
		var comments []struct {
			ID   int64  `json:"id"`
			Body string `json:"body"`
		}
		if err := c.do(ctx, installationID, http.MethodGet, path, nil, &comments); err != nil {
			return fmt.Errorf("list pull request comments: %w", err)
		}
		for _, comment := range comments {
			if strings.Contains(comment.Body, marker) {
				updatePath := fmt.Sprintf("/repos/%s/%s/issues/comments/%d", escape(owner), escape(repo), comment.ID)
				if err := c.do(ctx, installationID, http.MethodPatch, updatePath, map[string]string{"body": body}, nil); err != nil {
					return fmt.Errorf("update Sensei briefing comment: %w", err)
				}
				return nil
			}
		}
		if len(comments) < 100 {
			break
		}
	}

	createPath := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", escape(owner), escape(repo), number)
	if err := c.do(ctx, installationID, http.MethodPost, createPath, map[string]string{"body": body}, nil); err != nil {
		return fmt.Errorf("create Sensei briefing comment: %w", err)
	}
	return nil
}

// UpsertCheckRun keeps one check run per PR head SHA and external identity.
func (c *Client) UpsertCheckRun(ctx context.Context, installationID int64, owner, repo, headSHA, externalID, summary, text string) error {
	query := url.Values{}
	query.Set("check_name", checkRunName)
	query.Set("per_page", "100")
	listPath := fmt.Sprintf("/repos/%s/%s/commits/%s/check-runs?%s", escape(owner), escape(repo), escape(headSHA), query.Encode())
	var existing struct {
		CheckRuns []struct {
			ID         int64  `json:"id"`
			ExternalID string `json:"external_id"`
		} `json:"check_runs"`
	}
	if err := c.do(ctx, installationID, http.MethodGet, listPath, nil, &existing); err != nil {
		return fmt.Errorf("list Sensei check runs: %w", err)
	}

	completedAt := time.Now().UTC().Format(time.RFC3339)
	payload := map[string]any{
		"name":         checkRunName,
		"external_id":  externalID,
		"status":       "completed",
		"conclusion":   "neutral",
		"completed_at": completedAt,
		"output": map[string]string{
			"title":   "Sensei architectural briefing",
			"summary": summary,
			"text":    text,
		},
	}

	for _, checkRun := range existing.CheckRuns {
		if checkRun.ExternalID == externalID {
			path := fmt.Sprintf("/repos/%s/%s/check-runs/%d", escape(owner), escape(repo), checkRun.ID)
			if err := c.do(ctx, installationID, http.MethodPatch, path, payload, nil); err != nil {
				return fmt.Errorf("update Sensei check run: %w", err)
			}
			return nil
		}
	}

	payload["head_sha"] = headSHA
	path := fmt.Sprintf("/repos/%s/%s/check-runs", escape(owner), escape(repo))
	if err := c.do(ctx, installationID, http.MethodPost, path, payload, nil); err != nil {
		return fmt.Errorf("create Sensei check run: %w", err)
	}
	return nil
}

func (c *Client) do(ctx context.Context, installationID int64, method, path string, input, output any) error {
	token, err := c.auth.InstallationToken(ctx, installationID)
	if err != nil {
		return err
	}

	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("encode GitHub request body: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("create GitHub API request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", "sensei-github-app")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub API request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return fmt.Errorf("GitHub API returned %s for %s %s: %s", resp.Status, method, path, strings.TrimSpace(string(responseBody)))
	}
	if output == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(output); err != nil {
		return fmt.Errorf("decode GitHub API response: %w", err)
	}
	return nil
}

func escape(value string) string {
	return url.PathEscape(value)
}

func checkExternalID(prNumber int, headSHA string) string {
	return "sensei-pr-" + strconv.Itoa(prNumber) + "-" + headSHA
}
