package controllers

import (
	"sync"
	"time"

	"github.com/rancher/agentic-extension/controller/internal/github"
)

// AgentJobName is the gh-aw job that runs the coding agent.
const AgentJobName = "agent"

// AgentJobRan reports whether the gh-aw "agent" job actually ran (not skipped, not pending).
// Runs where only the activation gate ran are noise and are not imported.
func AgentJobRan(jobs []github.Job) bool {
	for _, j := range jobs {
		if j.Name == AgentJobName && j.Conclusion != "" && j.Conclusion != "skipped" {
			return true
		}
	}
	return false
}

// JobsDuration is max(completed_at) - min(started_at) over jobs that were not skipped.
func JobsDuration(jobs []github.Job) (int64, bool) {
	var first, last time.Time
	for _, j := range jobs {
		if j.Conclusion == "skipped" || j.StartedAt == nil || j.CompletedAt == nil || j.StartedAt.IsZero() || j.CompletedAt.IsZero() {
			continue
		}
		if first.IsZero() || j.StartedAt.Before(first) {
			first = *j.StartedAt
		}
		if j.CompletedAt.After(last) {
			last = *j.CompletedAt
		}
	}
	if first.IsZero() || last.IsZero() || last.Before(first) {
		return 0, false
	}
	return int64(last.Sub(first).Seconds()), true
}

// ignoredConclusions are run conclusions never imported from GitHub.
var ignoredConclusions = map[string]bool{"skipped": true, "action_required": true}

// runDecisions caches "agent did not run" decisions so runs are not re-checked every sync.
type runDecisions struct {
	mu sync.Mutex
	m  map[int64]bool
}

func (d *runDecisions) rejected(id int64) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.m[id]
}

func (d *runDecisions) reject(id int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.m == nil {
		d.m = map[int64]bool{}
	}
	d.m[id] = true
}
