package controllers

import (
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/rancher/agentic-extension/controller/api/v1alpha1"
	"github.com/rancher/agentic-extension/controller/internal/github"
)

func TestPhaseFor(t *testing.T) {
	cases := []struct{ status, conclusion, want string }{
		{"queued", "", v1alpha1.RunPhaseQueued},
		{"waiting", "", v1alpha1.RunPhaseQueued},
		{"pending", "", v1alpha1.RunPhaseQueued},
		{"requested", "", v1alpha1.RunPhaseQueued},
		{"in_progress", "", v1alpha1.RunPhaseRunning},
		{"completed", "success", v1alpha1.RunPhaseSucceeded},
		{"completed", "cancelled", v1alpha1.RunPhaseCancelled},
		{"completed", "skipped", v1alpha1.RunPhaseCancelled},
		{"completed", "failure", v1alpha1.RunPhaseFailed},
		{"completed", "timed_out", v1alpha1.RunPhaseFailed},
		{"completed", "startup_failure", v1alpha1.RunPhaseFailed},
		{"completed", "action_required", v1alpha1.RunPhaseCancelled},
		{"completed", "stale", v1alpha1.RunPhaseFailed},
	}
	for _, c := range cases {
		if got, _ := PhaseFor(c.status, c.conclusion); got != c.want {
			t.Errorf("PhaseFor(%q,%q) = %q, want %q", c.status, c.conclusion, got, c.want)
		}
	}
	if _, msg := PhaseFor("completed", "skipped"); msg != "Skipped by the workflow if: condition" {
		t.Errorf("skipped message = %q", msg)
	}
}

func TestNames(t *testing.T) {
	if got := AgentName("dashboard", "daily-test-improver"); got != "dashboard-daily-test-improver" {
		t.Errorf("AgentName = %q", got)
	}
	if got := AgentName("Dash_Board", "Weird.Name"); got != "dash-board-weird-name" {
		t.Errorf("sanitised AgentName = %q", got)
	}
	long := AgentName("a-really-long-repository-resource-name", "an-even-longer-workflow-basename-for-testing")
	if len(long) > 63 || len(validation.IsDNS1123Label(long)) != 0 {
		t.Errorf("long AgentName %q (%d) invalid", long, len(long))
	}
	if long == AgentName("a-really-long-repository-resource-name", "an-even-longer-workflow-basename-for-testinx") {
		t.Error("truncated names must stay unique")
	}

	if got := AgentRunName("dashboard-issue-triage", 37616098023); got != "dashboard-issue-triage-37616098023" {
		t.Errorf("AgentRunName = %q", got)
	}
	runName := AgentRunName(strings.Repeat("x", 62), 37616098023)
	if len(runName) != 63 || !strings.HasSuffix(runName, "-37616098023") || len(validation.IsDNS1123Label(runName)) != 0 {
		t.Errorf("truncated AgentRunName = %q (%d)", runName, len(runName))
	}
}

func TestApplyRun(t *testing.T) {
	start := time.Date(2026, 10, 6, 20, 55, 9, 0, time.UTC)
	st := &v1alpha1.AgentRunStatus{}
	ApplyRun(st, &github.WorkflowRun{
		ID: 1, RunNumber: 210, RunAttempt: 1, Event: "schedule", Status: "completed", Conclusion: "success",
		HeadBranch: "master", HTMLURL: "https://x", CreatedAt: start, RunStartedAt: &start,
		UpdatedAt: start.Add(14*time.Minute + 29*time.Second), Actor: &github.Actor{Login: "bot"},
	})
	if st.Phase != v1alpha1.RunPhaseSucceeded || st.DurationSeconds != 869 || st.UsageState != v1alpha1.UsagePending ||
		st.Actor != "bot" || st.CompletionTime == nil || st.RunNumber != 210 {
		t.Errorf("status = %+v", st)
	}
}

func TestPruneCandidates(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	mk := func(name, phase string, counted bool, age time.Duration) v1alpha1.AgentRun {
		st := metav1.NewTime(now.Add(-age))
		return v1alpha1.AgentRun{ObjectMeta: metav1.ObjectMeta{Name: name},
			Status: v1alpha1.AgentRunStatus{Phase: phase, Counted: counted, StartTime: &st}}
	}
	runs := []v1alpha1.AgentRun{
		mk("new", v1alpha1.RunPhaseSucceeded, true, time.Hour),
		mk("mid", v1alpha1.RunPhaseFailed, true, 2*time.Hour),
		mk("old", v1alpha1.RunPhaseSucceeded, true, 3*time.Hour),
		mk("running", v1alpha1.RunPhaseRunning, false, 4*time.Hour),
		mk("uncounted", v1alpha1.RunPhaseSucceeded, false, 5*time.Hour),
		mk("ancient", v1alpha1.RunPhaseSucceeded, true, 31*24*time.Hour),
		mk("ancient-running", v1alpha1.RunPhaseQueued, false, 40*24*time.Hour),
	}
	got := map[string]bool{}
	for _, r := range PruneCandidates(runs, 2, now) {
		got[r.Name] = true
	}
	want := map[string]bool{"old": true, "ancient": true}
	if len(got) != len(want) || !got["old"] || !got["ancient"] {
		t.Errorf("pruned = %v, want %v", got, want)
	}
	got = map[string]bool{}
	for _, r := range PruneCandidates(runs, 50, now) {
		got[r.Name] = true
	}
	if len(got) != 1 || !got["ancient"] {
		t.Errorf("age prune = %v", got)
	}
}

func TestCollectionLimiter(t *testing.T) {
	l := NewCollectionLimiter(2)
	key := types.NamespacedName{Namespace: "ns", Name: "repo"}
	now := time.Now()
	for i := 0; i < 2; i++ {
		if ok, _ := l.Allow(key, time.Minute, now); !ok {
			t.Fatal("expected allow")
		}
	}
	ok, wait := l.Allow(key, time.Minute, now.Add(10*time.Second))
	if ok || wait != 50*time.Second {
		t.Errorf("third = %v %v", ok, wait)
	}
	if ok, _ := l.Allow(key, time.Minute, now.Add(61*time.Second)); !ok {
		t.Error("expected new window")
	}
}

func TestRateLimitBackoff(t *testing.T) {
	rl := NewRateLimits()
	key := types.NamespacedName{Namespace: "ns", Name: "repo"}
	now := time.Now()
	rl.Record(key, github.RateLimit{Limit: 5000, Remaining: 4000, Reset: now.Add(time.Hour)})
	if rl.BackoffFor(key, now) != 0 {
		t.Error("no backoff expected")
	}
	rl.Record(key, github.RateLimit{Limit: 5000, Remaining: 150, Reset: now.Add(10 * time.Minute)})
	if d := rl.BackoffFor(key, now); d < 10*time.Minute {
		t.Errorf("backoff = %v", d)
	}
}

func TestAgentJobRanAndDuration(t *testing.T) {
	at := func(h, m int) *time.Time {
		v := time.Date(2026, 10, 6, h, m, 0, 0, time.UTC)
		return &v
	}
	ran := []github.Job{
		{Name: "pre_activation", Conclusion: "success", StartedAt: at(10, 0), CompletedAt: at(10, 1)},
		{Name: "agent", Conclusion: "failure", StartedAt: at(10, 2), CompletedAt: at(10, 20)},
		{Name: "conclusion", Conclusion: "success", StartedAt: at(10, 21), CompletedAt: at(10, 22)},
		{Name: "safe_outputs", Conclusion: "skipped", StartedAt: at(23, 0), CompletedAt: at(23, 0)},
	}
	if !AgentJobRan(ran) {
		t.Error("agent job with failure conclusion did run")
	}
	if d, ok := JobsDuration(ran); !ok || d != 22*60 {
		t.Errorf("duration = %d %v, want 1320 (skipped jobs ignored)", d, ok)
	}

	gate := []github.Job{
		{Name: "pre_activation", Conclusion: "success", StartedAt: at(10, 0), CompletedAt: at(10, 1)},
		{Name: "agent", Conclusion: "skipped", StartedAt: at(10, 1), CompletedAt: at(10, 1)},
	}
	if AgentJobRan(gate) {
		t.Error("skipped agent job must not count as ran")
	}
	if AgentJobRan([]github.Job{{Name: "agent", Status: "in_progress"}}) {
		t.Error("agent job without conclusion must not count as ran")
	}
	if AgentJobRan(nil) {
		t.Error("no jobs")
	}
	if _, ok := JobsDuration([]github.Job{{Name: "agent", Conclusion: "skipped", StartedAt: at(1, 0), CompletedAt: at(2, 0)}}); ok {
		t.Error("only skipped jobs: no duration")
	}
}
