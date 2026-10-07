// Package github is a minimal GitHub REST client covering what the controller needs.
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

// DefaultBaseURL is the public GitHub API endpoint.
const DefaultBaseURL = "https://api.github.com"

// RateLimit is parsed from X-RateLimit-* response headers.
type RateLimit struct {
	Limit     int
	Remaining int
	Reset     time.Time
}

// APIError is returned for non-2xx responses.
type APIError struct {
	StatusCode int
	Method     string
	URL        string
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("github %s %s: %d %s", e.Method, e.URL, e.StatusCode, e.Message)
}

// IsNotFound reports whether err is a GitHub 404.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// Client talks to the GitHub REST API with a token.
type Client struct {
	BaseURL     string
	Token       string
	HTTP        *http.Client
	OnRateLimit func(RateLimit)
}

// NewClient returns a client for baseURL (DefaultBaseURL when empty).
func NewClient(baseURL, token string, onRateLimit func(RateLimit)) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		Token:       token,
		OnRateLimit: onRateLimit,
		HTTP: &http.Client{
			Timeout: 60 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return errors.New("too many redirects")
				}
				// Never forward the token to another host (artifact downloads redirect to blob storage).
				if len(via) > 0 && req.URL.Host != via[0].URL.Host {
					req.Header.Del("Authorization")
				}
				return nil
			},
		},
	}
}

func (c *Client) newRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	u := path
	if !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
		u = c.BaseURL + path
	}
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "rancher-agentic-controller")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (c *Client) recordRateLimit(resp *http.Response) {
	if c.OnRateLimit == nil {
		return
	}
	h := resp.Header
	if h.Get("X-RateLimit-Remaining") == "" {
		return
	}
	rl := RateLimit{}
	rl.Limit, _ = strconv.Atoi(h.Get("X-RateLimit-Limit"))
	rl.Remaining, _ = strconv.Atoi(h.Get("X-RateLimit-Remaining"))
	if s, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		rl.Reset = time.Unix(s, 0).UTC()
	}
	c.OnRateLimit(rl)
}

// do performs the request and returns the body. Non-2xx statuses become *APIError.
func (c *Client) do(req *http.Request) (int, []byte, error) {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	c.recordRateLimit(resp)
	b, err := io.ReadAll(io.LimitReader(resp.Body, 200<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := strings.TrimSpace(string(b))
		var e struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(b, &e) == nil && e.Message != "" {
			msg = e.Message
		}
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return resp.StatusCode, b, &APIError{StatusCode: resp.StatusCode, Method: req.Method, URL: req.URL.Path, Message: msg}
	}
	return resp.StatusCode, b, nil
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	req, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	_, b, err := c.do(req)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// Repository is the subset of GET /repos/{o}/{r} we use.
type Repository struct {
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	HTMLURL       string `json:"html_url"`
}

// GetRepository fetches repository metadata.
func (c *Client) GetRepository(ctx context.Context, repo string) (*Repository, error) {
	var r Repository
	if err := c.getJSON(ctx, "/repos/"+repo, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ContentEntry is one item of a directory listing.
type ContentEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	SHA  string `json:"sha"`
	Type string `json:"type"`
}

func escapePath(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

// ListDirectory lists a directory at ref.
func (c *Client) ListDirectory(ctx context.Context, repo, path, ref string) ([]ContentEntry, error) {
	var out []ContentEntry
	p := fmt.Sprintf("/repos/%s/contents/%s?ref=%s", repo, escapePath(path), url.QueryEscape(ref))
	if err := c.getJSON(ctx, p, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetFileRaw returns the raw content of a file at ref.
func (c *Client) GetFileRaw(ctx context.Context, repo, path, ref string) ([]byte, error) {
	p := fmt.Sprintf("/repos/%s/contents/%s?ref=%s", repo, escapePath(path), url.QueryEscape(ref))
	req, err := c.newRequest(ctx, http.MethodGet, p, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.raw+json")
	_, b, err := c.do(req)
	return b, err
}

// Workflow is the subset of a workflow object we use.
type Workflow struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	State   string `json:"state"`
	HTMLURL string `json:"html_url"`
}

// GetWorkflow fetches a workflow by id or file basename.
func (c *Client) GetWorkflow(ctx context.Context, repo, idOrFile string) (*Workflow, error) {
	var w Workflow
	if err := c.getJSON(ctx, fmt.Sprintf("/repos/%s/actions/workflows/%s", repo, url.PathEscape(idOrFile)), &w); err != nil {
		return nil, err
	}
	return &w, nil
}

// Actor is a GitHub user.
type Actor struct {
	Login string `json:"login"`
}

// WorkflowRun is the subset of a workflow run we use.
type WorkflowRun struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	DisplayTitle string     `json:"display_title"`
	RunNumber    int64      `json:"run_number"`
	RunAttempt   int        `json:"run_attempt"`
	Event        string     `json:"event"`
	Status       string     `json:"status"`
	Conclusion   string     `json:"conclusion"`
	HeadBranch   string     `json:"head_branch"`
	HTMLURL      string     `json:"html_url"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	RunStartedAt *time.Time `json:"run_started_at"`
	Actor        *Actor     `json:"actor"`
	WorkflowID   int64      `json:"workflow_id"`
}

// RunListOptions filters ListWorkflowRuns.
type RunListOptions struct {
	CreatedSince time.Time // date granularity
	Event        string
	MaxPages     int
}

// ListWorkflowRuns lists runs of a workflow, newest first, following pagination.
func (c *Client) ListWorkflowRuns(ctx context.Context, repo string, workflowID int64, opts RunListOptions) ([]WorkflowRun, error) {
	maxPages := opts.MaxPages
	if maxPages <= 0 {
		maxPages = 10
	}
	var all []WorkflowRun
	for page := 1; page <= maxPages; page++ {
		q := url.Values{}
		q.Set("per_page", "100")
		q.Set("page", strconv.Itoa(page))
		if !opts.CreatedSince.IsZero() {
			q.Set("created", ">="+opts.CreatedSince.UTC().Format("2006-01-02"))
		}
		if opts.Event != "" {
			q.Set("event", opts.Event)
		}
		var resp struct {
			TotalCount   int           `json:"total_count"`
			WorkflowRuns []WorkflowRun `json:"workflow_runs"`
		}
		if err := c.getJSON(ctx, fmt.Sprintf("/repos/%s/actions/workflows/%d/runs?%s", repo, workflowID, q.Encode()), &resp); err != nil {
			return nil, err
		}
		if len(resp.WorkflowRuns) == 0 && len(all) < resp.TotalCount {
			// The runs endpoint is search-backed and occasionally returns empty pages; never
			// treat that as "no runs", or the import window would move past them.
			return nil, fmt.Errorf("incomplete run list for workflow %d: page %d empty, total_count %d", workflowID, page, resp.TotalCount)
		}
		all = append(all, resp.WorkflowRuns...)
		if len(resp.WorkflowRuns) < 100 || len(all) >= resp.TotalCount {
			break
		}
	}
	return all, nil
}

// GetRun fetches a workflow run.
func (c *Client) GetRun(ctx context.Context, repo string, runID int64) (*WorkflowRun, error) {
	var r WorkflowRun
	if err := c.getJSON(ctx, fmt.Sprintf("/repos/%s/actions/runs/%d", repo, runID), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// Artifact is a workflow run artifact.
type Artifact struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	Expired            bool   `json:"expired"`
	ArchiveDownloadURL string `json:"archive_download_url"`
	SizeInBytes        int64  `json:"size_in_bytes"`
}

// ListRunArtifacts lists artifacts of a run.
func (c *Client) ListRunArtifacts(ctx context.Context, repo string, runID int64) ([]Artifact, error) {
	var resp struct {
		Artifacts []Artifact `json:"artifacts"`
	}
	if err := c.getJSON(ctx, fmt.Sprintf("/repos/%s/actions/runs/%d/artifacts?per_page=100", repo, runID), &resp); err != nil {
		return nil, err
	}
	return resp.Artifacts, nil
}

// DownloadArtifact downloads an artifact zip, following the redirect without forwarding the token.
func (c *Client) DownloadArtifact(ctx context.Context, a Artifact) ([]byte, error) {
	req, err := c.newRequest(ctx, http.MethodGet, a.ArchiveDownloadURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "*/*")
	_, b, err := c.do(req)
	return b, err
}

// DispatchResult is returned by DispatchWorkflow.
type DispatchResult struct {
	// RunID is 0 when GitHub answered 204 without run details.
	RunID   int64  `json:"workflow_run_id"`
	RunURL  string `json:"run_url"`
	HTMLURL string `json:"html_url"`
}

// DispatchWorkflow triggers a workflow_dispatch event.
func (c *Client) DispatchWorkflow(ctx context.Context, repo string, workflowID int64, ref string, inputs map[string]string) (*DispatchResult, error) {
	body := map[string]any{"ref": ref, "return_run_details": true}
	if len(inputs) > 0 {
		body["inputs"] = inputs
	}
	req, err := c.newRequest(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/actions/workflows/%d/dispatches", repo, workflowID), body)
	if err != nil {
		return nil, err
	}
	status, b, err := c.do(req)
	if err != nil {
		return nil, err
	}
	res := &DispatchResult{}
	if status == http.StatusOK && len(bytes.TrimSpace(b)) > 0 {
		if err := json.Unmarshal(b, res); err != nil {
			return nil, fmt.Errorf("decoding dispatch response: %w", err)
		}
	}
	return res, nil
}

// PullRequest is the subset of a pull request we use.
type PullRequest struct {
	Number int64  `json:"number"`
	State  string `json:"state"`
	Merged bool   `json:"merged"`
	Title  string `json:"title"`
}

// GetPullRequest fetches a pull request. repo is owner/name.
func (c *Client) GetPullRequest(ctx context.Context, repo string, number int64) (*PullRequest, error) {
	var pr PullRequest
	if err := c.getJSON(ctx, fmt.Sprintf("/repos/%s/pulls/%d", repo, number), &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// Job is the subset of a workflow job we use.
type Job struct {
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	Conclusion  string     `json:"conclusion"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

// ListRunJobs lists the jobs of the latest attempt of a run.
func (c *Client) ListRunJobs(ctx context.Context, repo string, runID int64) ([]Job, error) {
	var resp struct {
		Jobs []Job `json:"jobs"`
	}
	if err := c.getJSON(ctx, fmt.Sprintf("/repos/%s/actions/runs/%d/jobs?per_page=100&filter=latest", repo, runID), &resp); err != nil {
		return nil, err
	}
	return resp.Jobs, nil
}
