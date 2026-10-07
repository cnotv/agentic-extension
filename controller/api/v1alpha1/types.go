package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Labels and annotations used by the controller.
const (
	LabelRepository = "agentic.rancher.io/repository"
	LabelAgent      = "agentic.rancher.io/agent"
	LabelOrigin     = "agentic.rancher.io/origin"

	// AnnotationMarkdownSHA / AnnotationLockSHA record the blob SHAs the Agent spec was built from.
	AnnotationMarkdownSHA = "agentic.rancher.io/markdown-sha"
	AnnotationLockSHA     = "agentic.rancher.io/lock-sha"
	// AnnotationRunsSyncedAt records when runs were last imported for an Agent.
	AnnotationRunsSyncedAt = "agentic.rancher.io/runs-synced-at"
	// AnnotationOutputsRefreshedAt records when pull request outputs were last refreshed.
	AnnotationOutputsRefreshedAt = "agentic.rancher.io/outputs-refreshed-at"
)

// Origins of an AgentRun.
const (
	OriginRancher = "rancher"
	OriginGitHub  = "github"
)

// AgentRepository phases.
const (
	RepoPhasePending = "Pending"
	RepoPhaseSyncing = "Syncing"
	RepoPhaseReady   = "Ready"
	RepoPhaseError   = "Error"
)

// AgentRun phases.
const (
	RunPhasePending    = "Pending"
	RunPhaseDispatched = "Dispatched"
	RunPhaseQueued     = "Queued"
	RunPhaseRunning    = "Running"
	RunPhaseSucceeded  = "Succeeded"
	RunPhaseFailed     = "Failed"
	RunPhaseCancelled  = "Cancelled"
	RunPhaseError      = "Error"
)

// Usage states.
const (
	UsagePending     = "Pending"
	UsageCollected   = "Collected"
	UsageUnavailable = "Unavailable"
)

// Output kinds.
const (
	OutputKindPullRequest = "pullRequest"
	OutputKindIssue       = "issue"
	OutputKindComment     = "comment"
	OutputKindLabel       = "label"
	OutputKindPush        = "push"
	OutputKindOther       = "other"
)

// ---------------------------------------------------------------------------
// AgentRepository

// RepositorySecretRef points to a Secret in the same namespace holding a GitHub token.
type RepositorySecretRef struct {
	Name string `json:"name"`
	Key  string `json:"key,omitempty"`
}

// AgentRepositorySpec is the desired state of an AgentRepository.
type AgentRepositorySpec struct {
	Repository      string              `json:"repository"`
	Branch          string              `json:"branch,omitempty"`
	WorkflowsPath   string              `json:"workflowsPath,omitempty"`
	SecretRef       RepositorySecretRef `json:"secretRef"`
	SyncInterval    string              `json:"syncInterval,omitempty"`
	BackfillDays    *int                `json:"backfillDays,omitempty"`
	MaxRunsPerAgent *int                `json:"maxRunsPerAgent,omitempty"`
	Suspend         bool                `json:"suspend,omitempty"`
}

// RateLimit mirrors the GitHub X-RateLimit-* headers.
type RateLimit struct {
	Limit     int          `json:"limit,omitempty"`
	Remaining int          `json:"remaining"`
	Reset     *metav1.Time `json:"reset,omitempty"`
}

// Condition is a simplified status condition.
type Condition struct {
	Type               string       `json:"type"`
	Status             string       `json:"status"`
	Reason             string       `json:"reason,omitempty"`
	Message            string       `json:"message,omitempty"`
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`
}

// AgentRepositoryStatus is the observed state of an AgentRepository.
type AgentRepositoryStatus struct {
	Phase              string       `json:"phase,omitempty"`
	Message            string       `json:"message,omitempty"`
	ObservedGeneration int64        `json:"observedGeneration,omitempty"`
	LastSyncTime       *metav1.Time `json:"lastSyncTime,omitempty"`
	AgentCount         int          `json:"agentCount"`
	DefaultBranch      string       `json:"defaultBranch,omitempty"`
	HTMLURL            string       `json:"htmlUrl,omitempty"`
	RateLimit          *RateLimit   `json:"rateLimit,omitempty"`
	Conditions         []Condition  `json:"conditions,omitempty"`
}

// AgentRepository is a GitHub repository whose gh-aw workflows are synced into the cluster.
// +kubebuilder:object:root=true
type AgentRepository struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentRepositorySpec   `json:"spec,omitempty"`
	Status AgentRepositoryStatus `json:"status,omitempty"`
}

// AgentRepositoryList is a list of AgentRepository.
// +kubebuilder:object:root=true
type AgentRepositoryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentRepository `json:"items"`
}

// ---------------------------------------------------------------------------
// Agent

// DispatchInput describes one workflow_dispatch input.
type DispatchInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Type        string   `json:"type,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Default     string   `json:"default,omitempty"`
	Options     []string `json:"options,omitempty"`
}

// AgentSpec is written by the controller from the workflow files.
type AgentSpec struct {
	RepositoryRef  string          `json:"repositoryRef,omitempty"`
	Repository     string          `json:"repository,omitempty"`
	WorkflowFile   string          `json:"workflowFile,omitempty"`
	LockFile       string          `json:"lockFile,omitempty"`
	WorkflowID     int64           `json:"workflowId,omitempty"`
	DisplayName    string          `json:"displayName,omitempty"`
	Description    string          `json:"description,omitempty"`
	Engine         string          `json:"engine,omitempty"`
	Model          string          `json:"model,omitempty"`
	TimeoutMinutes int             `json:"timeoutMinutes,omitempty"`
	Triggers       []string        `json:"triggers,omitempty"`
	Schedule       string          `json:"schedule,omitempty"`
	SlashCommand   string          `json:"slashCommand,omitempty"`
	Dispatchable   bool            `json:"dispatchable"`
	DispatchInputs []DispatchInput `json:"dispatchInputs,omitempty"`
	SafeOutputs    []string        `json:"safeOutputs,omitempty"`
	HTMLURL        string          `json:"htmlUrl,omitempty"`
}

// LastRun summarises the newest run of an Agent.
type LastRun struct {
	Name       string       `json:"name,omitempty"`
	Conclusion string       `json:"conclusion,omitempty"`
	StartTime  *metav1.Time `json:"startTime,omitempty"`
	HTMLURL    string       `json:"htmlUrl,omitempty"`
}

// RunStats holds counters shared by totals and daily buckets.
type RunStats struct {
	Runs               int64   `json:"runs"`
	Succeeded          int64   `json:"succeeded"`
	Failed             int64   `json:"failed"`
	Cancelled          int64   `json:"cancelled"`
	DurationSeconds    int64   `json:"durationSeconds"`
	InputTokens        int64   `json:"inputTokens"`
	OutputTokens       int64   `json:"outputTokens"`
	CacheReadTokens    int64   `json:"cacheReadTokens"`
	CacheWriteTokens   int64   `json:"cacheWriteTokens"`
	Comments           int64   `json:"comments"`
	Issues             int64   `json:"issues"`
	PullRequests       int64   `json:"pullRequests"`
	PullRequestsMerged int64   `json:"pullRequestsMerged"`
	PullRequestsClosed int64   `json:"pullRequestsClosed"`
	Labels             int64   `json:"labels"`
	OtherOutputs       int64   `json:"otherOutputs"`
	AICredits          float64 `json:"aiCredits"`
}

// DailyBucket is RunStats for one UTC day.
type DailyBucket struct {
	Date     string `json:"date"`
	RunStats `json:",inline"`
}

// AgentStatus is the observed state of an Agent.
type AgentStatus struct {
	State              string        `json:"state,omitempty"`
	LastRun            *LastRun      `json:"lastRun,omitempty"`
	Totals             *RunStats     `json:"totals,omitempty"`
	Daily              []DailyBucket `json:"daily,omitempty"`
	ObservedGeneration int64         `json:"observedGeneration,omitempty"`
}

// Agent is a gh-aw agentic workflow discovered in an AgentRepository.
// +kubebuilder:object:root=true
type Agent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentSpec   `json:"spec,omitempty"`
	Status AgentStatus `json:"status,omitempty"`
}

// AgentList is a list of Agent.
// +kubebuilder:object:root=true
type AgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Agent `json:"items"`
}

// ---------------------------------------------------------------------------
// AgentRun

// AgentRunSpec is the desired state of an AgentRun.
type AgentRunSpec struct {
	AgentRef    string            `json:"agentRef"`
	Origin      string            `json:"origin,omitempty"`
	Ref         string            `json:"ref,omitempty"`
	Inputs      map[string]string `json:"inputs,omitempty"`
	GitHubRunID int64             `json:"githubRunId,omitempty"`
}

// Usage holds token and credit usage of a run.
type Usage struct {
	InputTokens      int64   `json:"inputTokens"`
	OutputTokens     int64   `json:"outputTokens"`
	CacheReadTokens  int64   `json:"cacheReadTokens"`
	CacheWriteTokens int64   `json:"cacheWriteTokens"`
	AICredits        float64 `json:"aiCredits"`
}

// Output is one safe output produced by a run.
type Output struct {
	Type   string `json:"type"`
	Kind   string `json:"kind,omitempty"`
	Repo   string `json:"repo,omitempty"`
	Number int64  `json:"number,omitempty"`
	URL    string `json:"url,omitempty"`
	Title  string `json:"title,omitempty"`
	State  string `json:"state,omitempty"`
}

// AgentRunStatus is the observed state of an AgentRun.
type AgentRunStatus struct {
	Phase           string       `json:"phase,omitempty"`
	Message         string       `json:"message,omitempty"`
	GitHubRunID     int64        `json:"githubRunId,omitempty"`
	RunNumber       int64        `json:"runNumber,omitempty"`
	RunAttempt      int          `json:"runAttempt,omitempty"`
	HTMLURL         string       `json:"htmlUrl,omitempty"`
	Event           string       `json:"event,omitempty"`
	Actor           string       `json:"actor,omitempty"`
	HeadBranch      string       `json:"headBranch,omitempty"`
	Conclusion      string       `json:"conclusion,omitempty"`
	DispatchTime    *metav1.Time `json:"dispatchTime,omitempty"`
	StartTime       *metav1.Time `json:"startTime,omitempty"`
	CompletionTime  *metav1.Time `json:"completionTime,omitempty"`
	DurationSeconds int64        `json:"durationSeconds,omitempty"`
	Model           string       `json:"model,omitempty"`
	Usage           *Usage       `json:"usage,omitempty"`
	UsageState      string       `json:"usageState,omitempty"`
	Outputs         []Output     `json:"outputs,omitempty"`
	Counted         bool         `json:"counted,omitempty"`
}

// AgentRun is one run of an Agent on GitHub Actions.
// +kubebuilder:object:root=true
type AgentRun struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentRunSpec   `json:"spec,omitempty"`
	Status AgentRunStatus `json:"status,omitempty"`
}

// AgentRunList is a list of AgentRun.
// +kubebuilder:object:root=true
type AgentRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentRun `json:"items"`
}

// IsTerminal reports whether a run phase is final.
func IsTerminal(phase string) bool {
	switch phase {
	case RunPhaseSucceeded, RunPhaseFailed, RunPhaseCancelled, RunPhaseError:
		return true
	}
	return false
}
