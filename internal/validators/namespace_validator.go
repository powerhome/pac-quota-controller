// Package validators contains validation logic for various resources
package validators

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/powerhome/pac-quota-controller/internal/models"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// NamespaceValidator validates namespace uniqueness across CRDs
type NamespaceValidator struct {
	client *kubernetes.Clientset
}

// NewNamespaceValidator creates a new NamespaceValidator
func NewNamespaceValidator() (*NamespaceValidator, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get in-cluster config: %v", err)
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %v", err)
	}

	return &NamespaceValidator{
		client: client,
	}, nil
}

// ValidateNamespaceUniqueness checks if a namespace is already referenced by another ClusterResourceQuota
func (v *NamespaceValidator) ValidateNamespaceUniqueness(ctx context.Context, namespace string) error {
	// Get all ClusterResourceQuotas
	raw, err := v.client.RESTClient().
		Get().
		AbsPath("/apis/pac.powerhome.com/v1alpha1/clusterresourcequotas").
		DoRaw(ctx)
	if err != nil {
		return fmt.Errorf("failed to get ClusterResourceQuotas: %v", err)
	}

	var quotaList models.ClusterResourceQuotaList
	if err := json.Unmarshal(raw, &quotaList); err != nil {
		return fmt.Errorf("failed to unmarshal ClusterResourceQuotas: %v", err)
	}

	// Check if namespace is already referenced
	for _, quota := range quotaList.Items {
		for _, ns := range quota.Spec.Namespaces {
			if ns == namespace {
				return fmt.Errorf("namespace %s is already referenced by ClusterResourceQuota %s", namespace, quota.Name)
			}
		}
	}

	return nil
}

// ValidateNamespacesUniqueness checks if any of the provided namespaces are already referenced by another ClusterResourceQuota
func (v *NamespaceValidator) ValidateNamespacesUniqueness(ctx context.Context, namespaces []string) error {
	for _, ns := range namespaces {
		if err := v.ValidateNamespaceUniqueness(ctx, ns); err != nil {
			return err
		}
	}
	return nil
}

// ValidateNamespaceExists checks if a namespace exists in the cluster
func (v *NamespaceValidator) ValidateNamespaceExists(ctx context.Context, namespace string) error {
	_, err := v.client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("namespace %s does not exist: %v", namespace, err)
	}
	return nil
}

// ValidateNamespacesExist checks if all provided namespaces exist in the cluster
func (v *NamespaceValidator) ValidateNamespacesExist(ctx context.Context, namespaces []string) error {
	for _, ns := range namespaces {
		if err := v.ValidateNamespaceExists(ctx, ns); err != nil {
			return err
		}
	}
	return nil
}

// IsNamespaceIncluded checks if a namespace is included in the ClusterResourceQuota
func IsNamespaceIncluded(crq models.ClusterResourceQuota, namespace string) bool {
	return slices.Contains(crq.Spec.Namespaces, namespace)
}
