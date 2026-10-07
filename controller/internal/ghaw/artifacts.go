package ghaw

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/rancher/agentic-extension/controller/api/v1alpha1"
)

// Artifact and file names produced by gh-aw.
const (
	UsageArtifact       = "usage"
	UsageFile           = "agent_usage.json"
	SafeOutputsArtifact = "safe-outputs-items"
	SafeOutputsFile     = "safe-output-items.jsonl"
)

// UsageReport is agent_usage.json.
type UsageReport struct {
	InputTokens      int64   `json:"input_tokens"`
	OutputTokens     int64   `json:"output_tokens"`
	CacheReadTokens  int64   `json:"cache_read_tokens"`
	CacheWriteTokens int64   `json:"cache_write_tokens"`
	AICredits        float64 `json:"ai_credits"`
	PrimaryModel     string  `json:"primary_model"`
}

// ToUsage converts to the API type.
func (u UsageReport) ToUsage() *v1alpha1.Usage {
	return &v1alpha1.Usage{
		InputTokens:      u.InputTokens,
		OutputTokens:     u.OutputTokens,
		CacheReadTokens:  u.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens,
		AICredits:        u.AICredits,
	}
}

// ParseUsage decodes agent_usage.json.
func ParseUsage(b []byte) (*UsageReport, error) {
	var u UsageReport
	if err := json.Unmarshal(b, &u); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", UsageFile, err)
	}
	return &u, nil
}

// FileFromZip returns the first file in the zip whose base name is name.
func FileFromZip(zipData []byte, name string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("opening artifact zip: %w", err)
	}
	for _, f := range zr.File {
		if path.Base(f.Name) != name || f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(io.LimitReader(rc, 50<<20))
	}
	return nil, fmt.Errorf("%s not found in artifact", name)
}

type safeOutputItem struct {
	Type        string `json:"type"`
	URL         string `json:"url"`
	Number      int64  `json:"number"`
	Repo        string `json:"repo"`
	Title       string `json:"title"`
	BeforeState *struct {
		Title string `json:"title"`
	} `json:"before_state"`
}

// NormaliseKind maps a gh-aw item type to an Output kind.
func NormaliseKind(itemType string) string {
	switch itemType {
	case "create_pull_request":
		return v1alpha1.OutputKindPullRequest
	case "create_issue":
		return v1alpha1.OutputKindIssue
	case "add_comment":
		return v1alpha1.OutputKindComment
	case "add_labels", "remove_labels":
		return v1alpha1.OutputKindLabel
	case "push_to_pull_request_branch":
		return v1alpha1.OutputKindPush
	}
	return v1alpha1.OutputKindOther
}

var numberInURL = regexp.MustCompile(`/(?:pull|issues)/(\d+)`)

func numberFromURL(u string) int64 {
	m := numberInURL.FindStringSubmatch(u)
	if m == nil {
		return 0
	}
	n, _ := strconv.ParseInt(m[1], 10, 64)
	return n
}

// ParseSafeOutputs decodes safe-output-items.jsonl. Malformed lines are skipped.
func ParseSafeOutputs(b []byte) []v1alpha1.Output {
	var out []v1alpha1.Output
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 64*1024), 10<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var it safeOutputItem
		if err := json.Unmarshal([]byte(line), &it); err != nil || it.Type == "" {
			continue
		}
		o := v1alpha1.Output{
			Type:   it.Type,
			Kind:   NormaliseKind(it.Type),
			Repo:   it.Repo,
			Number: it.Number,
			URL:    it.URL,
			Title:  it.Title,
		}
		if o.Number == 0 {
			o.Number = numberFromURL(o.URL)
		}
		if o.Title == "" && it.BeforeState != nil {
			o.Title = it.BeforeState.Title
		}
		if o.Kind == v1alpha1.OutputKindPullRequest {
			o.State = "open"
		}
		out = append(out, o)
	}
	return out
}
