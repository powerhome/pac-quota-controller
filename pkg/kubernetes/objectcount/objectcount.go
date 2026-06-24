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
	"k8s.io/apimachinery/pkg/api/meta"
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

// listConstructors maps each supported `objectcount`-style resource name to a
// factory that returns a typed empty list. The switch table that used to live
// in CalculateUsage was 10 identical branches — this map is the same data
// without the duplication.
var listConstructors = map[corev1.ResourceName]func() client.ObjectList{
	// There is always a kube-root-ca.crt configmap in each namespace
	"configmaps":             func() client.ObjectList { return &corev1.ConfigMapList{} },
	"secrets":                func() client.ObjectList { return &corev1.SecretList{} },
	"replicationcontrollers": func() client.ObjectList { return &corev1.ReplicationControllerList{} },
	"deployments.apps":       func() client.ObjectList { return &appsv1.DeploymentList{} },
	"statefulsets.apps":      func() client.ObjectList { return &appsv1.StatefulSetList{} },
	"daemonsets.apps":        func() client.ObjectList { return &appsv1.DaemonSetList{} },
	"jobs.batch":             func() client.ObjectList { return &batchv1.JobList{} },
	"cronjobs.batch":         func() client.ObjectList { return &batchv1.CronJobList{} },
	"horizontalpodautoscalers.autoscaling": func() client.ObjectList {
		return &autoscalingv1.HorizontalPodAutoscalerList{}
	},
	"ingresses.networking.k8s.io": func() client.ObjectList { return &networkingv1.IngressList{} },
}

// CalculateUsage returns the count of the specified resource in the namespace.
func (c *ObjectCountCalculator) CalculateUsage(
	ctx context.Context,
	namespace string,
	resourceName corev1.ResourceName) (resource.Quantity, error) {
	correlationID := quota.GetCorrelationID(ctx)

	newList, ok := listConstructors[resourceName]
	if !ok {
		return resource.Quantity{}, nil
	}

	list := newList()
	if err := c.Client.List(ctx, list, client.InNamespace(namespace)); err != nil {
		c.logger.Error("Failed to calculate object count usage",
			zap.String("correlation_id", correlationID),
			zap.String("namespace", namespace),
			zap.String("resource", string(resourceName)),
			zap.Error(err))
		return resource.Quantity{}, err
	}

	count := int64(meta.LenList(list))
	c.logger.Debug("Calculated object count usage",
		zap.String("correlation_id", correlationID),
		zap.String("namespace", namespace),
		zap.String("resource", string(resourceName)),
		zap.Int64("count", count))

	return *resource.NewQuantity(count, resource.DecimalSI), nil
}
