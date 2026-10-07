package stats

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/rancher/agentic-extension/controller/api/v1alpha1"
)

func mkRun(name, phase string, start time.Time, credits float64, outputs ...v1alpha1.Output) *v1alpha1.AgentRun {
	st := metav1.NewTime(start)
	return &v1alpha1.AgentRun{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: v1alpha1.AgentRunStatus{
			Phase:           phase,
			StartTime:       &st,
			DurationSeconds: 60,
			Usage:           &v1alpha1.Usage{InputTokens: 10, OutputTokens: 20, CacheReadTokens: 30, CacheWriteTokens: 40, AICredits: credits},
			Outputs:         outputs,
			HTMLURL:         "https://example/" + name,
		},
	}
}

func TestCountRunBucketsAndTotals(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	st := &v1alpha1.AgentStatus{}

	CountRun(st, mkRun("a", v1alpha1.RunPhaseSucceeded, now.Add(-2*time.Hour), 1.5,
		v1alpha1.Output{Kind: v1alpha1.OutputKindComment}, v1alpha1.Output{Kind: v1alpha1.OutputKindLabel},
		v1alpha1.Output{Kind: v1alpha1.OutputKindPullRequest, State: "open"}, v1alpha1.Output{Kind: v1alpha1.OutputKindPush}), now)
	CountRun(st, mkRun("b", v1alpha1.RunPhaseFailed, now.Add(-1*time.Hour), 2.0), now)
	CountRun(st, mkRun("c", v1alpha1.RunPhaseCancelled, now.Add(-26*time.Hour), 0.5,
		v1alpha1.Output{Kind: v1alpha1.OutputKindIssue}), now)

	if len(st.Daily) != 2 {
		t.Fatalf("buckets = %+v", st.Daily)
	}
	if st.Daily[0].Date != "2026-10-06" || st.Daily[1].Date != "2026-10-07" {
		t.Errorf("buckets not sorted oldest first: %s, %s", st.Daily[0].Date, st.Daily[1].Date)
	}
	today := st.Daily[1]
	if today.Runs != 2 || today.Succeeded != 1 || today.Failed != 1 || today.Comments != 1 || today.Labels != 1 ||
		today.PullRequests != 1 || today.OtherOutputs != 1 || today.AICredits != 3.5 || today.DurationSeconds != 120 {
		t.Errorf("today bucket = %+v", today)
	}
	tot := st.Totals
	if tot.Runs != 3 || tot.Cancelled != 1 || tot.Issues != 1 || tot.InputTokens != 30 || tot.CacheWriteTokens != 120 || tot.AICredits != 4.0 {
		t.Errorf("totals = %+v", tot)
	}
	if st.LastRun == nil || st.LastRun.Name != "b" || st.LastRun.Conclusion != v1alpha1.RunPhaseFailed {
		t.Errorf("lastRun = %+v", st.LastRun)
	}
}

func TestTrimDropsOldBuckets(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	st := &v1alpha1.AgentStatus{Daily: []v1alpha1.DailyBucket{
		{Date: "2026-06-01", RunStats: v1alpha1.RunStats{Runs: 5}},
		{Date: "2026-07-09", RunStats: v1alpha1.RunStats{Runs: 2}}, // exactly 90 days ago: kept
		{Date: "2026-10-01", RunStats: v1alpha1.RunStats{Runs: 1}},
	}}
	CountRun(st, mkRun("x", v1alpha1.RunPhaseSucceeded, now, 0), now)
	if len(st.Daily) != 3 || st.Daily[0].Date != "2026-07-09" {
		t.Fatalf("buckets = %+v", st.Daily)
	}
	if st.Totals.Runs != 4 {
		t.Errorf("totals.runs = %d, want 4", st.Totals.Runs)
	}

	// A run older than retention is not bucketed at all.
	CountRun(st, mkRun("old", v1alpha1.RunPhaseSucceeded, now.AddDate(0, 0, -120), 0), now)
	if st.Totals.Runs != 4 || len(st.Daily) != 3 {
		t.Errorf("old run was bucketed: %+v", st.Daily)
	}
	if st.LastRun.Name != "x" {
		t.Errorf("older run replaced lastRun: %+v", st.LastRun)
	}
}

func TestAICreditsRounding(t *testing.T) {
	a := v1alpha1.RunStats{AICredits: 21.84691}
	Add(&a, v1alpha1.RunStats{AICredits: 96.56567})
	if a.AICredits != 118.41258 {
		t.Errorf("aiCredits = %v, want 118.41258", a.AICredits)
	}
}

func TestAddToExistingBucket(t *testing.T) {
	st := &v1alpha1.AgentStatus{Daily: []v1alpha1.DailyBucket{{Date: "2026-10-01", RunStats: v1alpha1.RunStats{Runs: 1, PullRequests: 1}}}}
	if !AddToExistingBucket(st, "2026-10-01", v1alpha1.RunStats{PullRequestsMerged: 1}) {
		t.Fatal("expected bucket to exist")
	}
	if AddToExistingBucket(st, "2026-09-01", v1alpha1.RunStats{PullRequestsMerged: 1}) {
		t.Fatal("expected missing bucket")
	}
	Recompute(st)
	if st.Totals.PullRequestsMerged != 1 || st.Totals.Runs != 1 {
		t.Errorf("totals = %+v", st.Totals)
	}
}
