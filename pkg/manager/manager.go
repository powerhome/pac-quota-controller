package manager

import (
	"context"
	"os"
	"time"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/internal/controller"
	"github.com/powerhome/pac-quota-controller/pkg/config"
	pkglogger "github.com/powerhome/pac-quota-controller/pkg/logger"
	"go.uber.org/zap"

	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
)

var logger = pkglogger.L().Named("manager")

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

	// Setup manager options
	options := ctrl.Options{
		Scheme:           scheme,
		LeaderElection:   cfg.EnableLeaderElection,
		LeaderElectionID: "81307769.powerapp.cloud",
		PprofBindAddress: cfg.PprofBindAddress,
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
func SetupControllers(ctx context.Context, mgr ctrl.Manager, cfg *config.Config, loggerInstance *zap.Logger) error {
	if loggerInstance != nil {
		logger = loggerInstance.Named("setup")
	}

	if err := (&controller.ClusterResourceQuotaReconciler{
		Client:                   mgr.GetClient(),
		Scheme:                   mgr.GetScheme(),
		Config:                   cfg,
		ExcludeNamespaceLabelKey: cfg.ExcludeNamespaceLabelKey,
		ExcludedNamespaces:       cfg.ExcludedNamespaces,
	}).SetupWithManager(ctx, cfg, mgr); err != nil {
		logger.Error("unable to create controller", zap.Error(err), zap.String("controller", "ClusterResourceQuota"))
		return err
	}

	return nil
}

// Start starts the manager with graceful shutdown
func Start(mgr ctrl.Manager) {
	logger.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error("problem running manager", zap.Error(err))
		os.Exit(1)
	}
}
