package controllers

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/rancher/agentic-extension/controller/api/v1alpha1"
	"github.com/rancher/agentic-extension/controller/internal/ghaw"
	"github.com/rancher/agentic-extension/controller/internal/github"
	"github.com/rancher/agentic-extension/controller/internal/stats"
)

// AgentRunReconciler dispatches, polls and accounts AgentRuns.
type AgentRunReconciler struct {
	client.Client
	APIReader client.Reader
	Recorder  record.EventRecorder
	GitHub    *GitHubFactory
	Limiter   *CollectionLimiter
	Now       func() time.Time
}

// SetupWithManager registers the reconciler.
func (r *AgentRunReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Now == nil {
		r.Now = time.Now
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named("agentrun").
		For(&v1alpha1.AgentRun{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		WithOptions(controllerOptions(2)).
		Complete(r)
}

type runCtx struct {
	run     *v1alpha1.AgentRun
	agent   *v1alpha1.Agent
	repo    *v1alpha1.AgentRepository
	repoKey types.NamespacedName
	gh      *github.Client
}

// Reconcile implements reconcile.Reconciler.
func (r *AgentRunReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var run v1alpha1.AgentRun
	if err := r.Get(ctx, req.NamespacedName, &run); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if run.DeletionTimestamp != nil {
		return ctrl.Result{}, nil
	}
	now := r.Now()
	origin := run.Spec.Origin
	if origin == "" {
		origin = v1alpha1.OriginRancher
	}

	// Imported runs get their first status from the repository sync; give it a moment.
	if origin == v1alpha1.OriginGitHub && run.Status.Phase == "" && now.Sub(run.CreationTimestamp.Time) < 10*time.Second {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	if run.Status.Phase == v1alpha1.RunPhaseError {
		return ctrl.Result{}, nil
	}

	var agent v1alpha1.Agent
	if err := r.Get(ctx, types.NamespacedName{Namespace: run.Namespace, Name: run.Spec.AgentRef}, &agent); err != nil {
		if apierrors.IsNotFound(err) {
			if v1alpha1.IsTerminal(run.Status.Phase) {
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, r.fail(ctx, &run, fmt.Sprintf("Agent %q not found", run.Spec.AgentRef))
		}
		return ctrl.Result{}, err
	}
	var repo v1alpha1.AgentRepository
	if err := r.Get(ctx, types.NamespacedName{Namespace: run.Namespace, Name: agent.Spec.RepositoryRef}, &repo); err != nil {
		if apierrors.IsNotFound(err) {
			if v1alpha1.IsTerminal(run.Status.Phase) {
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, r.fail(ctx, &run, fmt.Sprintf("AgentRepository %q not found", agent.Spec.RepositoryRef))
		}
		return ctrl.Result{}, err
	}

	if err := r.ensureLabels(ctx, &run, &agent, origin); err != nil {
		return ctrl.Result{}, err
	}

	rc := &runCtx{run: &run, agent: &agent, repo: &repo, repoKey: types.NamespacedName{Namespace: repo.Namespace, Name: repo.Name}}

	runID := run.Status.GitHubRunID
	if runID == 0 {
		runID = run.Spec.GitHubRunID
	}
	terminal := v1alpha1.IsTerminal(run.Status.Phase)

	// Nothing left to do with GitHub?
	if terminal && run.Status.Counted && !hasOpenPullRequests(&run) {
		return ctrl.Result{}, nil
	}
	if terminal && !run.Status.Counted && (run.Status.UsageState == v1alpha1.UsageCollected || run.Status.UsageState == v1alpha1.UsageUnavailable) {
		return r.count(ctx, rc)
	}

	if wait := r.GitHub.RateLimits.BackoffFor(rc.repoKey, now); wait > 0 {
		return ctrl.Result{RequeueAfter: wait}, nil
	}
	gh, err := r.GitHub.ClientFor(ctx, &repo)
	if err != nil {
		if runID == 0 && origin == v1alpha1.OriginRancher && run.Status.DispatchTime == nil {
			return ctrl.Result{}, r.fail(ctx, &run, err.Error())
		}
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}
	rc.gh = gh

	if runID == 0 {
		if origin == v1alpha1.OriginGitHub {
			return ctrl.Result{}, r.fail(ctx, &run, "githubRunId is required for runs imported from GitHub")
		}
		return r.dispatch(ctx, rc)
	}

	if !terminal {
		return r.poll(ctx, rc, runID)
	}
	if run.Status.UsageState == "" || run.Status.UsageState == v1alpha1.UsagePending {
		return r.collect(ctx, rc)
	}
	return r.refreshPullRequests(ctx, rc)
}

func (r *AgentRunReconciler) ensureLabels(ctx context.Context, run *v1alpha1.AgentRun, agent *v1alpha1.Agent, origin string) error {
	want := map[string]string{
		v1alpha1.LabelAgent:      agent.Name,
		v1alpha1.LabelRepository: agent.Spec.RepositoryRef,
		v1alpha1.LabelOrigin:     origin,
	}
	missing := false
	for k, v := range want {
		if run.Labels[k] != v {
			missing = true
		}
	}
	if !missing {
		return nil
	}
	patch := client.MergeFrom(run.DeepCopy())
	if run.Labels == nil {
		run.Labels = map[string]string{}
	}
	for k, v := range want {
		run.Labels[k] = v
	}
	return r.Patch(ctx, run, patch)
}

func (r *AgentRunReconciler) fail(ctx context.Context, run *v1alpha1.AgentRun, msg string) error {
	run.Status.Phase = v1alpha1.RunPhaseError
	run.Status.Message = msg
	r.Recorder.Event(run, corev1.EventTypeWarning, "Error", msg)
	return r.Status().Update(ctx, run)
}

// dispatch triggers the workflow once. dispatchTime is persisted before calling GitHub so a
// retry never dispatches twice; if the run id is unknown afterwards it is looked up instead.
func (r *AgentRunReconciler) dispatch(ctx context.Context, rc *runCtx) (ctrl.Result, error) {
	run, agent := rc.run, rc.agent
	if !agent.Spec.Dispatchable || agent.Spec.WorkflowID == 0 {
		return ctrl.Result{}, r.fail(ctx, run, fmt.Sprintf("Agent %q cannot be dispatched (no workflow_dispatch trigger)", agent.Name))
	}
	now := r.Now()

	if run.Status.DispatchTime != nil {
		return r.findDispatchedRun(ctx, rc)
	}

	ref := run.Spec.Ref
	if ref == "" {
		ref = rc.repo.Spec.Branch
	}
	if ref == "" {
		ref = rc.repo.Status.DefaultBranch
	}
	if ref == "" {
		info, err := rc.gh.GetRepository(ctx, agent.Spec.Repository)
		if err != nil {
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}
		ref = info.DefaultBranch
	}

	// Persist intent first. A conflict here means our copy is stale: retry later, never dispatch.
	t := metav1.NewTime(now.UTC())
	run.Status.DispatchTime = &t
	run.Status.Phase = v1alpha1.RunPhasePending
	run.Status.Message = "Dispatching workflow on " + ref
	if err := r.Status().Update(ctx, run); err != nil {
		return ctrl.Result{}, err
	}

	res, err := rc.gh.DispatchWorkflow(ctx, agent.Spec.Repository, agent.Spec.WorkflowID, ref, run.Spec.Inputs)
	if err != nil {
		return ctrl.Result{}, r.fail(ctx, run, "Dispatch failed: "+err.Error())
	}
	run.Status.Phase = v1alpha1.RunPhaseDispatched
	run.Status.Message = "Workflow dispatched on " + ref
	if res.RunID != 0 {
		run.Status.GitHubRunID = res.RunID
		run.Status.HTMLURL = res.HTMLURL
	}
	r.Recorder.Event(run, corev1.EventTypeNormal, "Dispatched", run.Status.Message)
	if err := r.Status().Update(ctx, run); err != nil {
		// The run id will be recovered by findDispatchedRun.
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

func (r *AgentRunReconciler) findDispatchedRun(ctx context.Context, rc *runCtx) (ctrl.Result, error) {
	run, agent := rc.run, rc.agent
	now := r.Now()
	from := run.Status.DispatchTime.Add(-10 * time.Second)
	runs, err := rc.gh.ListWorkflowRuns(ctx, agent.Spec.Repository, agent.Spec.WorkflowID,
		github.RunListOptions{CreatedSince: from, Event: "workflow_dispatch", MaxPages: 1})
	if err != nil {
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}
	var all v1alpha1.AgentRunList
	if err := r.List(ctx, &all, client.InNamespace(run.Namespace)); err != nil {
		return ctrl.Result{}, err
	}
	claimed := map[int64]bool{}
	for _, o := range all.Items {
		if o.Name == run.Name {
			continue
		}
		claimed[o.Spec.GitHubRunID] = true
		claimed[o.Status.GitHubRunID] = true
	}
	var best *github.WorkflowRun
	for i := range runs {
		gr := &runs[i]
		if gr.CreatedAt.Before(from) || claimed[gr.ID] {
			continue
		}
		if best == nil || gr.CreatedAt.After(best.CreatedAt) {
			best = gr
		}
	}
	if best == nil {
		if now.Sub(run.Status.DispatchTime.Time) > DispatchLookupTimeout {
			return ctrl.Result{}, r.fail(ctx, run, "Workflow was dispatched but the run could not be found")
		}
		if run.Status.Phase != v1alpha1.RunPhaseDispatched {
			run.Status.Phase = v1alpha1.RunPhaseDispatched
			run.Status.Message = "Waiting for the dispatched run to appear"
			if err := r.Status().Update(ctx, run); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	ApplyRun(&run.Status, best)
	if v1alpha1.IsTerminal(run.Status.Phase) {
		r.applyJobsDuration(ctx, rc)
	}
	if err := r.Status().Update(ctx, run); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: PollInterval}, nil
}

func (r *AgentRunReconciler) poll(ctx context.Context, rc *runCtx, runID int64) (ctrl.Result, error) {
	run := rc.run
	gr, err := rc.gh.GetRun(ctx, rc.agent.Spec.Repository, runID)
	if err != nil {
		if github.IsNotFound(err) {
			return ctrl.Result{}, r.fail(ctx, run, fmt.Sprintf("GitHub run %d not found", runID))
		}
		log.FromContext(ctx).Error(err, "polling run")
		return ctrl.Result{RequeueAfter: PollInterval}, nil
	}
	wasTerminal := v1alpha1.IsTerminal(run.Status.Phase)
	ApplyRun(&run.Status, gr)
	if v1alpha1.IsTerminal(run.Status.Phase) {
		r.applyJobsDuration(ctx, rc)
	}
	if err := r.Status().Update(ctx, run); err != nil {
		return ctrl.Result{}, err
	}
	if !v1alpha1.IsTerminal(run.Status.Phase) {
		return ctrl.Result{RequeueAfter: PollInterval}, nil
	}
	if !wasTerminal {
		r.Recorder.Event(run, corev1.EventTypeNormal, "Completed", run.Status.Message)
	}
	return r.collect(ctx, rc)
}

// applyJobsDuration replaces the run duration with the span of the jobs that actually ran.
func (r *AgentRunReconciler) applyJobsDuration(ctx context.Context, rc *runCtx) {
	jobs, err := rc.gh.ListRunJobs(ctx, rc.agent.Spec.Repository, rc.run.Status.GitHubRunID)
	if err != nil {
		log.FromContext(ctx).Error(err, "listing jobs", "run", rc.run.Status.GitHubRunID)
		return
	}
	if d, ok := JobsDuration(jobs); ok {
		rc.run.Status.DurationSeconds = d
	}
}

// collect downloads usage and safe-output artifacts of a terminal run.
func (r *AgentRunReconciler) collect(ctx context.Context, rc *runCtx) (ctrl.Result, error) {
	run := rc.run
	now := r.Now()
	ok, wait := r.Limiter.Allow(rc.repoKey, SyncInterval(rc.repo), now)
	if !ok {
		return ctrl.Result{RequeueAfter: wait + time.Second}, nil
	}
	pastGrace := run.Status.CompletionTime == nil || now.Sub(run.Status.CompletionTime.Time) > ArtifactGracePeriod

	arts, err := rc.gh.ListRunArtifacts(ctx, rc.agent.Spec.Repository, run.Status.GitHubRunID)
	if err != nil {
		log.FromContext(ctx).Error(err, "listing artifacts")
		return ctrl.Result{RequeueAfter: ArtifactRetryInterval}, nil
	}
	var usageArt, outputsArt *github.Artifact
	for i := range arts {
		switch arts[i].Name {
		case ghaw.UsageArtifact:
			usageArt = &arts[i]
		case ghaw.SafeOutputsArtifact:
			outputsArt = &arts[i]
		}
	}

	var usage *ghaw.UsageReport
	var usageErr error
	switch {
	case usageArt == nil:
		usageErr = fmt.Errorf("usage artifact not found")
	case usageArt.Expired:
		usageErr = fmt.Errorf("usage artifact expired")
		pastGrace = true
	default:
		zipData, err := rc.gh.DownloadArtifact(ctx, *usageArt)
		if err == nil {
			var b []byte
			if b, err = ghaw.FileFromZip(zipData, ghaw.UsageFile); err == nil {
				usage, err = ghaw.ParseUsage(b)
			}
		}
		usageErr = err
	}
	if usageErr != nil && !pastGrace {
		return ctrl.Result{RequeueAfter: ArtifactRetryInterval}, nil
	}

	if outputsArt != nil && !outputsArt.Expired {
		if zipData, err := rc.gh.DownloadArtifact(ctx, *outputsArt); err == nil {
			if b, err := ghaw.FileFromZip(zipData, ghaw.SafeOutputsFile); err == nil {
				run.Status.Outputs = ghaw.ParseSafeOutputs(b)
			}
		} else if !pastGrace {
			return ctrl.Result{RequeueAfter: ArtifactRetryInterval}, nil
		}
	}

	if usage != nil {
		run.Status.Usage = usage.ToUsage()
		run.Status.Model = usage.PrimaryModel
		run.Status.UsageState = v1alpha1.UsageCollected
	} else {
		run.Status.UsageState = v1alpha1.UsageUnavailable
		log.FromContext(ctx).Info("usage unavailable", "run", run.Name, "reason", usageErr.Error())
	}
	if err := r.Status().Update(ctx, run); err != nil {
		return ctrl.Result{}, err
	}
	return r.count(ctx, rc)
}

// count adds a terminal run to the Agent's daily buckets once.
func (r *AgentRunReconciler) count(ctx context.Context, rc *runCtx) (ctrl.Result, error) {
	run := rc.run
	now := r.Now()
	snapshot := run.DeepCopy()
	if err := UpdateAgentStatus(ctx, r.Client, r.APIReader, client.ObjectKeyFromObject(rc.agent), func(st *v1alpha1.AgentStatus) bool {
		stats.CountRun(st, snapshot, now)
		return true
	}); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating agent stats: %w", err)
	}
	run.Status.Counted = true
	if err := r.Status().Update(ctx, run); err != nil {
		return ctrl.Result{}, err
	}
	if hasOpenPullRequests(run) {
		return ctrl.Result{RequeueAfter: SyncInterval(rc.repo)}, nil
	}
	return ctrl.Result{}, nil
}

func hasOpenPullRequests(run *v1alpha1.AgentRun) bool {
	for _, o := range run.Status.Outputs {
		if o.Kind == v1alpha1.OutputKindPullRequest && o.State == "open" && o.Number > 0 {
			return true
		}
	}
	return false
}

// refreshPullRequests updates the state of open pull requests at most every sync interval.
func (r *AgentRunReconciler) refreshPullRequests(ctx context.Context, rc *runCtx) (ctrl.Result, error) {
	run := rc.run
	if !run.Status.Counted || !hasOpenPullRequests(run) {
		return ctrl.Result{}, nil
	}
	now := r.Now()
	interval := SyncInterval(rc.repo)
	if s := run.Annotations[v1alpha1.AnnotationOutputsRefreshedAt]; s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			if elapsed := now.Sub(t); elapsed < interval {
				return ctrl.Result{RequeueAfter: interval - elapsed}, nil
			}
		}
	}

	delta := v1alpha1.RunStats{}
	changed := false
	for i := range run.Status.Outputs {
		o := &run.Status.Outputs[i]
		if o.Kind != v1alpha1.OutputKindPullRequest || o.State != "open" || o.Number == 0 {
			continue
		}
		repoName := o.Repo
		if repoName == "" {
			repoName = rc.agent.Spec.Repository
		}
		pr, err := rc.gh.GetPullRequest(ctx, repoName, o.Number)
		if err != nil {
			log.FromContext(ctx).Error(err, "refreshing pull request", "repo", repoName, "number", o.Number)
			continue
		}
		if o.Title == "" {
			o.Title = pr.Title
			changed = true
		}
		switch {
		case pr.Merged:
			o.State = "merged"
			delta.PullRequestsMerged++
			changed = true
		case pr.State == "closed":
			o.State = "closed"
			delta.PullRequestsClosed++
			changed = true
		}
	}

	if delta.PullRequestsMerged > 0 || delta.PullRequestsClosed > 0 {
		date := stats.RunDate(run)
		if err := UpdateAgentStatus(ctx, r.Client, r.APIReader, client.ObjectKeyFromObject(rc.agent), func(st *v1alpha1.AgentStatus) bool {
			if !stats.AddToExistingBucket(st, date, delta) {
				return false
			}
			stats.Recompute(st)
			return true
		}); err != nil {
			return ctrl.Result{}, err
		}
	}
	if changed {
		if err := r.Status().Update(ctx, run); err != nil {
			return ctrl.Result{}, err
		}
	}
	patch := client.MergeFrom(run.DeepCopy())
	if run.Annotations == nil {
		run.Annotations = map[string]string{}
	}
	run.Annotations[v1alpha1.AnnotationOutputsRefreshedAt] = now.UTC().Format(time.RFC3339)
	if err := r.Patch(ctx, run, patch); err != nil {
		return ctrl.Result{}, err
	}
	if hasOpenPullRequests(run) {
		return ctrl.Result{RequeueAfter: interval}, nil
	}
	return ctrl.Result{}, nil
}
