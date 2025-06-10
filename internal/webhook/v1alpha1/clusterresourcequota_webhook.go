/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"context"
	"fmt"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/internal/controller/namespaceselection"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var clusterresourcequotalog = logf.Log.WithName("clusterresourcequota-resource")

// SetupClusterResourceQuotaWebhookWithManager registers the webhook for ClusterResourceQuota in the manager.
func SetupClusterResourceQuotaWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&quotav1alpha1.ClusterResourceQuota{}).
		WithValidator(&ClusterResourceQuotaCustomValidator{Client: mgr.GetClient()}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-quota-powerapp-cloud-v1alpha1-clusterresourcequota,mutating=false,failurePolicy=fail,sideEffects=None,groups=quota.powerapp.cloud,resources=clusterresourcequotas,verbs=create;update,versions=v1alpha1,name=vclusterresourcequota-v1alpha1.kb.io,admissionReviewVersions=v1
// ClusterResourceQuotaCustomValidator struct is responsible for validating the ClusterResourceQuota resource
// when it is created, updated, or deleted.
type ClusterResourceQuotaCustomValidator struct {
	Client client.Client
}

var _ webhook.CustomValidator = &ClusterResourceQuotaCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type ClusterResourceQuota.
func (v *ClusterResourceQuotaCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	crq, ok := obj.(*quotav1alpha1.ClusterResourceQuota)
	if !ok {
		return nil, fmt.Errorf("expected a ClusterResourceQuota object but got %T", obj)
	}
	clusterresourcequotalog.Info("Validating namespace ownership on create", "name", crq.GetName())

	return validateNamespaceOwnershipWithAPI(ctx, v.Client, crq)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type ClusterResourceQuota.
func (v *ClusterResourceQuotaCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	crq, ok := newObj.(*quotav1alpha1.ClusterResourceQuota)
	if !ok {
		return nil, fmt.Errorf("expected a ClusterResourceQuota object for the newObj but got %T", newObj)
	}
	clusterresourcequotalog.Info("Validating namespace ownership on update", "name", crq.GetName())

	return validateNamespaceOwnershipWithAPI(ctx, v.Client, crq)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type ClusterResourceQuota.
func (v *ClusterResourceQuotaCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	clusterresourcequota, ok := obj.(*quotav1alpha1.ClusterResourceQuota)
	if !ok {
		return nil, fmt.Errorf("expected a ClusterResourceQuota object but got %T", obj)
	}
	clusterresourcequotalog.Info("Validation for ClusterResourceQuota upon deletion", "name", clusterresourcequota.GetName())

	return nil, nil
}

// validateNamespaceOwnershipWithAPI checks the API to ensure no namespace is already owned by another CRQ.
func validateNamespaceOwnershipWithAPI(ctx context.Context, c client.Client, crq *quotav1alpha1.ClusterResourceQuota) (admission.Warnings, error) {
	if crq.Spec.NamespaceSelector == nil {
		return nil, nil // If no selector, nothing to check
	}

	// Use the namespace selector utility to get intended namespaces for this CRQ
	selector, err := namespaceselection.NewLabelBasedNamespaceSelector(c, crq.Spec.NamespaceSelector)
	if err != nil {
		return nil, fmt.Errorf("failed to create namespace selector: %w", err)
	}
	intendedNamespaces, err := selector.GetSelectedNamespaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to select namespaces: %w", err)
	}
	if len(intendedNamespaces) == 0 {
		return nil, nil // No intended namespaces, nothing to check
	}
	myNamespaces := make(map[string]struct{}, len(intendedNamespaces))
	for _, ns := range intendedNamespaces {
		myNamespaces[ns] = struct{}{}
	}

	// List all CRQs
	crqList := &quotav1alpha1.ClusterResourceQuotaList{}
	if err := c.List(ctx, crqList); err != nil {
		return nil, fmt.Errorf("failed to list ClusterResourceQuotas: %w", err)
	}

	// Check for conflicts with other CRQs' status.namespaces
	conflicts := []string{}
	for _, otherCRQ := range crqList.Items {
		if otherCRQ.Name == crq.Name {
			continue
		}
		for _, nsStatus := range otherCRQ.Status.Namespaces {
			if _, conflict := myNamespaces[nsStatus.Namespace]; conflict {
				conflicts = append(conflicts, fmt.Sprintf("namespace '%s' is already owned by another ClusterResourceQuota", nsStatus.Namespace))
			}
		}
	}
	if len(conflicts) > 0 {
		return nil, fmt.Errorf("namespace ownership conflict: %s", conflicts)
	}
	return nil, nil
}
