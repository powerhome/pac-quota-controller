package manager

import (
	"os"
	"time"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/internal/controller"
	"github.com/powerhome/pac-quota-controller/pkg/config"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/pod"
	"go.uber.org/zap"

	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
)

var setupLog = zap.NewNop()

// InitScheme initializes the runtime scheme
func InitScheme() *k8sruntime.Scheme {
	scheme := k8sruntime.NewScheme()

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(quotav1alpha1.AddToScheme(scheme))

	return scheme
}

// SetupManager creates and configures a controller manager
func SetupManager(
	cfg *config.Config,
	scheme *k8sruntime.Scheme,
) (ctrl.Manager, error) {
	// Determine leader election namespace
	leaderElectionNamespace := cfg.LeaderElectionNamespace
	if leaderElectionNamespace == "" {
		leaderElectionNamespace = cfg.OwnNamespace
	}

	// Setup manager options
	options := ctrl.Options{
		Scheme:                  scheme,
		LeaderElection:          cfg.EnableLeaderElection,
		LeaderElectionID:        "81307769.powerapp.cloud",
		LeaderElectionNamespace: leaderElectionNamespace,
	}

	// Configure leader election timing if enabled
	if cfg.EnableLeaderElection {
		leaseDuration := time.Duration(cfg.LeaderElectionLeaseDuration) * time.Second
		renewDeadline := time.Duration(cfg.LeaderElectionRenewDeadline) * time.Second
		retryPeriod := time.Duration(cfg.LeaderElectionRetryPeriod) * time.Second

		options.LeaseDuration = &leaseDuration
		options.RenewDeadline = &renewDeadline
		options.RetryPeriod = &retryPeriod
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), options)
	if err != nil {
		return nil, err
	}

	return mgr, nil
}

// SetupControllers sets up all controllers with the manager
func SetupControllers(mgr ctrl.Manager, cfg *config.Config) error {
	// Initialize compute resource calculator
	// Convert controller-runtime client to kubernetes clientset
	k8sConfig := mgr.GetConfig()
	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		setupLog.Error("unable to create kubernetes clientset", zap.Error(err))
		return err
	}
	computeCalculator := pod.NewPodResourceCalculator(clientset)

	if err := (&controller.ClusterResourceQuotaReconciler{
		Client:                   mgr.GetClient(),
		Scheme:                   mgr.GetScheme(),
		ComputeCalculator:        computeCalculator,
		ExcludeNamespaceLabelKey: cfg.ExcludeNamespaceLabelKey,
		ExcludedNamespaces:       cfg.ExcludedNamespaces,
		OwnNamespace:             cfg.OwnNamespace,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error("unable to create controller", zap.Error(err), zap.String("controller", "ClusterResourceQuota"))
		return err
	}

	return nil
}

// Start starts the manager with graceful shutdown
func Start(mgr ctrl.Manager) {
	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error("problem running manager", zap.Error(err))
		os.Exit(1)
	}
}
