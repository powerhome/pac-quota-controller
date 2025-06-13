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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/powerhome/pac-quota-controller/pkg/kubernetes"
	"github.com/powerhome/pac-quota-controller/pkg/utils"
)

// log is for logging in this package.
var namespacelog = logf.Log.WithName("namespace-resource")

// SetupNamespaceWebhookWithManager registers the webhook for Namespace in the manager.
func SetupNamespaceWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&corev1.Namespace{}).
		WithValidator(&NamespaceCustomValidator{
			Client:    mgr.GetClient(),
			crqClient: kubernetes.NewCRQClient(mgr.GetClient()),
		}).
		Complete()
}

// +kubebuilder:webhook:path=/validate--v1-namespace,mutating=false,failurePolicy=fail,sideEffects=None,groups="",resources=namespaces,verbs=update,versions=v1,name=vnamespace-v1.powerapp.cloud,admissionReviewVersions=v1
// NamespaceCustomValidator struct is responsible for validating the Namespace resource
// when it is created, updated, or deleted.
type NamespaceCustomValidator struct {
	Client    client.Client
	crqClient *kubernetes.CRQClient
}

var _ webhook.CustomValidator = &NamespaceCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Namespace.
func (v *NamespaceCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	namespace, ok := obj.(*corev1.Namespace)
	if !ok {
		return nil, fmt.Errorf("expected a Namespace object but got %T", obj)
	}
	namespacelog.Info("Validation for Namespace upon creation", "name", namespace.GetName())
	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Namespace.
func (v *NamespaceCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	namespace, ok := newObj.(*corev1.Namespace)
	if !ok {
		return nil, fmt.Errorf("expected a Namespace object for the newObj but got %T", newObj)
	}
	oldNamespace, ok := oldObj.(*corev1.Namespace)
	if !ok {
		return nil, fmt.Errorf("expected a Namespace object for the oldObj but got %T", oldObj)
	}
	namespacelog.Info("Validation for Namespace upon update", "name", namespace.GetName())

	// Only validate if labels changed
	if !utils.EqualStringMap(namespace.Labels, oldNamespace.Labels) {
		matching, err := v.crqClient.ListCRQsForNamespace(namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to list ClusterResourceQuotas for namespace %s: %w", namespace.Name, err)
		}
		if len(matching) > 1 {
			var names []string
			for _, crq := range matching {
				names = append(names, crq.Name)
			}
			return nil, fmt.Errorf("namespace '%s' would be selected by multiple ClusterResourceQuotas: %v", namespace.Name, names)
		}
	}

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Namespace.
func (v *NamespaceCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	namespace, ok := obj.(*corev1.Namespace)
	if !ok {
		return nil, fmt.Errorf("expected a Namespace object but got %T", obj)
	}
	namespacelog.Info("Validation for Namespace upon deletion", "name", namespace.GetName())
	return nil, nil
}
