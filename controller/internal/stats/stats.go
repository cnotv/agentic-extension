// Package stats maintains Agent daily buckets and totals.
package stats

import (
	"math"
	"sort"
	"time"

	"github.com/rancher/agentic-extension/controller/api/v1alpha1"
)

// RetentionDays is how long daily buckets are kept.
const RetentionDays = 90

// DateOf returns the UTC day of t as YYYY-MM-DD.
func DateOf(t time.Time) string { return t.UTC().Format("2006-01-02") }

// RunDate returns the bucket date for a run (start time, falling back to creation).
func RunDate(run *v1alpha1.AgentRun) string {
	if run.Status.StartTime != nil {
		return DateOf(run.Status.StartTime.Time)
	}
	if run.Status.DispatchTime != nil {
		return DateOf(run.Status.DispatchTime.Time)
	}
	return DateOf(run.CreationTimestamp.Time)
}

// RunContribution returns what a single terminal run adds to a bucket.
func RunContribution(run *v1alpha1.AgentRun) v1alpha1.RunStats {
	s := v1alpha1.RunStats{Runs: 1, DurationSeconds: run.Status.DurationSeconds}
	switch run.Status.Phase {
	case v1alpha1.RunPhaseSucceeded:
		s.Succeeded = 1
	case v1alpha1.RunPhaseCancelled:
		s.Cancelled = 1
	default:
		s.Failed = 1
	}
	if u := run.Status.Usage; u != nil {
		s.InputTokens = u.InputTokens
		s.OutputTokens = u.OutputTokens
		s.CacheReadTokens = u.CacheReadTokens
		s.CacheWriteTokens = u.CacheWriteTokens
		s.AICredits = u.AICredits
	}
	for _, o := range run.Status.Outputs {
		switch o.Kind {
		case v1alpha1.OutputKindComment:
			s.Comments++
		case v1alpha1.OutputKindIssue:
			s.Issues++
		case v1alpha1.OutputKindPullRequest:
			s.PullRequests++
			switch o.State {
			case "merged":
				s.PullRequestsMerged++
			case "closed":
				s.PullRequestsClosed++
			}
		case v1alpha1.OutputKindLabel:
			s.Labels++
		default:
			s.OtherOutputs++
		}
	}
	return s
}

// Add accumulates b into a.
func Add(a *v1alpha1.RunStats, b v1alpha1.RunStats) {
	a.Runs += b.Runs
	a.Succeeded += b.Succeeded
	a.Failed += b.Failed
	a.Cancelled += b.Cancelled
	a.DurationSeconds += b.DurationSeconds
	a.InputTokens += b.InputTokens
	a.OutputTokens += b.OutputTokens
	a.CacheReadTokens += b.CacheReadTokens
	a.CacheWriteTokens += b.CacheWriteTokens
	a.Comments += b.Comments
	a.Issues += b.Issues
	a.PullRequests += b.PullRequests
	a.PullRequestsMerged += b.PullRequestsMerged
	a.PullRequestsClosed += b.PullRequestsClosed
	a.Labels += b.Labels
	a.OtherOutputs += b.OtherOutputs
	// Round away float noise such as 118.41257999999999.
	a.AICredits = math.Round((a.AICredits+b.AICredits)*1e6) / 1e6
}

// AddToBucket adds delta to the bucket for date, creating it if needed, and keeps buckets sorted.
func AddToBucket(status *v1alpha1.AgentStatus, date string, delta v1alpha1.RunStats) {
	for i := range status.Daily {
		if status.Daily[i].Date == date {
			Add(&status.Daily[i].RunStats, delta)
			return
		}
	}
	status.Daily = append(status.Daily, v1alpha1.DailyBucket{Date: date, RunStats: delta})
	sort.Slice(status.Daily, func(i, j int) bool { return status.Daily[i].Date < status.Daily[j].Date })
}

// AddToExistingBucket adds delta only if the bucket for date exists. It reports whether it did.
func AddToExistingBucket(status *v1alpha1.AgentStatus, date string, delta v1alpha1.RunStats) bool {
	for i := range status.Daily {
		if status.Daily[i].Date == date {
			Add(&status.Daily[i].RunStats, delta)
			return true
		}
	}
	return false
}

// Trim drops buckets older than RetentionDays relative to now.
func Trim(status *v1alpha1.AgentStatus, now time.Time) {
	cutoff := DateOf(now.AddDate(0, 0, -RetentionDays))
	kept := status.Daily[:0]
	for _, b := range status.Daily {
		if b.Date >= cutoff {
			kept = append(kept, b)
		}
	}
	status.Daily = kept
}

// Recompute sets totals to the sum of all buckets.
func Recompute(status *v1alpha1.AgentStatus) {
	t := v1alpha1.RunStats{}
	for _, b := range status.Daily {
		Add(&t, b.RunStats)
	}
	status.Totals = &t
}

// UpdateLastRun replaces lastRun when run started later than the current one.
func UpdateLastRun(status *v1alpha1.AgentStatus, run *v1alpha1.AgentRun) {
	start := run.Status.StartTime
	if start == nil {
		return
	}
	if status.LastRun != nil && status.LastRun.StartTime != nil && !start.After(status.LastRun.StartTime.Time) {
		return
	}
	conclusion := run.Status.Conclusion
	if conclusion == "" {
		conclusion = run.Status.Phase
	}
	status.LastRun = &v1alpha1.LastRun{
		Name:       run.Name,
		Conclusion: conclusion,
		StartTime:  start.DeepCopy(),
		HTMLURL:    run.Status.HTMLURL,
	}
}

// CountRun adds a terminal run to the agent status: bucket, trim, totals, lastRun.
func CountRun(status *v1alpha1.AgentStatus, run *v1alpha1.AgentRun, now time.Time) {
	date := RunDate(run)
	cutoff := DateOf(now.AddDate(0, 0, -RetentionDays))
	if date >= cutoff {
		AddToBucket(status, date, RunContribution(run))
	}
	Trim(status, now)
	Recompute(status)
	UpdateLastRun(status, run)
}
