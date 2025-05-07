// Package repositories contains implementations for retrieving and managing resources
package repositories

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/powerhome/pac-quota-controller/pkg/kube"
)

// NamespaceRepository provides methods for interacting with Kubernetes namespaces
type NamespaceRepository struct {
	client *kubernetes.Clientset
}

// NewNamespaceRepository creates a new NamespaceRepository
func NewNamespaceRepository() (*NamespaceRepository, error) {
	client, err := kube.GetClientset()
	if err != nil {
		return nil, fmt.Errorf("failed to get Kubernetes client: %v", err)
	}

	return &NamespaceRepository{
		client: client,
	}, nil
}

// Get retrieves a namespace by name
func (r *NamespaceRepository) Get(ctx context.Context, name string) (exists bool, err error) {
	_, err = r.client.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		// Check if the error is "not found" vs other errors
		return false, fmt.Errorf("error retrieving namespace %s: %v", name, err)
	}
	return true, nil
}

// List retrieves all namespaces matching the specified label selector
func (r *NamespaceRepository) List(ctx context.Context, labelSelector string) ([]string, error) {
	namespaces, err := r.client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %v", err)
	}

	var namespaceNames []string
	for _, ns := range namespaces.Items {
		namespaceNames = append(namespaceNames, ns.Name)
	}

	return namespaceNames, nil
}
