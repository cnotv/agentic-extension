package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rancher/agentic-extension/controller/api/v1alpha1"
	"github.com/rancher/agentic-extension/controller/internal/github"
)

// RebuildStats is a one-off repair run before the controllers start (--rebuild-stats):
//   - deletes github-origin AgentRuns that the import rules now reject (skipped or
//     action_required conclusion, or the gh-aw "agent" job did not run),
//   - recomputes durationSeconds from jobs for the remaining terminal runs,
//   - clears every Agent's daily/totals/lastRun and sets counted=false on all runs, so the
//     AgentRun reconciler recounts everything from scratch when it starts.
//
// Stats of runs that were already pruned are lost, so this must not run on every start.
func RebuildStats(ctx context.Context, c client.Client, factory *GitHubFactory, logger logr.Logger) error {
	var repos v1alpha1.AgentRepositoryList
	if err := c.List(ctx, &repos); err != nil {
		return err
	}
	repoByKey := map[types.NamespacedName]*v1alpha1.AgentRepository{}
	for i := range repos.Items {
		repoByKey[client.ObjectKeyFromObject(&repos.Items[i])] = &repos.Items[i]
	}
	var agents v1alpha1.AgentList
	if err := c.List(ctx, &agents); err != nil {
		return err
	}
	agentByKey := map[types.NamespacedName]*v1alpha1.Agent{}
	for i := range agents.Items {
		agentByKey[client.ObjectKeyFromObject(&agents.Items[i])] = &agents.Items[i]
	}
	clients := map[types.NamespacedName]*github.Client{}
	clientFor := func(agent *v1alpha1.Agent) *github.Client {
		key := types.NamespacedName{Namespace: agent.Namespace, Name: agent.Spec.RepositoryRef}
		if gh, ok := clients[key]; ok {
			return gh
		}
		repo, ok := repoByKey[key]
		if !ok {
			return nil
		}
		gh, err := factory.ClientFor(ctx, repo)
		if err != nil {
			logger.Error(err, "no GitHub client", "repository", key)
		}
		clients[key] = gh
		return gh
	}

	var runs v1alpha1.AgentRunList
	if err := c.List(ctx, &runs); err != nil {
		return err
	}
	deleted, reset := 0, 0
	for i := range runs.Items {
		run := &runs.Items[i]
		agent := agentByKey[types.NamespacedName{Namespace: run.Namespace, Name: run.Spec.AgentRef}]
		runID := run.Status.GitHubRunID
		if runID == 0 {
			runID = run.Spec.GitHubRunID
		}
		isGitHub := run.Spec.Origin == v1alpha1.OriginGitHub
		terminal := v1alpha1.IsTerminal(run.Status.Phase) && run.Status.Phase != v1alpha1.RunPhaseError

		if isGitHub && ignoredConclusions[run.Status.Conclusion] {
			if err := deleteRun(ctx, c, run); err != nil {
				return err
			}
			deleted++
			continue
		}
		if terminal && runID != 0 && agent != nil {
			if gh := clientFor(agent); gh != nil {
				jobs, err := gh.ListRunJobs(ctx, agent.Spec.Repository, runID)
				switch {
				case err != nil:
					logger.Error(err, "listing jobs; keeping run", "run", run.Name)
				case isGitHub && !AgentJobRan(jobs):
					if err := deleteRun(ctx, c, run); err != nil {
						return err
					}
					deleted++
					continue
				default:
					if d, ok := JobsDuration(jobs); ok {
						run.Status.DurationSeconds = d
					}
				}
			}
		}
		run.Status.Counted = false
		if err := c.Status().Update(ctx, run); err != nil {
			return fmt.Errorf("resetting run %s: %w", run.Name, err)
		}
		reset++
	}

	for i := range agents.Items {
		agent := &agents.Items[i]
		agent.Status.Daily = nil
		agent.Status.Totals = &v1alpha1.RunStats{}
		agent.Status.LastRun = nil
		if err := c.Status().Update(ctx, agent); err != nil {
			return fmt.Errorf("resetting agent %s: %w", agent.Name, err)
		}
	}
	logger.Info("rebuilt stats", "deletedRuns", deleted, "resetRuns", reset, "agents", len(agents.Items))
	return nil
}

func deleteRun(ctx context.Context, c client.Client, run *v1alpha1.AgentRun) error {
	if err := c.Delete(ctx, run); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting run %s: %w", run.Name, err)
	}
	return nil
}
