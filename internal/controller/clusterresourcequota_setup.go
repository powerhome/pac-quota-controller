package controller

import (
	"context"
	"fmt"
	"time"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/config"
	"github.com/powerhome/pac-quota-controller/pkg/events"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/objectcount"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/pod"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/services"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/storage"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterResourceQuotaReconciler) SetupWithManager(ctx context.Context, cfg *config.Config, mgr ctrl.Manager) error {
	// Initialize logger
	if r.logger == nil {
		r.logger = zap.L().Named("clusterresourcequota-controller")
	}

	// Initialize the KubeClient using the manager's config if not already set
	if r.KubeClient == nil {
		cfg := mgr.GetConfig()
		r.KubeClient = kubernetes.NewForConfigOrDie(cfg)
	}
	k8sConfig := mgr.GetConfig()
	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return fmt.Errorf("unable to create kubernetes clientset: %w", err)
	}
	if r.crqClient == nil {
		r.crqClient = quota.NewCRQClient(r.Client, r.logger)
	}

	if r.StorageCalculator == nil {
		r.StorageCalculator = storage.NewStorageResourceCalculator(clientset, r.logger)
	}
	if r.ComputeCalculator == nil {
		r.ComputeCalculator = pod.NewPodResourceCalculator(clientset, r.logger)
	}
	if r.ServiceCalculator == nil {
		r.ServiceCalculator = services.NewServiceResourceCalculator(clientset, r.logger)
	}
	if r.ObjectCountCalculator == nil {
		r.ObjectCountCalculator = objectcount.NewObjectCountCalculator(clientset, r.logger)
	}

	// Initialize EventRecorder
	if r.EventRecorder == nil {
		r.EventRecorder = events.NewEventRecorder(
			mgr.GetEventRecorderFor("pac-quota-controller"),
			cfg.OwnNamespace,
			r.logger,
		)
	}

	// Initialize previous namespaces tracking
	if r.previousNamespacesByQuota == nil {
		r.previousNamespacesByQuota = make(map[string][]string)
	}
	if r.usageStateStore == nil {
		r.usageStateStore = newUsageStateStore()
	}

	// Load event cleanup configuration from multiple sources
	var cleanupConfig events.CleanupConfig

	if r.Config != nil && r.Config.EventsEnable {
		cleanupConfig, err = events.LoadEventCleanupConfig(
			r.Config.EventsConfigPath,
			r.Config.EventsTTL,
			r.Config.EventsMaxEventsPerCRQ,
			r.Config.EventsCleanupInterval,
		)
		if err != nil {
			r.logger.Warn("Failed to load event cleanup config, using defaults", zap.Error(err))
			cleanupConfig = events.DefaultCleanupConfig()
		}
	} else {
		// Events disabled or no config provided, use defaults but disable cleanup
		cleanupConfig = events.DefaultCleanupConfig()
		if r.Config != nil && !r.Config.EventsEnable {
			cleanupConfig.Enabled = false
		}
	}

	cleanupManager := events.NewEventCleanupManager(mgr.GetClient(), cleanupConfig, r.logger)

	// Start cleanup in background
	go func() {
		cleanupManager.Start(ctx)
	}()

	// Start periodic violation cache cleanup
	go func() {
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			r.EventRecorder.CleanupExpiredViolations()
		}
	}()

	r.logger.Info("Setting up ClusterResourceQuota controller")

	// Predicate to filter out updates to status subresource
	// This prevents reconcile loops caused by status updates
	resourcePredicate := resourceUpdatePredicate{}

	b := ctrl.NewControllerManagedBy(mgr).
		For(&quotav1alpha1.ClusterResourceQuota{})

	// Watch for changes to tracked resources and trigger reconciliation for associated CRQs
	watchedObjectTypes := []struct {
		obj   client.Object
		preds []predicate.Predicate
	}{
		{&corev1.Namespace{}, nil},
		{&corev1.Pod{}, []predicate.Predicate{resourcePredicate}},
		{&corev1.PersistentVolumeClaim{}, nil},
		{&corev1.Service{}, nil},
		// Generic object count resources
		{&corev1.ConfigMap{}, nil},
		{&corev1.Secret{}, nil},
		{&corev1.ReplicationController{}, nil},
		{&appsv1.Deployment{}, nil},
		{&appsv1.StatefulSet{}, nil},
		{&appsv1.DaemonSet{}, nil},
		{&batchv1.Job{}, nil},
		{&batchv1.CronJob{}, nil},
		{&autoscalingv1.HorizontalPodAutoscaler{}, nil},
		{&networkingv1.Ingress{}, nil},
	}
	for _, w := range watchedObjectTypes {
		b = b.Watches(
			w.obj,
			handler.EnqueueRequestsFromMapFunc(r.findQuotasForObject),
			builder.WithPredicates(w.preds...),
		)
	}

	return b.Named("clusterresourcequota").
		Complete(r)
}
