// Package controllers contains the AgentRepository and AgentRun reconcilers.
package controllers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	"github.com/rancher/agentic-extension/controller/api/v1alpha1"
	"github.com/rancher/agentic-extension/controller/internal/github"
)

const (
	// DefaultSyncInterval is used when spec.syncInterval is empty or invalid.
	DefaultSyncInterval = 10 * time.Minute
	// MinSyncInterval protects the GitHub rate limit.
	MinSyncInterval = time.Minute
	// RateLimitFloor: below this many remaining requests, back off until reset.
	RateLimitFloor = 200
	// DefaultMaxCollectionsPerSync caps artifact collections per repository per sync interval.
	DefaultMaxCollectionsPerSync = 60
	// PollInterval is how often non-terminal runs are polled.
	PollInterval = 30 * time.Second
	// ArtifactGracePeriod is how long after completion missing artifacts are retried.
	ArtifactGracePeriod = 10 * time.Minute
	// ArtifactRetryInterval is the delay between artifact retries.
	ArtifactRetryInterval = 2 * time.Minute
	// DispatchLookupTimeout is how long to look for a dispatched run when GitHub returned no id.
	DispatchLookupTimeout = 5 * time.Minute
	// PruneAge: terminal, counted runs older than this are deleted.
	PruneAge = 30 * 24 * time.Hour
)

var dnsInvalid = regexp.MustCompile(`[^a-z0-9-]+`)

// SanitizeName lowercases s and replaces characters invalid in DNS-1123 labels with '-'.
func SanitizeName(s string) string {
	s = dnsInvalid.ReplaceAllString(strings.ToLower(s), "-")
	s = regexp.MustCompile(`-+`).ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// truncateWithHash shortens s to max chars, appending a short hash when it had to cut.
func truncateWithHash(s string, max int) string {
	if len(s) <= max {
		return s
	}
	sum := sha256.Sum256([]byte(s))
	h := hex.EncodeToString(sum[:])[:6]
	cut := max - len(h) - 1
	return strings.TrimRight(s[:cut], "-") + "-" + h
}

// AgentName returns the Agent object name for a repository CR and workflow basename (≤63 chars).
func AgentName(repoCR, basename string) string {
	return truncateWithHash(SanitizeName(repoCR+"-"+basename), 63)
}

// AgentRunName returns "<agent>-<runId>", truncating the agent part to fit 63 chars.
func AgentRunName(agent string, runID int64) string {
	suffix := "-" + strconv.FormatInt(runID, 10)
	prefix := agent
	if max := 63 - len(suffix); len(prefix) > max {
		prefix = strings.TrimRight(prefix[:max], "-")
	}
	return prefix + suffix
}

// PhaseFor maps a GitHub run status/conclusion to an AgentRun phase and message.
func PhaseFor(status, conclusion string) (phase, message string) {
	switch status {
	case "queued", "waiting", "pending", "requested":
		return v1alpha1.RunPhaseQueued, "Run is " + status
	case "in_progress":
		return v1alpha1.RunPhaseRunning, "Run is in progress"
	case "completed":
		switch conclusion {
		case "success", "neutral":
			return v1alpha1.RunPhaseSucceeded, "Run succeeded"
		case "cancelled":
			return v1alpha1.RunPhaseCancelled, "Run was cancelled"
		case "skipped":
			return v1alpha1.RunPhaseCancelled, "Skipped by the workflow if: condition"
		case "action_required":
			return v1alpha1.RunPhaseCancelled, "Waiting for approval on GitHub (action_required)"
		case "failure", "timed_out", "startup_failure", "stale":
			return v1alpha1.RunPhaseFailed, "Run concluded " + conclusion
		default:
			return v1alpha1.RunPhaseFailed, "Run concluded " + conclusion
		}
	}
	return v1alpha1.RunPhaseQueued, "Run is " + status
}

// ApplyRun copies GitHub run fields into an AgentRun status.
func ApplyRun(st *v1alpha1.AgentRunStatus, r *github.WorkflowRun) {
	st.GitHubRunID = r.ID
	st.RunNumber = r.RunNumber
	st.RunAttempt = r.RunAttempt
	st.HTMLURL = r.HTMLURL
	st.Event = r.Event
	if r.Actor != nil {
		st.Actor = r.Actor.Login
	}
	st.HeadBranch = r.HeadBranch
	st.Conclusion = r.Conclusion
	start := r.CreatedAt
	if r.RunStartedAt != nil && !r.RunStartedAt.IsZero() {
		start = *r.RunStartedAt
	}
	if !start.IsZero() {
		t := metav1.NewTime(start.UTC())
		st.StartTime = &t
	}
	st.Phase, st.Message = PhaseFor(r.Status, r.Conclusion)
	if r.Status == "completed" {
		t := metav1.NewTime(r.UpdatedAt.UTC())
		st.CompletionTime = &t
		if st.StartTime != nil {
			d := int64(r.UpdatedAt.Sub(st.StartTime.Time).Seconds())
			if d < 0 {
				d = 0
			}
			st.DurationSeconds = d
		}
		if st.UsageState == "" {
			st.UsageState = v1alpha1.UsagePending
		}
	}
}

// SyncInterval parses spec.syncInterval.
func SyncInterval(repo *v1alpha1.AgentRepository) time.Duration {
	d, err := time.ParseDuration(repo.Spec.SyncInterval)
	if err != nil || d <= 0 {
		return DefaultSyncInterval
	}
	if d < MinSyncInterval {
		return MinSyncInterval
	}
	return d
}

func intOr(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

// RateLimits tracks the last seen GitHub rate limit per AgentRepository.
type RateLimits struct {
	mu sync.Mutex
	m  map[types.NamespacedName]github.RateLimit
}

// NewRateLimits returns an empty store.
func NewRateLimits() *RateLimits {
	return &RateLimits{m: map[types.NamespacedName]github.RateLimit{}}
}

// Record stores rl for key.
func (r *RateLimits) Record(key types.NamespacedName, rl github.RateLimit) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[key] = rl
}

// Get returns the last rate limit for key.
func (r *RateLimits) Get(key types.NamespacedName) (github.RateLimit, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rl, ok := r.m[key]
	return rl, ok
}

// BackoffFor returns how long to wait before calling GitHub for key, or 0.
func (r *RateLimits) BackoffFor(key types.NamespacedName, now time.Time) time.Duration {
	rl, ok := r.Get(key)
	if !ok || rl.Limit == 0 || rl.Remaining >= RateLimitFloor || rl.Reset.IsZero() {
		return 0
	}
	if wait := rl.Reset.Sub(now); wait > 0 {
		return wait + 5*time.Second
	}
	return 0
}

// CollectionLimiter caps artifact collections per repository per window.
type CollectionLimiter struct {
	mu  sync.Mutex
	Max int
	m   map[types.NamespacedName]*window
}

type window struct {
	start time.Time
	count int
}

// NewCollectionLimiter returns a limiter allowing max collections per window.
func NewCollectionLimiter(max int) *CollectionLimiter {
	return &CollectionLimiter{Max: max, m: map[types.NamespacedName]*window{}}
}

// Allow reserves a collection slot. When denied it returns the time until the window resets.
func (l *CollectionLimiter) Allow(key types.NamespacedName, length time.Duration, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	w, ok := l.m[key]
	if !ok || now.Sub(w.start) >= length {
		w = &window{start: now}
		l.m[key] = w
	}
	if w.count >= l.Max {
		return false, length - now.Sub(w.start)
	}
	w.count++
	return true, 0
}

// GitHubFactory builds GitHub clients for AgentRepositories.
type GitHubFactory struct {
	BaseURL    string
	Reader     client.Reader // uncached reader for Secrets
	RateLimits *RateLimits
}

// ClientFor reads the repository token and returns a client that records rate limits.
func (f *GitHubFactory) ClientFor(ctx context.Context, repo *v1alpha1.AgentRepository) (*github.Client, error) {
	key := repo.Spec.SecretRef.Key
	if key == "" {
		key = "token"
	}
	var secret corev1.Secret
	if err := f.Reader.Get(ctx, types.NamespacedName{Namespace: repo.Namespace, Name: repo.Spec.SecretRef.Name}, &secret); err != nil {
		return nil, fmt.Errorf("reading secret %s: %w", repo.Spec.SecretRef.Name, err)
	}
	token := strings.TrimSpace(string(secret.Data[key]))
	if token == "" {
		return nil, fmt.Errorf("secret %s has no %q key", repo.Spec.SecretRef.Name, key)
	}
	repoKey := types.NamespacedName{Namespace: repo.Namespace, Name: repo.Name}
	return github.NewClient(f.BaseURL, token, func(rl github.RateLimit) {
		f.RateLimits.Record(repoKey, rl)
	}), nil
}

func controllerOptions(concurrency int) controller.Options {
	return controller.Options{MaxConcurrentReconciles: concurrency}
}
