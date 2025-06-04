package manager

import (
	"os"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/internal/controller"
	"github.com/powerhome/pac-quota-controller/pkg/config"
	"github.com/powerhome/pac-quota-controller/pkg/health"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

var setupLog = ctrl.Log.WithName("setup.manager")

// InitScheme initializes the runtime scheme
func InitScheme() *k8sruntime.Scheme {
	scheme := k8sruntime.NewScheme()

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(quotav1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme

	return scheme
}

// SetupManager creates and configures a controller manager
func SetupManager(cfg *config.Config, scheme *k8sruntime.Scheme, metricsOpts server.Options, webhookServer webhook.Server) (ctrl.Manager, error) {
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsOpts,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: cfg.ProbeAddr,
		LeaderElection:         cfg.EnableLeaderElection,
		LeaderElectionID:       "81307769.powerapp.cloud",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})

	if err != nil {
		return nil, err
	}

	// Configure health checks
	health.SetupChecks(mgr)

	return mgr, nil
}

// SetupControllers sets up all controllers with the manager
func SetupControllers(mgr ctrl.Manager) error {
	if err := (&controller.ClusterResourceQuotaReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterResourceQuota")
		return err
	}
	// +kubebuilder:scaffold:builder

	return nil
}

// AddCertWatchers adds certificate watchers to the manager
func AddCertWatchers(mgr ctrl.Manager, metricsCertWatcher, webhookCertWatcher *certwatcher.CertWatcher) error {
	if metricsCertWatcher != nil {
		setupLog.Info("Adding metrics certificate watcher to manager")
		if err := mgr.Add(metricsCertWatcher); err != nil {
			setupLog.Error(err, "unable to add metrics certificate watcher to manager")
			return err
		}
	}

	if webhookCertWatcher != nil {
		setupLog.Info("Adding webhook certificate watcher to manager")
		if err := mgr.Add(webhookCertWatcher); err != nil {
			setupLog.Error(err, "unable to add webhook certificate watcher to manager")
			return err
		}
	}

	return nil
}

// Start starts the manager with graceful shutdown
func Start(mgr ctrl.Manager) {
	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
