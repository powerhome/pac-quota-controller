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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/namespace"
)

var clusterresourcequotalog = logf.Log.WithName("clusterresourcequota-resource")

// SetupClusterResourceQuotaWebhookWithManager registers the webhook for ClusterResourceQuota in the manager.
func SetupClusterResourceQuotaWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&quotav1alpha1.ClusterResourceQuota{}).
		WithValidator(&ClusterResourceQuotaCustomValidator{Client: mgr.GetClient()}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-quota-powerapp-cloud-v1alpha1-clusterresourcequota,mutating=false,failurePolicy=fail,sideEffects=None,groups=quota.powerapp.cloud,resources=clusterresourcequotas,verbs=create;update,versions=v1alpha1,name=vclusterresourcequota-v1alpha1.powerapp.cloud,admissionReviewVersions=v1
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

	return namespace.ValidateNamespaceOwnership(ctx, v.Client, crq)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type ClusterResourceQuota.
func (v *ClusterResourceQuotaCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	crq, ok := newObj.(*quotav1alpha1.ClusterResourceQuota)
	if !ok {
		return nil, fmt.Errorf("expected a ClusterResourceQuota object for the newObj but got %T", newObj)
	}
	clusterresourcequotalog.Info("Validating namespace ownership on update", "name", crq.GetName())

	return namespace.ValidateNamespaceOwnership(ctx, v.Client, crq)
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
