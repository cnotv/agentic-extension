package controllers

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/rancher/agentic-extension/controller/api/v1alpha1"
)

const (
	ns        = "agentic-demo"
	repoName  = "acme/widgets"
	testToken = "test-token"
)

// fakeGitHub is a tiny in-memory GitHub API.
type fakeGitHub struct {
	t           *testing.T
	api         *httptest.Server
	blob        *httptest.Server
	mu          sync.Mutex
	dispatches  int
	dispatch204 bool
	runs        map[int64]map[string]any
	listRuns    []map[string]any
	jobs        map[int64][]map[string]any
	jobCalls    int
	blobAuth    []string
}

func zipOf(name, content string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create(name)
	_, _ = w.Write([]byte(content))
	_ = zw.Close()
	return buf.Bytes()
}

const dispatchableMD = `---
description: Improves tests
on:
  schedule: daily
  workflow_dispatch:
engine: copilot
safe-outputs:
  create-pull-request:
---
body
`

const dispatchableLock = `name: "Test Improver"
on:
  workflow_dispatch:
    inputs:
      aw_context:
        type: string
      focus:
        description: Area to focus on
        type: string
`

const triageMD = `  ---
  on:
    issues:
      types: [opened]
    reaction: eyes
  safe-outputs:
    add-labels:
  ---
`

func newFakeGitHub(t *testing.T) *fakeGitHub {
	f := &fakeGitHub{t: t, runs: map[int64]map[string]any{}, jobs: map[int64][]map[string]any{}}
	f.blob = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.blobAuth = append(f.blobAuth, r.Header.Get("Authorization"))
		f.mu.Unlock()
		switch r.URL.Path {
		case "/usage.zip":
			_, _ = w.Write(zipOf("agent_usage.json", `{"input_tokens":100,"output_tokens":200,"cache_read_tokens":300,"cache_write_tokens":400,"ai_credits":12.5,"primary_model":"claude-sonnet-5"}`))
		case "/outputs.zip":
			_, _ = w.Write(zipOf("safe-output-items.jsonl",
				`{"type":"create_pull_request","url":"https://github.com/acme/widgets/pull/42","number":42,"repo":"acme/widgets","title":"More tests"}`+"\n"+
					`{"type":"add_comment","url":"https://github.com/acme/widgets/issues/7#issuecomment-1","number":7,"repo":"acme/widgets"}`+"\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	f.api = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(func() { f.api.Close(); f.blob.Close() })
	return f
}

func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+testToken {
		http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
		return
	}
	w.Header().Set("X-RateLimit-Limit", "5000")
	w.Header().Set("X-RateLimit-Remaining", "4321")
	w.Header().Set("X-RateLimit-Reset", fmt.Sprint(time.Now().Add(time.Hour).Unix()))
	p := r.URL.Path
	writeJSON := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	base := "/repos/" + repoName
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case p == base:
		writeJSON(map[string]any{"full_name": repoName, "default_branch": "main", "html_url": "https://github.com/" + repoName})
	case p == base+"/contents/.github/workflows":
		writeJSON([]map[string]any{
			{"name": "test-improver.md", "path": ".github/workflows/test-improver.md", "sha": "a1", "type": "file"},
			{"name": "test-improver.lock.yml", "path": ".github/workflows/test-improver.lock.yml", "sha": "a2", "type": "file"},
			{"name": "triage.md", "path": ".github/workflows/triage.md", "sha": "b1", "type": "file"},
			{"name": "triage.lock.yml", "path": ".github/workflows/triage.lock.yml", "sha": "b2", "type": "file"},
			{"name": "README.md", "path": ".github/workflows/README.md", "sha": "c1", "type": "file"},
			{"name": "ci.yml", "path": ".github/workflows/ci.yml", "sha": "d1", "type": "file"},
		})
	case p == base+"/contents/.github/workflows/test-improver.md":
		_, _ = w.Write([]byte(dispatchableMD))
	case p == base+"/contents/.github/workflows/test-improver.lock.yml":
		_, _ = w.Write([]byte(dispatchableLock))
	case p == base+"/contents/.github/workflows/triage.md":
		_, _ = w.Write([]byte(triageMD))
	case p == base+"/contents/.github/workflows/triage.lock.yml":
		_, _ = w.Write([]byte("name: Triage\n"))
	case p == base+"/actions/workflows/test-improver.lock.yml":
		writeJSON(map[string]any{"id": 101, "name": "Test Improver", "state": "active", "html_url": "https://github.com/acme/widgets/blob/main/.github/workflows/test-improver.lock.yml"})
	case p == base+"/actions/workflows/triage.lock.yml":
		writeJSON(map[string]any{"id": 102, "name": "Triage", "state": "disabled_manually"})
	case p == base+"/actions/workflows/101/runs" || p == base+"/actions/workflows/102/runs":
		runs := f.listRuns
		if ev := r.URL.Query().Get("event"); ev != "" {
			var filtered []map[string]any
			for _, run := range runs {
				if run["event"] == ev {
					filtered = append(filtered, run)
				}
			}
			runs = filtered
		}
		if strings.HasSuffix(p, "/102/runs") {
			runs = nil
		}
		writeJSON(map[string]any{"total_count": len(runs), "workflow_runs": runs})
	case p == base+"/actions/workflows/101/dispatches" && r.Method == http.MethodPost:
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["ref"] != "main" || body["return_run_details"] != true {
			f.t.Errorf("unexpected dispatch body %v", body)
		}
		f.dispatches++
		if f.dispatch204 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(map[string]any{"workflow_run_id": 555, "run_url": f.api.URL + base + "/actions/runs/555", "html_url": "https://github.com/acme/widgets/actions/runs/555"})
	case strings.HasPrefix(p, base+"/actions/runs/") && strings.HasSuffix(p, "/jobs"):
		f.jobCalls++
		var id int64
		_, _ = fmt.Sscanf(strings.TrimPrefix(p, base+"/actions/runs/"), "%d", &id)
		jobs, ok := f.jobs[id]
		if !ok {
			jobs = agentRanJobs(time.Date(2026, 10, 6, 20, 0, 0, 0, time.UTC), 5*time.Minute)
		}
		writeJSON(map[string]any{"total_count": len(jobs), "jobs": jobs})
	case strings.HasPrefix(p, base+"/actions/runs/") && strings.HasSuffix(p, "/artifacts"):
		writeJSON(map[string]any{"artifacts": []map[string]any{
			{"id": 1, "name": "usage", "expired": false, "archive_download_url": f.api.URL + "/download/usage"},
			{"id": 2, "name": "safe-outputs-items", "expired": false, "archive_download_url": f.api.URL + "/download/outputs"},
			{"id": 3, "name": "agent", "expired": false, "archive_download_url": f.api.URL + "/download/agent"},
		}})
	case strings.HasPrefix(p, base+"/actions/runs/"):
		var id int64
		_, _ = fmt.Sscanf(strings.TrimPrefix(p, base+"/actions/runs/"), "%d", &id)
		run, ok := f.runs[id]
		if !ok {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			return
		}
		writeJSON(run)
	case p == "/download/usage":
		http.Redirect(w, r, f.blob.URL+"/usage.zip", http.StatusFound)
	case p == "/download/outputs":
		http.Redirect(w, r, f.blob.URL+"/outputs.zip", http.StatusFound)
	case p == base+"/pulls/42":
		writeJSON(map[string]any{"number": 42, "state": "closed", "merged": true, "title": "More tests"})
	default:
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	}
}

func ghRun(id int64, event, status, conclusion string, created time.Time) map[string]any {
	return map[string]any{
		"id": id, "name": "Test Improver", "run_number": id % 1000, "run_attempt": 1, "event": event,
		"status": status, "conclusion": conclusion, "head_branch": "main",
		"html_url":   fmt.Sprintf("https://github.com/acme/widgets/actions/runs/%d", id),
		"created_at": created.Format(time.RFC3339), "updated_at": created.Add(10 * time.Minute).Format(time.RFC3339),
		"run_started_at": created.Format(time.RFC3339), "actor": map[string]any{"login": "octocat"}, "workflow_id": 101,
	}
}

func job(name, conclusion string, start time.Time, d time.Duration) map[string]any {
	return map[string]any{"name": name, "status": "completed", "conclusion": conclusion,
		"started_at": start.Format(time.RFC3339), "completed_at": start.Add(d).Format(time.RFC3339)}
}

// agentRanJobs: activation 1m, agent d, conclusion 1m, and one skipped job far in the future.
func agentRanJobs(start time.Time, d time.Duration) []map[string]any {
	return []map[string]any{
		job("activation", "success", start, time.Minute),
		job("agent", "success", start.Add(time.Minute), d),
		job("conclusion", "success", start.Add(time.Minute+d), time.Minute),
		job("push_repo_memory", "skipped", start.Add(10*time.Hour), 0),
	}
}

// gateOnlyJobs: only the pre-activation gate ran.
func gateOnlyJobs(start time.Time) []map[string]any {
	return []map[string]any{
		job("pre_activation", "success", start, 30*time.Second),
		job("activation", "skipped", start.Add(30*time.Second), 0),
		job("agent", "skipped", start.Add(30*time.Second), 0),
	}
}

func testScheme(t *testing.T) *runtime.Scheme {
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func baseObjects() []client.Object {
	return []client.Object{
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "github-token", Namespace: ns}, Data: map[string][]byte{"token": []byte(testToken)}},
		&v1alpha1.AgentRepository{
			ObjectMeta: metav1.ObjectMeta{Name: "widgets", Namespace: ns, Generation: 1},
			Spec:       v1alpha1.AgentRepositorySpec{Repository: repoName, SecretRef: v1alpha1.RepositorySecretRef{Name: "github-token"}, SyncInterval: "10m"},
			Status:     v1alpha1.AgentRepositoryStatus{DefaultBranch: "main"},
		},
	}
}

func newClient(t *testing.T, objs ...client.Object) client.Client {
	return fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(objs...).
		WithStatusSubresource(&v1alpha1.AgentRepository{}, &v1alpha1.Agent{}, &v1alpha1.AgentRun{}).Build()
}

func newRunReconciler(c client.Client, gh *fakeGitHub, now *time.Time) *AgentRunReconciler {
	return &AgentRunReconciler{
		Client: c, APIReader: c, Recorder: record.NewFakeRecorder(100),
		GitHub:  &GitHubFactory{BaseURL: gh.api.URL, Reader: c, RateLimits: NewRateLimits()},
		Limiter: NewCollectionLimiter(60),
		Now:     func() time.Time { return *now },
	}
}

func dispatchableAgent() *v1alpha1.Agent {
	return &v1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "widgets-test-improver", Namespace: ns},
		Spec: v1alpha1.AgentSpec{RepositoryRef: "widgets", Repository: repoName, WorkflowID: 101, Dispatchable: true,
			DisplayName: "Test Improver"},
	}
}

func reconcileRun(t *testing.T, r *AgentRunReconciler, name string) ctrl.Result {
	t.Helper()
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: name}})
	if err != nil {
		t.Fatalf("reconcile %s: %v", name, err)
	}
	return res
}

func getRun(t *testing.T, c client.Client, name string) *v1alpha1.AgentRun {
	t.Helper()
	var run v1alpha1.AgentRun
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, &run); err != nil {
		t.Fatal(err)
	}
	return &run
}

func getAgent(t *testing.T, c client.Client, name string) *v1alpha1.Agent {
	t.Helper()
	var a v1alpha1.Agent
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, &a); err != nil {
		t.Fatal(err)
	}
	return &a
}

func TestAgentRunDispatchPollCollectCount(t *testing.T) {
	gh := newFakeGitHub(t)
	now := time.Now().UTC()
	gh.runs[555] = ghRun(555, "workflow_dispatch", "in_progress", "", now)
	run := &v1alpha1.AgentRun{
		ObjectMeta: metav1.ObjectMeta{Name: "manual-1", Namespace: ns},
		Spec:       v1alpha1.AgentRunSpec{AgentRef: "widgets-test-improver", Origin: v1alpha1.OriginRancher},
	}
	c := newClient(t, append(baseObjects(), dispatchableAgent(), run)...)
	r := newRunReconciler(c, gh, &now)

	// 1. dispatch
	reconcileRun(t, r, "manual-1")
	got := getRun(t, c, "manual-1")
	if got.Status.Phase != v1alpha1.RunPhaseDispatched || got.Status.GitHubRunID != 555 || got.Status.DispatchTime == nil {
		t.Fatalf("after dispatch: %+v", got.Status)
	}
	if got.Labels[v1alpha1.LabelAgent] != "widgets-test-improver" || got.Labels[v1alpha1.LabelOrigin] != "rancher" {
		t.Errorf("labels = %v", got.Labels)
	}

	// 2. poll while running
	res := reconcileRun(t, r, "manual-1")
	if got = getRun(t, c, "manual-1"); got.Status.Phase != v1alpha1.RunPhaseRunning || res.RequeueAfter != PollInterval {
		t.Fatalf("after poll: phase=%s requeue=%v", got.Status.Phase, res.RequeueAfter)
	}

	// 3. completes: collect + count
	gh.mu.Lock()
	gh.runs[555] = ghRun(555, "workflow_dispatch", "completed", "success", now)
	gh.mu.Unlock()
	now = now.Add(11 * time.Minute)
	reconcileRun(t, r, "manual-1")
	got = getRun(t, c, "manual-1")
	if got.Status.Phase != v1alpha1.RunPhaseSucceeded || got.Status.UsageState != v1alpha1.UsageCollected || !got.Status.Counted {
		t.Fatalf("after completion: %+v", got.Status)
	}
	if got.Status.DurationSeconds != 420 {
		t.Errorf("duration = %d, want 420 (from jobs)", got.Status.DurationSeconds)
	}
	if got.Status.Usage == nil || got.Status.Usage.AICredits != 12.5 || got.Status.Model != "claude-sonnet-5" {
		t.Errorf("usage = %+v model=%q", got.Status.Usage, got.Status.Model)
	}
	if len(got.Status.Outputs) != 2 || got.Status.Outputs[0].Kind != v1alpha1.OutputKindPullRequest || got.Status.Outputs[0].State != "open" {
		t.Errorf("outputs = %+v", got.Status.Outputs)
	}
	agent := getAgent(t, c, "widgets-test-improver")
	if agent.Status.Totals == nil || agent.Status.Totals.Runs != 1 || agent.Status.Totals.Succeeded != 1 ||
		agent.Status.Totals.PullRequests != 1 || agent.Status.Totals.Comments != 1 || agent.Status.Totals.AICredits != 12.5 ||
		len(agent.Status.Daily) != 1 || agent.Status.LastRun == nil || agent.Status.LastRun.Name != "manual-1" {
		t.Errorf("agent status = %+v", agent.Status)
	}

	// 4. PR refresh: merged
	reconcileRun(t, r, "manual-1")
	got = getRun(t, c, "manual-1")
	if got.Status.Outputs[0].State != "merged" {
		t.Errorf("pr state = %q", got.Status.Outputs[0].State)
	}
	agent = getAgent(t, c, "widgets-test-improver")
	if agent.Status.Totals.PullRequestsMerged != 1 || agent.Status.Daily[0].PullRequestsMerged != 1 || agent.Status.Totals.Runs != 1 {
		t.Errorf("after merge: totals = %+v", agent.Status.Totals)
	}

	// 5. nothing left: no more GitHub work, never re-dispatched, never re-counted
	res = reconcileRun(t, r, "manual-1")
	if res.RequeueAfter != 0 {
		t.Errorf("expected no requeue, got %v", res.RequeueAfter)
	}
	if gh.dispatches != 1 {
		t.Errorf("dispatches = %d, want 1", gh.dispatches)
	}
	if getAgent(t, c, "widgets-test-improver").Status.Totals.Runs != 1 {
		t.Error("run counted twice")
	}
	for _, auth := range gh.blobAuth {
		if auth != "" {
			t.Errorf("token leaked to artifact storage: %q", auth)
		}
	}
	if len(gh.blobAuth) != 2 {
		t.Errorf("blob downloads = %d", len(gh.blobAuth))
	}
}

func TestAgentRunDispatch204FallsBackToRunLookup(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.dispatch204 = true
	now := time.Now().UTC().Truncate(time.Second)
	run := &v1alpha1.AgentRun{
		ObjectMeta: metav1.ObjectMeta{Name: "manual-2", Namespace: ns},
		Spec:       v1alpha1.AgentRunSpec{AgentRef: "widgets-test-improver"},
	}
	// Another AgentRun already claimed run 700.
	other := &v1alpha1.AgentRun{
		ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: ns},
		Spec:       v1alpha1.AgentRunSpec{AgentRef: "widgets-test-improver", Origin: "github", GitHubRunID: 700},
	}
	c := newClient(t, append(baseObjects(), dispatchableAgent(), run, other)...)
	r := newRunReconciler(c, gh, &now)

	reconcileRun(t, r, "manual-2")
	got := getRun(t, c, "manual-2")
	if got.Status.Phase != v1alpha1.RunPhaseDispatched || got.Status.GitHubRunID != 0 {
		t.Fatalf("after 204 dispatch: %+v", got.Status)
	}

	// Run not visible yet: keep waiting, do not dispatch again.
	reconcileRun(t, r, "manual-2")
	if gh.dispatches != 1 {
		t.Fatalf("dispatches = %d", gh.dispatches)
	}

	gh.mu.Lock()
	gh.listRuns = []map[string]any{
		ghRun(700, "workflow_dispatch", "queued", "", now.Add(2*time.Second)),
		ghRun(701, "workflow_dispatch", "queued", "", now.Add(time.Second)),
		ghRun(650, "workflow_dispatch", "completed", "success", now.Add(-time.Hour)),
		ghRun(702, "schedule", "queued", "", now.Add(time.Second)),
	}
	gh.mu.Unlock()
	reconcileRun(t, r, "manual-2")
	got = getRun(t, c, "manual-2")
	if got.Status.GitHubRunID != 701 || got.Status.Phase != v1alpha1.RunPhaseQueued {
		t.Fatalf("lookup picked %d phase %s", got.Status.GitHubRunID, got.Status.Phase)
	}
	if gh.dispatches != 1 {
		t.Errorf("dispatches = %d", gh.dispatches)
	}
}

func TestAgentRunNotDispatchable(t *testing.T) {
	gh := newFakeGitHub(t)
	now := time.Now()
	agent := dispatchableAgent()
	agent.Spec.Dispatchable = false
	run := &v1alpha1.AgentRun{
		ObjectMeta: metav1.ObjectMeta{Name: "manual-3", Namespace: ns},
		Spec:       v1alpha1.AgentRunSpec{AgentRef: agent.Name, Origin: v1alpha1.OriginRancher},
	}
	c := newClient(t, append(baseObjects(), agent, run)...)
	reconcileRun(t, newRunReconciler(c, gh, &now), "manual-3")
	if got := getRun(t, c, "manual-3"); got.Status.Phase != v1alpha1.RunPhaseError {
		t.Fatalf("phase = %s", got.Status.Phase)
	}
	if gh.dispatches != 0 {
		t.Error("must not dispatch")
	}
}

func TestRepositorySyncDiscoversAgentsAndImportsRuns(t *testing.T) {
	gh := newFakeGitHub(t)
	now := time.Now().UTC()
	gh.listRuns = []map[string]any{
		ghRun(901, "schedule", "completed", "success", now.Add(-2*time.Hour)),
		ghRun(902, "issue_comment", "completed", "skipped", now.Add(-time.Hour)),
		ghRun(903, "schedule", "in_progress", "", now.Add(-time.Minute)),
		ghRun(904, "workflow_dispatch", "completed", "failure", now.Add(-3*time.Hour)),
		ghRun(905, "pull_request", "completed", "success", now.Add(-4*time.Hour)),
		ghRun(906, "pull_request", "completed", "action_required", now.Add(-5*time.Hour)),
	}
	gh.jobs[905] = gateOnlyJobs(now.Add(-4 * time.Hour))
	existingRancherRun := &v1alpha1.AgentRun{
		ObjectMeta: metav1.ObjectMeta{Name: "manual-run", Namespace: ns},
		Spec:       v1alpha1.AgentRunSpec{AgentRef: "widgets-test-improver", Origin: v1alpha1.OriginRancher},
		Status:     v1alpha1.AgentRunStatus{GitHubRunID: 904, Phase: v1alpha1.RunPhaseFailed},
	}
	stale := &v1alpha1.Agent{ObjectMeta: metav1.ObjectMeta{Name: "widgets-removed", Namespace: ns,
		Labels: map[string]string{v1alpha1.LabelRepository: "widgets"}}}
	c := newClient(t, append(baseObjects(), existingRancherRun, stale)...)
	rl := NewRateLimits()
	r := &AgentRepositoryReconciler{
		Client: c, APIReader: c, Scheme: testScheme(t), Recorder: record.NewFakeRecorder(100),
		GitHub:  &GitHubFactory{BaseURL: gh.api.URL, Reader: c, RateLimits: rl},
		Limiter: NewCollectionLimiter(60),
		Now:     func() time.Time { return now },
	}
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: "widgets"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.RequeueAfter != 10*time.Minute {
		t.Errorf("requeue = %v", res.RequeueAfter)
	}

	var repo v1alpha1.AgentRepository
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "widgets"}, &repo)
	if repo.Status.Phase != v1alpha1.RepoPhaseReady || repo.Status.AgentCount != 2 || repo.Status.LastSyncTime == nil ||
		repo.Status.RateLimit == nil || repo.Status.RateLimit.Remaining != 4321 || len(repo.Status.Conditions) != 1 ||
		repo.Status.Conditions[0].Status != "True" {
		t.Fatalf("repo status = %+v", repo.Status)
	}

	ti := getAgent(t, c, "widgets-test-improver")
	if !ti.Spec.Dispatchable || ti.Spec.WorkflowID != 101 || ti.Spec.DisplayName != "Test Improver" || ti.Status.State != "active" ||
		len(ti.Spec.DispatchInputs) != 1 || ti.Spec.DispatchInputs[0].Name != "focus" || len(ti.OwnerReferences) != 1 {
		t.Errorf("test-improver agent = %+v / %+v", ti.Spec, ti.Status)
	}
	tr := getAgent(t, c, "widgets-triage")
	if tr.Spec.Dispatchable || tr.Spec.DisplayName != "Triage" || tr.Status.State != "disabled_manually" ||
		len(tr.Spec.Triggers) != 1 || tr.Spec.Triggers[0] != "issues" {
		t.Errorf("triage agent = %+v / %+v", tr.Spec, tr.Status)
	}
	var removed v1alpha1.Agent
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "widgets-removed"}, &removed); err == nil {
		t.Error("stale agent was not deleted")
	}

	var runs v1alpha1.AgentRunList
	_ = c.List(context.Background(), &runs, client.InNamespace(ns))
	names := map[string]v1alpha1.AgentRun{}
	for _, run := range runs.Items {
		names[run.Name] = run
	}
	if len(names) != 2 {
		t.Errorf("runs = %v (want manual-run + 901; 902 skipped, 903 in progress, 904 tracked, 905 gate only, 906 action_required)", keys(names))
	}
	imported, ok := names["widgets-test-improver-901"]
	if !ok || imported.Spec.Origin != "github" || imported.Status.Phase != v1alpha1.RunPhaseSucceeded ||
		imported.Status.UsageState != v1alpha1.UsagePending || imported.Labels[v1alpha1.LabelRepository] != "widgets" {
		t.Errorf("imported run = %+v", imported)
	}
	if imported.Status.DurationSeconds != 420 {
		t.Errorf("imported duration = %d, want 420 (from jobs)", imported.Status.DurationSeconds)
	}
	// 903 is still running: the import window must stay open at its creation time.
	if want := now.Add(-time.Minute).UTC().Format(time.RFC3339); ti.Annotations[v1alpha1.AnnotationRunsSyncedAt] != want {
		t.Errorf("runs-synced-at = %q, want %q", ti.Annotations[v1alpha1.AnnotationRunsSyncedAt], want)
	}
	// Jobs were checked for 901 and 905 only (not skipped/action_required/in-progress/known).
	if gh.jobCalls != 2 {
		t.Errorf("job calls = %d, want 2", gh.jobCalls)
	}

	// Second sync: idempotent, files not re-read (SHA unchanged).
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: "widgets"}}); err != nil {
		t.Fatal(err)
	}
	_ = c.List(context.Background(), &runs, client.InNamespace(ns))
	if len(runs.Items) != 2 {
		t.Errorf("second sync created duplicates: %d runs", len(runs.Items))
	}
	// 905 was rejected and cached; 901 is known: no new job calls.
	if gh.jobCalls != 2 {
		t.Errorf("job calls after second sync = %d, want 2 (negative decisions cached)", gh.jobCalls)
	}

	// 903 completes with the agent job: imported on the next sync.
	gh.mu.Lock()
	gh.listRuns[2] = ghRun(903, "schedule", "completed", "success", now.Add(-time.Minute))
	gh.mu.Unlock()
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: "widgets"}}); err != nil {
		t.Fatal(err)
	}
	if run := getRun(t, c, "widgets-test-improver-903"); run.Status.Phase != v1alpha1.RunPhaseSucceeded {
		t.Errorf("903 phase = %s", run.Status.Phase)
	}
}

func TestRepositorySyncRespectsCap(t *testing.T) {
	gh := newFakeGitHub(t)
	now := time.Now().UTC()
	gh.listRuns = []map[string]any{
		ghRun(1001, "schedule", "completed", "success", now.Add(-1*time.Hour)),
		ghRun(1002, "schedule", "completed", "success", now.Add(-2*time.Hour)),
		ghRun(1003, "schedule", "completed", "success", now.Add(-3*time.Hour)),
	}
	c := newClient(t, baseObjects()...)
	r := &AgentRepositoryReconciler{
		Client: c, APIReader: c, Scheme: testScheme(t), Recorder: record.NewFakeRecorder(100),
		GitHub:  &GitHubFactory{BaseURL: gh.api.URL, Reader: c, RateLimits: NewRateLimits()},
		Limiter: NewCollectionLimiter(2),
		Now:     func() time.Time { return now },
	}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: "widgets"}}); err != nil {
		t.Fatal(err)
	}
	var runs v1alpha1.AgentRunList
	_ = c.List(context.Background(), &runs, client.InNamespace(ns))
	if len(runs.Items) != 2 || gh.jobCalls != 2 {
		t.Errorf("runs = %d jobCalls = %d, want 2/2 with a cap of 2", len(runs.Items), gh.jobCalls)
	}
	ti := getAgent(t, c, "widgets-test-improver")
	if want := now.Add(-3 * time.Hour).UTC().Format(time.RFC3339); ti.Annotations[v1alpha1.AnnotationRunsSyncedAt] != want {
		t.Errorf("runs-synced-at = %q, want %q (oldest deferred run)", ti.Annotations[v1alpha1.AnnotationRunsSyncedAt], want)
	}
}

func TestRebuildStats(t *testing.T) {
	gh := newFakeGitHub(t)
	start := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	gh.jobs[2002] = gateOnlyJobs(start)
	mkRun := func(name, origin string, id int64, phase, conclusion string) *v1alpha1.AgentRun {
		st := metav1.NewTime(start)
		return &v1alpha1.AgentRun{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec:       v1alpha1.AgentRunSpec{AgentRef: "widgets-test-improver", Origin: origin, GitHubRunID: id},
			Status: v1alpha1.AgentRunStatus{GitHubRunID: id, Phase: phase, Conclusion: conclusion, Counted: true,
				StartTime: &st, DurationSeconds: 99999, UsageState: v1alpha1.UsageUnavailable},
		}
	}
	agent := dispatchableAgent()
	agent.Status = v1alpha1.AgentStatus{Totals: &v1alpha1.RunStats{Runs: 4}, Daily: []v1alpha1.DailyBucket{{Date: "2026-10-06", RunStats: v1alpha1.RunStats{Runs: 4}}}}
	c := newClient(t, append(baseObjects(), agent,
		mkRun("good", "github", 2001, v1alpha1.RunPhaseSucceeded, "success"),
		mkRun("gate-only", "github", 2002, v1alpha1.RunPhaseSucceeded, "success"),
		mkRun("approval", "github", 2003, v1alpha1.RunPhaseFailed, "action_required"),
		mkRun("manual", "rancher", 2004, v1alpha1.RunPhaseSucceeded, "success"),
	)...)
	factory := &GitHubFactory{BaseURL: gh.api.URL, Reader: c, RateLimits: NewRateLimits()}
	if err := RebuildStats(context.Background(), c, factory, ctrl.Log); err != nil {
		t.Fatal(err)
	}
	var runs v1alpha1.AgentRunList
	_ = c.List(context.Background(), &runs, client.InNamespace(ns))
	got := map[string]v1alpha1.AgentRun{}
	for _, r := range runs.Items {
		got[r.Name] = r
	}
	if len(got) != 2 {
		t.Fatalf("remaining runs = %v, want good + manual", keys(got))
	}
	for _, name := range []string{"good", "manual"} {
		if got[name].Status.Counted || got[name].Status.DurationSeconds != 420 {
			t.Errorf("%s: counted=%v duration=%d", name, got[name].Status.Counted, got[name].Status.DurationSeconds)
		}
	}
	a := getAgent(t, c, agent.Name)
	if len(a.Status.Daily) != 0 || a.Status.Totals == nil || a.Status.Totals.Runs != 0 {
		t.Errorf("agent stats not reset: %+v", a.Status)
	}

	// The AgentRun reconciler recounts the remaining runs.
	now := start.Add(24 * time.Hour)
	rr := newRunReconciler(c, gh, &now)
	reconcileRun(t, rr, "good")
	reconcileRun(t, rr, "manual")
	a = getAgent(t, c, agent.Name)
	if a.Status.Totals.Runs != 2 || a.Status.Totals.DurationSeconds != 840 {
		t.Errorf("recount totals = %+v", a.Status.Totals)
	}
}

func keys(m map[string]v1alpha1.AgentRun) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
