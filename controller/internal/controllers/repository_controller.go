package controllers

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/rancher/agentic-extension/controller/api/v1alpha1"
	"github.com/rancher/agentic-extension/controller/internal/ghaw"
	"github.com/rancher/agentic-extension/controller/internal/github"
)

// AgentRepositoryReconciler syncs Agents and imports AgentRuns from GitHub.
type AgentRepositoryReconciler struct {
	client.Client
	APIReader client.Reader
	Scheme    *runtime.Scheme
	Recorder  record.EventRecorder
	GitHub    *GitHubFactory
	// Limiter caps GitHub calls per sync (job checks here, artifact collections in AgentRuns).
	Limiter *CollectionLimiter
	Now     func() time.Time

	rejectedRuns runDecisions
}

// SetupWithManager registers the reconciler.
func (r *AgentRepositoryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Now == nil {
		r.Now = time.Now
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named("agentrepository").
		For(&v1alpha1.AgentRepository{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}

type discovered struct {
	basename string
	md       github.ContentEntry
	lock     github.ContentEntry
}

// Reconcile implements reconcile.Reconciler.
func (r *AgentRepositoryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	var repo v1alpha1.AgentRepository
	if err := r.Get(ctx, req.NamespacedName, &repo); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	now := r.Now()

	if repo.Spec.Suspend {
		r.setReady(&repo, "False", "Suspended", "Sync is suspended")
		repo.Status.Message = "Sync is suspended"
		repo.Status.ObservedGeneration = repo.Generation
		return ctrl.Result{}, r.updateRepoStatus(ctx, &repo)
	}
	interval := SyncInterval(&repo)

	if wait := r.GitHub.RateLimits.BackoffFor(req.NamespacedName, now); wait > 0 {
		repo.Status.Message = fmt.Sprintf("GitHub rate limit low, waiting until %s", now.Add(wait).UTC().Format(time.RFC3339))
		r.copyRateLimit(&repo)
		_ = r.updateRepoStatus(ctx, &repo)
		return ctrl.Result{RequeueAfter: wait}, nil
	}

	if repo.Status.Phase == "" || repo.Status.Phase == v1alpha1.RepoPhasePending {
		repo.Status.Phase = v1alpha1.RepoPhaseSyncing
		repo.Status.Message = "Syncing"
		if err := r.updateRepoStatus(ctx, &repo); err != nil {
			return ctrl.Result{}, err
		}
	}

	warnings, err := r.sync(ctx, &repo, now)
	r.copyRateLimit(&repo)
	repo.Status.ObservedGeneration = repo.Generation
	if err != nil {
		logger.Error(err, "sync failed")
		repo.Status.Phase = v1alpha1.RepoPhaseError
		repo.Status.Message = err.Error()
		r.setReady(&repo, "False", "SyncFailed", err.Error())
		r.Recorder.Event(&repo, "Warning", "SyncFailed", err.Error())
		if uerr := r.updateRepoStatus(ctx, &repo); uerr != nil {
			return ctrl.Result{}, uerr
		}
		retryAfter := interval
		if retryAfter > 2*time.Minute {
			retryAfter = 2 * time.Minute
		}
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}

	t := metav1.NewTime(now.UTC())
	repo.Status.LastSyncTime = &t
	repo.Status.Phase = v1alpha1.RepoPhaseReady
	msg := fmt.Sprintf("Synced %d agents", repo.Status.AgentCount)
	if len(warnings) > 0 {
		msg += "; " + strings.Join(warnings, "; ")
		if len(msg) > 1000 {
			msg = msg[:1000]
		}
	}
	repo.Status.Message = msg
	r.setReady(&repo, "True", "Synced", msg)
	if err := r.updateRepoStatus(ctx, &repo); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: interval}, nil
}

func (r *AgentRepositoryReconciler) copyRateLimit(repo *v1alpha1.AgentRepository) {
	rl, ok := r.GitHub.RateLimits.Get(types.NamespacedName{Namespace: repo.Namespace, Name: repo.Name})
	if !ok {
		return
	}
	out := &v1alpha1.RateLimit{Limit: rl.Limit, Remaining: rl.Remaining}
	if !rl.Reset.IsZero() {
		t := metav1.NewTime(rl.Reset)
		out.Reset = &t
	}
	repo.Status.RateLimit = out
}

func (r *AgentRepositoryReconciler) setReady(repo *v1alpha1.AgentRepository, status, reason, message string) {
	now := metav1.NewTime(r.Now().UTC())
	for i := range repo.Status.Conditions {
		c := &repo.Status.Conditions[i]
		if c.Type != "Ready" {
			continue
		}
		if c.Status != status {
			c.LastTransitionTime = &now
		}
		c.Status, c.Reason, c.Message = status, reason, message
		return
	}
	repo.Status.Conditions = append(repo.Status.Conditions, v1alpha1.Condition{
		Type: "Ready", Status: status, Reason: reason, Message: message, LastTransitionTime: &now,
	})
}

func (r *AgentRepositoryReconciler) updateRepoStatus(ctx context.Context, repo *v1alpha1.AgentRepository) error {
	desired := repo.Status.DeepCopy()
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var cur v1alpha1.AgentRepository
		if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(repo), &cur); err != nil {
			return err
		}
		cur.Status = *desired
		if err := r.Status().Update(ctx, &cur); err != nil {
			return err
		}
		repo.ResourceVersion = cur.ResourceVersion
		return nil
	})
}

// sync does one full repository sync. A returned error is fatal; warnings are per-agent problems.
func (r *AgentRepositoryReconciler) sync(ctx context.Context, repo *v1alpha1.AgentRepository, now time.Time) ([]string, error) {
	gh, err := r.GitHub.ClientFor(ctx, repo)
	if err != nil {
		return nil, err
	}
	info, err := gh.GetRepository(ctx, repo.Spec.Repository)
	if err != nil {
		return nil, fmt.Errorf("fetching repository: %w", err)
	}
	repo.Status.DefaultBranch = info.DefaultBranch
	repo.Status.HTMLURL = info.HTMLURL
	branch := repo.Spec.Branch
	if branch == "" {
		branch = info.DefaultBranch
	}
	wfPath := strings.Trim(repo.Spec.WorkflowsPath, "/")
	if wfPath == "" {
		wfPath = ".github/workflows"
	}
	entries, err := gh.ListDirectory(ctx, repo.Spec.Repository, wfPath, branch)
	if err != nil {
		return nil, fmt.Errorf("listing %s at %s: %w", wfPath, branch, err)
	}
	found := Discover(entries)

	var existingList v1alpha1.AgentList
	if err := r.List(ctx, &existingList, client.InNamespace(repo.Namespace), client.MatchingLabels{v1alpha1.LabelRepository: repo.Name}); err != nil {
		return nil, err
	}
	existing := map[string]*v1alpha1.Agent{}
	for i := range existingList.Items {
		existing[existingList.Items[i].Name] = &existingList.Items[i]
	}

	var warnings []string
	var agents []*v1alpha1.Agent
	wanted := map[string]bool{}
	for _, d := range found {
		name := AgentName(repo.Name, d.basename)
		wanted[name] = true
		agent, err := r.syncAgent(ctx, gh, repo, info, branch, d, existing[name])
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", d.basename, err))
			if agent == nil {
				agent = existing[name]
			}
		}
		if agent != nil {
			agents = append(agents, agent)
		}
	}
	for name, a := range existing {
		if wanted[name] {
			continue
		}
		if err := r.Delete(ctx, a); err != nil && !apierrors.IsNotFound(err) {
			warnings = append(warnings, fmt.Sprintf("deleting agent %s: %v", name, err))
		}
	}
	repo.Status.AgentCount = len(agents)

	var runList v1alpha1.AgentRunList
	if err := r.List(ctx, &runList, client.InNamespace(repo.Namespace)); err != nil {
		return warnings, err
	}
	byAgent := map[string][]v1alpha1.AgentRun{}
	for _, run := range runList.Items {
		byAgent[run.Spec.AgentRef] = append(byAgent[run.Spec.AgentRef], run)
	}

	for _, agent := range agents {
		if agent.Spec.WorkflowID == 0 {
			continue
		}
		if wait := r.GitHub.RateLimits.BackoffFor(client.ObjectKeyFromObject(repo), r.Now()); wait > 0 {
			warnings = append(warnings, "run import paused: GitHub rate limit low")
			break
		}
		if err := r.importRuns(ctx, gh, repo, agent, byAgent[agent.Name], now); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s runs: %v", agent.Name, err))
		}
	}
	for _, agent := range agents {
		if err := r.prune(ctx, repo, byAgent[agent.Name], now); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s prune: %v", agent.Name, err))
		}
	}
	return warnings, nil
}

// Discover returns agents found in a directory listing: X.md with a sibling X.lock.yml.
func Discover(entries []github.ContentEntry) []discovered {
	files := map[string]github.ContentEntry{}
	for _, e := range entries {
		if e.Type == "file" {
			files[e.Name] = e
		}
	}
	var out []discovered
	for name, e := range files {
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		base := strings.TrimSuffix(name, ".md")
		lock, ok := files[base+".lock.yml"]
		if !ok {
			continue
		}
		out = append(out, discovered{basename: base, md: e, lock: lock})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].basename < out[j].basename })
	return out
}

func (r *AgentRepositoryReconciler) syncAgent(ctx context.Context, gh *github.Client, repo *v1alpha1.AgentRepository,
	info *github.Repository, branch string, d discovered, existing *v1alpha1.Agent) (*v1alpha1.Agent, error) {

	name := AgentName(repo.Name, d.basename)
	var spec v1alpha1.AgentSpec
	unchanged := existing != nil &&
		existing.Annotations[v1alpha1.AnnotationMarkdownSHA] == d.md.SHA &&
		existing.Annotations[v1alpha1.AnnotationLockSHA] == d.lock.SHA
	if unchanged {
		spec = *existing.Spec.DeepCopy()
	} else {
		md, err := gh.GetFileRaw(ctx, repo.Spec.Repository, d.md.Path, branch)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", d.md.Path, err)
		}
		lock, err := gh.GetFileRaw(ctx, repo.Spec.Repository, d.lock.Path, branch)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", d.lock.Path, err)
		}
		wf, err := ghaw.Parse(md, lock)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", d.md.Path, err)
		}
		spec = v1alpha1.AgentSpec{
			DisplayName:    wf.DisplayName,
			Description:    wf.Description,
			Engine:         wf.Engine,
			Model:          wf.Model,
			TimeoutMinutes: wf.TimeoutMinutes,
			Triggers:       wf.Triggers,
			Schedule:       wf.Schedule,
			SlashCommand:   wf.SlashCommand,
			Dispatchable:   wf.Dispatchable,
			DispatchInputs: wf.DispatchInputs,
			SafeOutputs:    wf.SafeOutputs,
		}
	}
	spec.RepositoryRef = repo.Name
	spec.Repository = repo.Spec.Repository
	spec.WorkflowFile = d.md.Path
	spec.LockFile = d.lock.Path

	var warn error
	state := ""
	wf, err := gh.GetWorkflow(ctx, repo.Spec.Repository, d.lock.Name)
	switch {
	case err == nil:
		spec.WorkflowID = wf.ID
		state = wf.State
		if spec.DisplayName == "" {
			spec.DisplayName = wf.Name
		}
		spec.HTMLURL = wf.HTMLURL
	case github.IsNotFound(err):
		warn = fmt.Errorf("workflow %s is not registered on GitHub", d.lock.Name)
	default:
		warn = fmt.Errorf("fetching workflow: %w", err)
		if existing != nil {
			spec.WorkflowID = existing.Spec.WorkflowID
			state = existing.Status.State
		}
	}
	if spec.DisplayName == "" {
		spec.DisplayName = d.basename
	}
	if spec.HTMLURL == "" {
		spec.HTMLURL = fmt.Sprintf("%s/blob/%s/%s", info.HTMLURL, branch, d.lock.Path)
	}

	agent := existing
	if agent == nil {
		agent = &v1alpha1.Agent{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: repo.Namespace}}
	} else {
		agent = agent.DeepCopy()
	}
	before := agent.DeepCopy()
	if agent.Labels == nil {
		agent.Labels = map[string]string{}
	}
	agent.Labels[v1alpha1.LabelRepository] = repo.Name
	if agent.Annotations == nil {
		agent.Annotations = map[string]string{}
	}
	agent.Annotations[v1alpha1.AnnotationMarkdownSHA] = d.md.SHA
	agent.Annotations[v1alpha1.AnnotationLockSHA] = d.lock.SHA
	agent.Spec = spec
	if err := controllerutil.SetControllerReference(repo, agent, r.Scheme); err != nil {
		return nil, err
	}

	if existing == nil {
		if err := r.Create(ctx, agent); err != nil {
			return nil, fmt.Errorf("creating agent: %w", err)
		}
	} else if !equality.Semantic.DeepEqual(before.Spec, agent.Spec) ||
		!equality.Semantic.DeepEqual(before.Labels, agent.Labels) ||
		!equality.Semantic.DeepEqual(before.Annotations, agent.Annotations) ||
		!equality.Semantic.DeepEqual(before.OwnerReferences, agent.OwnerReferences) {
		if err := r.Update(ctx, agent); err != nil {
			return nil, fmt.Errorf("updating agent: %w", err)
		}
	}

	if agent.Status.State != state || agent.Status.ObservedGeneration != agent.Generation {
		gen := agent.Generation
		if err := UpdateAgentStatus(ctx, r.Client, r.APIReader, client.ObjectKeyFromObject(agent), func(st *v1alpha1.AgentStatus) bool {
			if st.State == state && st.ObservedGeneration == gen {
				return false
			}
			st.State = state
			st.ObservedGeneration = gen
			return true
		}); err != nil {
			return agent, fmt.Errorf("updating agent status: %w", err)
		}
		agent.Status.State = state
	}
	return agent, warn
}

// UpdateAgentStatus applies mutate to a fresh copy of the Agent status, retrying on conflict.
// mutate returns false when no write is needed.
func UpdateAgentStatus(ctx context.Context, c client.Client, reader client.Reader, key types.NamespacedName, mutate func(*v1alpha1.AgentStatus) bool) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var agent v1alpha1.Agent
		if err := reader.Get(ctx, key, &agent); err != nil {
			return err
		}
		if !mutate(&agent.Status) {
			return nil
		}
		return c.Status().Update(ctx, &agent)
	})
}

func (r *AgentRepositoryReconciler) importRuns(ctx context.Context, gh *github.Client, repo *v1alpha1.AgentRepository,
	agent *v1alpha1.Agent, runs []v1alpha1.AgentRun, now time.Time) error {

	logger := log.FromContext(ctx)
	known := map[int64]bool{}
	pendingDispatch := false
	for _, run := range runs {
		if run.Spec.GitHubRunID != 0 {
			known[run.Spec.GitHubRunID] = true
		}
		if run.Status.GitHubRunID != 0 {
			known[run.Status.GitHubRunID] = true
		}
		if run.Spec.Origin != v1alpha1.OriginGitHub && run.Status.GitHubRunID == 0 && !v1alpha1.IsTerminal(run.Status.Phase) {
			pendingDispatch = true
		}
	}

	since := now.AddDate(0, 0, -intOr(repo.Spec.BackfillDays, 14))
	if s := agent.Annotations[v1alpha1.AnnotationRunsSyncedAt]; s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			since = t.Add(-24 * time.Hour)
		}
	}
	ghRuns, err := gh.ListWorkflowRuns(ctx, repo.Spec.Repository, agent.Spec.WorkflowID, github.RunListOptions{CreatedSince: since})
	if err != nil {
		return err
	}
	repoKey := client.ObjectKeyFromObject(repo)
	interval := SyncInterval(repo)
	created, rejected, deferred := 0, 0, 0
	// Runs we could not decide on yet keep the import window open for the next sync.
	syncedAt := now
	deferRun := func(gr *github.WorkflowRun) {
		deferred++
		if gr.CreatedAt.Before(syncedAt) {
			syncedAt = gr.CreatedAt
		}
	}
	capped := false
	for i := range ghRuns {
		gr := &ghRuns[i]
		if ignoredConclusions[gr.Conclusion] || known[gr.ID] || r.rejectedRuns.rejected(gr.ID) {
			continue
		}
		// A rancher-origin run may still be looking for its dispatched run; let it claim first.
		if pendingDispatch && gr.Event == "workflow_dispatch" {
			deferRun(gr)
			continue
		}
		// Only import runs where the agent job actually ran; that is only known once completed.
		if gr.Status != "completed" {
			deferRun(gr)
			continue
		}
		if !capped {
			if ok, _ := r.Limiter.Allow(repoKey, interval, r.Now()); !ok {
				capped = true
			}
		}
		if capped {
			deferRun(gr)
			continue
		}
		jobs, err := gh.ListRunJobs(ctx, repo.Spec.Repository, gr.ID)
		if err != nil {
			logger.Error(err, "listing jobs", "run", gr.ID)
			deferRun(gr)
			continue
		}
		if !AgentJobRan(jobs) {
			r.rejectedRuns.reject(gr.ID)
			rejected++
			continue
		}
		run := &v1alpha1.AgentRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      AgentRunName(agent.Name, gr.ID),
				Namespace: agent.Namespace,
				Labels: map[string]string{
					v1alpha1.LabelAgent:      agent.Name,
					v1alpha1.LabelRepository: repo.Name,
					v1alpha1.LabelOrigin:     v1alpha1.OriginGitHub,
				},
			},
			Spec: v1alpha1.AgentRunSpec{AgentRef: agent.Name, Origin: v1alpha1.OriginGitHub, GitHubRunID: gr.ID},
		}
		if err := controllerutil.SetControllerReference(agent, run, r.Scheme); err != nil {
			return err
		}
		if err := r.Create(ctx, run); err != nil {
			if apierrors.IsAlreadyExists(err) {
				continue
			}
			return fmt.Errorf("creating run %d: %w", gr.ID, err)
		}
		created++
		ApplyRun(&run.Status, gr)
		if d, ok := JobsDuration(jobs); ok {
			run.Status.DurationSeconds = d
		}
		if err := r.Status().Update(ctx, run); err != nil && !apierrors.IsConflict(err) {
			logger.Error(err, "writing imported run status", "run", run.Name)
		}
	}
	logger.Info("imported runs", "agent", agent.Name, "since", since.UTC().Format("2006-01-02"),
		"listed", len(ghRuns), "known", len(known), "created", created, "rejected", rejected, "deferred", deferred)

	patch := client.MergeFrom(agent.DeepCopy())
	if agent.Annotations == nil {
		agent.Annotations = map[string]string{}
	}
	agent.Annotations[v1alpha1.AnnotationRunsSyncedAt] = syncedAt.UTC().Format(time.RFC3339)
	return r.Patch(ctx, agent, patch)
}

func runTime(run *v1alpha1.AgentRun) time.Time {
	if run.Status.StartTime != nil {
		return run.Status.StartTime.Time
	}
	return run.CreationTimestamp.Time
}

// PruneCandidates returns the runs to delete: beyond the newest maxRuns terminal+counted runs,
// or older than PruneAge. Non-terminal and uncounted runs are never returned.
func PruneCandidates(runs []v1alpha1.AgentRun, maxRuns int, now time.Time) []v1alpha1.AgentRun {
	var eligible []v1alpha1.AgentRun
	for _, run := range runs {
		if run.DeletionTimestamp == nil && v1alpha1.IsTerminal(run.Status.Phase) && run.Status.Counted {
			eligible = append(eligible, run)
		}
	}
	sort.SliceStable(eligible, func(i, j int) bool { return runTime(&eligible[i]).After(runTime(&eligible[j])) })
	var out []v1alpha1.AgentRun
	for i, run := range eligible {
		if i >= maxRuns || now.Sub(runTime(&run)) > PruneAge {
			out = append(out, run)
		}
	}
	return out
}

func (r *AgentRepositoryReconciler) prune(ctx context.Context, repo *v1alpha1.AgentRepository, runs []v1alpha1.AgentRun, now time.Time) error {
	for _, run := range PruneCandidates(runs, intOr(repo.Spec.MaxRunsPerAgent, 50), now) {
		run := run
		if err := r.Delete(ctx, &run); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}
