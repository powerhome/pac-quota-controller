// Package repositories contains implementations for retrieving and managing resources
package repositories

import (
	"context"
	"fmt"

	"github.com/powerhome/pac-quota-controller/internal/models"
	"github.com/powerhome/pac-quota-controller/pkg/kube"
	"github.com/powerhome/pac-quota-controller/pkg/logging"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var (
	logger = logging.NewLogger()

	// Define the GVR (Group, Version, Resource) for ClusterResourceQuota
	quotaGVR = schema.GroupVersionResource{
		Group:    "pac.powerhome.com",
		Version:  "v1alpha1",
		Resource: "clusterresourcequotas",
	}
)

// QuotaRepository provides methods for interacting with ClusterResourceQuotas
type QuotaRepository struct {
	client dynamic.Interface
}

// NewQuotaRepository creates a new QuotaRepository
func NewQuotaRepository() (*QuotaRepository, error) {
	client, err := kube.GetDynamicClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get dynamic client: %v", err)
	}

	return &QuotaRepository{
		client: client,
	}, nil
}

// GetAll retrieves all ClusterResourceQuota objects from the cluster
func (r *QuotaRepository) GetAll(ctx context.Context) ([]models.ClusterResourceQuota, error) {
	// Use dynamic client to list ClusterResourceQuotas
	unstList, err := r.client.Resource(quotaGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		logger.Error("failed to list ClusterResourceQuotas", zap.Error(err))
		return nil, fmt.Errorf("failed to fetch ClusterResourceQuotas: %v", err)
	}

	logger.Debug("fetched ClusterResourceQuotas", zap.Int("count", len(unstList.Items)))

	// Convert unstructured list to strongly typed list
	var quotaList []models.ClusterResourceQuota
	for _, item := range unstList.Items {
		quota, err := convertToClusterResourceQuota(item)
		if err != nil {
			logger.Error("failed to convert unstructured to ClusterResourceQuota",
				zap.String("name", item.GetName()),
				zap.Error(err))
			continue
		}
		quotaList = append(quotaList, quota)
	}

	return quotaList, nil
}

// GetByName retrieves a specific ClusterResourceQuota by name
func (r *QuotaRepository) GetByName(ctx context.Context, name string) (*models.ClusterResourceQuota, error) {
	unst, err := r.client.Resource(quotaGVR).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch ClusterResourceQuota %s: %v", name, err)
	}

	quota, err := convertToClusterResourceQuota(*unst)
	if err != nil {
		return nil, fmt.Errorf("failed to convert unstructured to ClusterResourceQuota %s: %v", name, err)
	}

	return &quota, nil
}

// FindQuotasContainingNamespace finds all ClusterResourceQuotas that include the specified namespace
func (r *QuotaRepository) FindQuotasContainingNamespace(ctx context.Context, namespace string) ([]models.ClusterResourceQuota, error) {
	quotas, err := r.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	var result []models.ClusterResourceQuota
	for _, quota := range quotas {
		for _, ns := range quota.Spec.Namespaces {
			if ns == namespace {
				result = append(result, quota)
				break
			}
		}
	}

	return result, nil
}

// convertToClusterResourceQuota converts an unstructured object to a ClusterResourceQuota
func convertToClusterResourceQuota(unst unstructured.Unstructured) (models.ClusterResourceQuota, error) {
	var quota models.ClusterResourceQuota

	// Set basic metadata
	quota.Name = unst.GetName()
	quota.Namespace = unst.GetNamespace()

	// Extract namespaces from spec
	namespaces, found, err := unstructured.NestedStringSlice(unst.Object, "spec", "namespaces")
	if err != nil {
		return quota, fmt.Errorf("error extracting namespaces: %v", err)
	}
	if found {
		quota.Spec.Namespaces = namespaces
	}

	// Extract hard limits
	hard, found, err := unstructured.NestedMap(unst.Object, "spec", "hard")
	if err != nil {
		return quota, fmt.Errorf("error extracting hard limits: %v", err)
	}
	if found {
		cpu, _ := hard["cpu"].(string)
		memory, _ := hard["memory"].(string)
		quota.Spec.Hard.CPU = cpu
		quota.Spec.Hard.Memory = memory
	}

	return quota, nil
}
