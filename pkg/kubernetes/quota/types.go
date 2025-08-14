package quota

import (
	"context"

	corev1 "k8s.io/api/core/v1"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
)

//go:generate mockery

// CRQClientInterface defines the interface for ClusterResourceQuota operations
type CRQClientInterface interface {
	ListAllCRQs(ctx context.Context) ([]quotav1alpha1.ClusterResourceQuota, error)
	GetCRQByNamespace(ctx context.Context, ns *corev1.Namespace) (*quotav1alpha1.ClusterResourceQuota, error)
	NamespaceMatchesCRQ(ns *corev1.Namespace, crq *quotav1alpha1.ClusterResourceQuota) (bool, error)
	GetNamespacesFromStatus(crq *quotav1alpha1.ClusterResourceQuota) []string
}
