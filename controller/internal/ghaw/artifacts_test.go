package ghaw

import (
	"archive/zip"
	"bytes"
	"os"
	"testing"

	"github.com/rancher/agentic-extension/controller/api/v1alpha1"
)

func TestNormaliseKind(t *testing.T) {
	cases := map[string]string{
		"create_pull_request":         v1alpha1.OutputKindPullRequest,
		"create_issue":                v1alpha1.OutputKindIssue,
		"add_comment":                 v1alpha1.OutputKindComment,
		"add_labels":                  v1alpha1.OutputKindLabel,
		"remove_labels":               v1alpha1.OutputKindLabel,
		"push_to_pull_request_branch": v1alpha1.OutputKindPush,
		"update_issue":                v1alpha1.OutputKindOther,
		"noop":                        v1alpha1.OutputKindOther,
	}
	for in, want := range cases {
		if got := NormaliseKind(in); got != want {
			t.Errorf("NormaliseKind(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseSafeOutputs(t *testing.T) {
	data := []byte(`{"type":"add_labels","number":19394,"repo":"rancher/dashboard","labelsAdded":["x"],"before_state":{"title":"Old title"}}
not json
{"type":"add_comment","url":"https://github.com/rancher/dashboard/issues/19379#issuecomment-1","number":19379,"repo":"rancher/dashboard"}

{"type":"create_pull_request","url":"https://github.com/rancher/dashboard/pull/19500","repo":"rancher/dashboard","title":"Add tests"}
`)
	out := ParseSafeOutputs(data)
	if len(out) != 3 {
		t.Fatalf("got %d outputs: %+v", len(out), out)
	}
	if out[0].Kind != v1alpha1.OutputKindLabel || out[0].Title != "Old title" || out[0].Number != 19394 {
		t.Errorf("label output = %+v", out[0])
	}
	if out[1].Kind != v1alpha1.OutputKindComment || out[1].State != "" {
		t.Errorf("comment output = %+v", out[1])
	}
	pr := out[2]
	if pr.Kind != v1alpha1.OutputKindPullRequest || pr.State != "open" || pr.Number != 19500 || pr.Title != "Add tests" {
		t.Errorf("pr output = %+v", pr)
	}
}

func TestParseFixtures(t *testing.T) {
	b, err := os.ReadFile("testdata/safe-output-items.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	out := ParseSafeOutputs(b)
	if len(out) == 0 {
		t.Fatal("no outputs parsed from fixture")
	}
	for _, o := range out {
		if o.Kind == "" || o.Type == "" {
			t.Errorf("unnormalised output %+v", o)
		}
	}

	ub, err := os.ReadFile("testdata/agent_usage.json")
	if err != nil {
		t.Fatal(err)
	}
	u, err := ParseUsage(ub)
	if err != nil {
		t.Fatal(err)
	}
	if u.InputTokens != 6348 || u.OutputTokens != 45345 || u.CacheReadTokens != 1215391 || u.CacheWriteTokens != 102573 ||
		u.AICredits != 96.56567 || u.PrimaryModel != "claude-sonnet-5" {
		t.Errorf("usage = %+v", u)
	}
}

func TestFileFromZip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("activity/summary.json")
	_, _ = w.Write([]byte("{}"))
	w, _ = zw.Create("agent_usage.json")
	_, _ = w.Write([]byte(`{"ai_credits":1.5}`))
	_ = zw.Close()

	b, err := FileFromZip(buf.Bytes(), UsageFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"ai_credits":1.5}` {
		t.Errorf("got %q", b)
	}
	if _, err := FileFromZip(buf.Bytes(), SafeOutputsFile); err == nil {
		t.Error("expected not found error")
	}
}
