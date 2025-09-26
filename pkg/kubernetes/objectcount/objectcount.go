// Package objectcount provides resource calculators for generic object count resources.
package objectcount

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes"
)

// ObjectCountCalculator implements usage.ResourceCalculatorInterface for generic object count resources.
type ObjectCountCalculator struct {
	Client kubernetes.Interface
}

func NewObjectCountCalculator(client kubernetes.Interface) *ObjectCountCalculator {
	return &ObjectCountCalculator{
		Client: client,
	}
}

// CalculateUsage returns the count of the specified resource in the namespace.
func (c *ObjectCountCalculator) CalculateUsage(
	ctx context.Context,
	namespace string,
	resourceName corev1.ResourceName) (resource.Quantity, error) {
	var count int64
	var err error

	switch resourceName {
	// There is always a kube-root-ca.crt configmap in each namespace
	case "configmaps":
		list, e := c.Client.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{})
		count, err = int64(len(list.Items)), e
	case "secrets":
		list, e := c.Client.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{})
		count, err = int64(len(list.Items)), e
	case "replicationcontrollers":
		list, e := c.Client.CoreV1().ReplicationControllers(namespace).List(ctx, metav1.ListOptions{})
		count, err = int64(len(list.Items)), e
	case "deployments.apps":
		list, e := c.Client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
		count, err = int64(len(list.Items)), e
	case "statefulsets.apps":
		list, e := c.Client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
		count, err = int64(len(list.Items)), e
	case "daemonsets.apps":
		list, e := c.Client.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{})
		count, err = int64(len(list.Items)), e
	case "jobs.batch":
		list, e := c.Client.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
		count, err = int64(len(list.Items)), e
	case "cronjobs.batch":
		list, e := c.Client.BatchV1().CronJobs(namespace).List(ctx, metav1.ListOptions{})
		count, err = int64(len(list.Items)), e
	case "horizontalpodautoscalers.autoscaling":
		list, e := c.Client.AutoscalingV1().HorizontalPodAutoscalers(namespace).List(ctx, metav1.ListOptions{})
		count, err = int64(len(list.Items)), e
	case "ingresses.networking.k8s.io":
		list, e := c.Client.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{})
		count, err = int64(len(list.Items)), e
	default:
		return resource.Quantity{}, nil
	}

	if err != nil {
		return resource.Quantity{}, err
	}
	return *resource.NewQuantity(count, resource.DecimalSI), nil
}
