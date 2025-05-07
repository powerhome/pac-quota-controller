// Package services contains business logic and coordination between components
package services

import (
	"context"
	"fmt"

	"github.com/powerhouse/pac-quota-controller/internal/models"
	"github.com/powerhouse/pac-quota-controller/internal/repositories"
	"github.com/powerhouse/pac-quota-controller/internal/validators"
)

// QuotaService provides business operations for ClusterResourceQuota resources
type QuotaService struct {
	quotaRepo    *repositories.QuotaRepository
	podRepo      *repositories.PodRepository
	resValidator *validators.ResourceValidator
	nsValidator  *validators.NamespaceValidator
}

// NewQuotaService creates a new QuotaService
func NewQuotaService() (*QuotaService, error) {
	quotaRepo, err := repositories.NewQuotaRepository()
	if err != nil {
		return nil, fmt.Errorf("failed to create quota repository: %v", err)
	}

	podRepo, err := repositories.NewPodRepository()
	if err != nil {
		return nil, fmt.Errorf("failed to create pod repository: %v", err)
	}

	resValidator, err := validators.NewResourceValidator()
	if err != nil {
		return nil, fmt.Errorf("failed to create resource validator: %v", err)
	}

	nsValidator, err := validators.NewNamespaceValidator()
	if err != nil {
		return nil, fmt.Errorf("failed to create namespace validator: %v", err)
	}

	return &QuotaService{
		quotaRepo:    quotaRepo,
		podRepo:      podRepo,
		resValidator: resValidator,
		nsValidator:  nsValidator,
	}, nil
}

// GetAllQuotas retrieves all ClusterResourceQuotas
func (s *QuotaService) GetAllQuotas(ctx context.Context) ([]models.ClusterResourceQuota, error) {
	return s.quotaRepo.GetAll(ctx)
}

// GetQuotasByNamespace retrieves all quotas that include the specified namespace
func (s *QuotaService) GetQuotasByNamespace(ctx context.Context, namespace string) ([]models.ClusterResourceQuota, error) {
	return s.quotaRepo.FindQuotasContainingNamespace(ctx, namespace)
}

// ValidatePodAgainstQuotas checks if a pod's resource requests would exceed any quota limits
func (s *QuotaService) ValidatePodAgainstQuotas(
	ctx context.Context,
	pod *models.PodResourceRequest,
	namespace string,
) error {
	// Find all quotas that include this namespace
	quotas, err := s.quotaRepo.FindQuotasContainingNamespace(ctx, namespace)
	if err != nil {
		return fmt.Errorf("failed to find quotas for namespace %s: %v", namespace, err)
	}

	// If no quota applies to this namespace, allow the pod
	if len(quotas) == 0 {
		return nil
	}

	// For each quota, check if adding this pod would exceed its limits
	for _, quota := range quotas {
		err := s.resValidator.ValidateQuotaNotExceeded(
			ctx,
			quota.Spec.Namespaces,
			quota.Spec.Hard.CPU,
			quota.Spec.Hard.Memory,
			pod.CPUMilliValue,
			pod.MemoryBytes,
		)
		if err != nil {
			return fmt.Errorf("quota validation failed for %s: %v", quota.Name, err)
		}
	}

	return nil
}

// ValidateQuotaCreation validates if a new ClusterResourceQuota can be created
func (s *QuotaService) ValidateQuotaCreation(ctx context.Context, quota *models.ClusterResourceQuota) error {
	// Validate that all namespaces exist
	if err := s.nsValidator.ValidateNamespacesExist(ctx, quota.Spec.Namespaces); err != nil {
		return err
	}

	// Validate namespace uniqueness (not already in another quota)
	if err := s.nsValidator.ValidateNamespacesUniqueness(ctx, quota.Spec.Namespaces); err != nil {
		return err
	}

	return nil
}
