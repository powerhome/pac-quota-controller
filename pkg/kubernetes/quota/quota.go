package quota

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
)

// CRQClient encapsulates logic for working with ClusterResourceQuotas
type CRQClient struct {
	Client client.Client
}

func NewCRQClient(c client.Client) *CRQClient {
	return &CRQClient{
		Client: c,
	}
}

// ListAllCRQs returns all ClusterResourceQuotas in the cluster.
func (c *CRQClient) ListAllCRQs(ctx context.Context) ([]quotav1alpha1.ClusterResourceQuota, error) {
	if c.Client == nil {
		return nil, fmt.Errorf("CRQClient is not configured")
	}
	var crqList quotav1alpha1.ClusterResourceQuotaList
	if err := c.Client.List(ctx, &crqList); err != nil {
		return nil, fmt.Errorf("failed to list ClusterResourceQuotas: %w", err)
	}
	return crqList.Items, nil
}

// GetCRQByNamespace returns the ClusterResourceQuota that selects the given Namespace.
// If more than one CRQ matches, it returns an error listing the matching CRQs.
func (c *CRQClient) GetCRQByNamespace(
	ctx context.Context,
	ns *corev1.Namespace,
) (*quotav1alpha1.ClusterResourceQuota, error) {
	crqs, err := c.ListAllCRQs(ctx)
	if err != nil {
		return nil, err
	}

	var matches []quotav1alpha1.ClusterResourceQuota
	for _, crq := range crqs {
		ok, err := c.NamespaceMatchesCRQ(ns, &crq)
		if err != nil {
			return nil, err
		}
		if ok {
			matches = append(matches, crq)
		}
	}

	if len(matches) == 0 {
		return nil, nil // No matching CRQ found
	}
	if len(matches) > 1 {
		names := make([]string, len(matches))
		for i, crq := range matches {
			names[i] = crq.Name
		}
		return nil, fmt.Errorf("multiple ClusterResourceQuotas select namespace %q: %v", ns.Name, names)
	}
	return &matches[0], nil
}

// NamespaceMatchesCRQ returns true if the namespace matches the CRQ's selector.
func (c *CRQClient) NamespaceMatchesCRQ(ns *corev1.Namespace, crq *quotav1alpha1.ClusterResourceQuota) (bool, error) {
	if crq.Spec.NamespaceSelector == nil {
		return false, nil
	}
	selector, err := metav1.LabelSelectorAsSelector(crq.Spec.NamespaceSelector)
	if err != nil {
		return false, err
	}
	return selector.Matches(labels.Set(ns.Labels)), nil
}

// GetNamespacesFromStatus extracts the list of namespaces from the CRQ's status.
func (c *CRQClient) GetNamespacesFromStatus(crq *quotav1alpha1.ClusterResourceQuota) []string {
	if crq.Status.Namespaces == nil {
		return nil
	}
	namespaces := make([]string, len(crq.Status.Namespaces))
	for i, nsStatus := range crq.Status.Namespaces {
		namespaces[i] = nsStatus.Namespace
	}
	return namespaces
}
