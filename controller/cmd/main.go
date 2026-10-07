// Command agentic-controller syncs gh-aw agentic workflows from GitHub into Kubernetes.
package main

import (
	"context"
	"flag"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/rancher/agentic-extension/controller/api/v1alpha1"
	"github.com/rancher/agentic-extension/controller/internal/controllers"
	"github.com/rancher/agentic-extension/controller/internal/github"
)

func main() {
	var (
		metricsAddr    string
		probeAddr      string
		leaderElect    bool
		githubURL      string
		maxCollections int
		rebuildStats   bool
	)
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "Address for the metrics endpoint (0 disables it).")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "Address for health probes.")
	flag.BoolVar(&leaderElect, "leader-elect", false, "Enable leader election.")
	flag.StringVar(&githubURL, "github-api-url", github.DefaultBaseURL, "GitHub REST API base URL.")
	flag.IntVar(&maxCollections, "max-collections-per-sync", controllers.DefaultMaxCollectionsPerSync,
		"Maximum artifact collections per repository per sync interval.")
	flag.BoolVar(&rebuildStats, "rebuild-stats", false,
		"One-off: delete imported runs where the agent did not run, reset all Agent stats and recount. "+
			"Stats of already-pruned runs are lost; do not leave this enabled.")
	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	setupLog := ctrl.Log.WithName("setup")

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))

	cfg := ctrl.GetConfigOrDie()
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         leaderElect,
		LeaderElectionID:       "agentic-controller.agentic.rancher.io",
	})
	if err != nil {
		setupLog.Error(err, "creating manager")
		os.Exit(1)
	}

	factory := &controllers.GitHubFactory{
		BaseURL:    githubURL,
		Reader:     mgr.GetAPIReader(),
		RateLimits: controllers.NewRateLimits(),
	}
	limiter := controllers.NewCollectionLimiter(maxCollections)
	if rebuildStats {
		direct, err := client.New(cfg, client.Options{Scheme: scheme})
		if err != nil {
			setupLog.Error(err, "creating client for stats rebuild")
			os.Exit(1)
		}
		rebuildFactory := &controllers.GitHubFactory{BaseURL: githubURL, Reader: direct, RateLimits: factory.RateLimits}
		if err := controllers.RebuildStats(context.Background(), direct, rebuildFactory, setupLog.WithName("rebuild")); err != nil {
			setupLog.Error(err, "rebuilding stats")
			os.Exit(1)
		}
	}
	if err := (&controllers.AgentRepositoryReconciler{
		Client:    mgr.GetClient(),
		APIReader: mgr.GetAPIReader(),
		Scheme:    mgr.GetScheme(),
		Recorder:  mgr.GetEventRecorderFor("agentic-controller"),
		GitHub:    factory,
		Limiter:   limiter,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "setting up AgentRepository controller")
		os.Exit(1)
	}
	if err := (&controllers.AgentRunReconciler{
		Client:    mgr.GetClient(),
		APIReader: mgr.GetAPIReader(),
		Recorder:  mgr.GetEventRecorderFor("agentic-controller"),
		GitHub:    factory,
		Limiter:   limiter,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "setting up AgentRun controller")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "adding health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "adding ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager", "githubApiUrl", githubURL)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "running manager")
		os.Exit(1)
	}
}
