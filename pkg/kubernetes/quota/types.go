package quota

import (
	"context"

	corev1 "k8s.io/api/core/v1"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
)

type contextKey string

const (
	// CorrelationIDKey is the key for the correlation ID in the context
	CorrelationIDKey contextKey = "correlation_id"
)

// GetCorrelationID safely retrieves the correlation ID from the context.
// It returns an empty string if the context is nil or the key is not found.
func GetCorrelationID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(CorrelationIDKey).(string)
	return id
}

//go:generate mockery

// CRQClientInterface defines the interface for ClusterResourceQuota operations
type CRQClientInterface interface {
	ListAllCRQs(ctx context.Context) ([]quotav1alpha1.ClusterResourceQuota, error)
	GetCRQByNamespace(ctx context.Context, ns *corev1.Namespace) (*quotav1alpha1.ClusterResourceQuota, error)
	NamespaceMatchesCRQ(ns *corev1.Namespace, crq *quotav1alpha1.ClusterResourceQuota) (bool, error)
	GetNamespacesFromStatus(crq *quotav1alpha1.ClusterResourceQuota) []string
}
