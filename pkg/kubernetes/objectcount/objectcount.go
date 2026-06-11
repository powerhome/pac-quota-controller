// Package objectcount provides resource calculators for generic object count resources.
package objectcount

import (
	"context"

	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ObjectCountCalculator implements usage.ResourceCalculatorInterface for generic object count resources.
type ObjectCountCalculator struct {
	Client client.Client
	logger *zap.Logger
}

func NewObjectCountCalculator(c client.Client, logger *zap.Logger) *ObjectCountCalculator {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ObjectCountCalculator{
		Client: c,
		logger: logger.Named("object-count-calculator"),
	}
}

// CalculateUsage returns the count of the specified resource in the namespace.
func (c *ObjectCountCalculator) CalculateUsage(
	ctx context.Context,
	namespace string,
	resourceName corev1.ResourceName) (resource.Quantity, error) {
	correlationID := quota.GetCorrelationID(ctx)
	var count int64
	var err error

	opts := []client.ListOption{client.InNamespace(namespace)}
	switch resourceName {
	// There is always a kube-root-ca.crt configmap in each namespace
	case "configmaps":
		list := &corev1.ConfigMapList{}
		err = c.Client.List(ctx, list, opts...)
		count = int64(len(list.Items))
	case "secrets":
		list := &corev1.SecretList{}
		err = c.Client.List(ctx, list, opts...)
		count = int64(len(list.Items))
	case "replicationcontrollers":
		list := &corev1.ReplicationControllerList{}
		err = c.Client.List(ctx, list, opts...)
		count = int64(len(list.Items))
	case "deployments.apps":
		list := &appsv1.DeploymentList{}
		err = c.Client.List(ctx, list, opts...)
		count = int64(len(list.Items))
	case "statefulsets.apps":
		list := &appsv1.StatefulSetList{}
		err = c.Client.List(ctx, list, opts...)
		count = int64(len(list.Items))
	case "daemonsets.apps":
		list := &appsv1.DaemonSetList{}
		err = c.Client.List(ctx, list, opts...)
		count = int64(len(list.Items))
	case "jobs.batch":
		list := &batchv1.JobList{}
		err = c.Client.List(ctx, list, opts...)
		count = int64(len(list.Items))
	case "cronjobs.batch":
		list := &batchv1.CronJobList{}
		err = c.Client.List(ctx, list, opts...)
		count = int64(len(list.Items))
	case "horizontalpodautoscalers.autoscaling":
		list := &autoscalingv1.HorizontalPodAutoscalerList{}
		err = c.Client.List(ctx, list, opts...)
		count = int64(len(list.Items))
	case "ingresses.networking.k8s.io":
		list := &networkingv1.IngressList{}
		err = c.Client.List(ctx, list, opts...)
		count = int64(len(list.Items))
	default:
		return resource.Quantity{}, nil
	}

	if err != nil {
		c.logger.Error("Failed to calculate object count usage",
			zap.String("correlation_id", correlationID),
			zap.String("namespace", namespace),
			zap.String("resource", string(resourceName)),
			zap.Error(err))
		return resource.Quantity{}, err
	}

	c.logger.Debug("Calculated object count usage",
		zap.String("correlation_id", correlationID),
		zap.String("namespace", namespace),
		zap.String("resource", string(resourceName)),
		zap.Int64("count", count))

	return *resource.NewQuantity(count, resource.DecimalSI), nil
}
