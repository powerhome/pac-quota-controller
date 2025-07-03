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
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/pod"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var podlog = logf.Log.WithName("pod-resource")

// SetupPodWebhookWithManager registers the webhook for Pod in the manager.
func SetupPodWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&corev1.Pod{}).
		WithValidator(&PodCustomValidator{
			Client:    mgr.GetClient(),
			crqClient: quota.NewCRQClient(mgr.GetClient()),
		}).
		Complete()
}

// +kubebuilder:webhook:path=/validate--v1-pod,mutating=false,failurePolicy=fail,sideEffects=None,groups="",resources=pods,verbs=create;update,versions=v1,name=vpod-v1alpha1.powerapp.cloud,admissionReviewVersions=v1

// PodCustomValidator struct is responsible for validating the Pod resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type PodCustomValidator struct {
	client.Client
	crqClient quota.CRQClientInterface
}

var _ webhook.CustomValidator = &PodCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Pod.
func (v *PodCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	podObj, ok := obj.(*corev1.Pod)
	if !ok {
		return nil, fmt.Errorf("expected a Pod object but got %T", obj)
	}
	podlog.Info("Validation for Pod upon creation", "name", podObj.GetName(), "namespace", podObj.GetNamespace())

	// If the pod is in a terminal state, allow creation
	// Terminal pods don't consume compute resources
	if pod.IsTerminal(podObj) {
		return nil, nil
	}

	// Get the namespace for this pod
	namespace := &corev1.Namespace{}
	if err := v.Get(ctx, types.NamespacedName{Name: podObj.GetNamespace()}, namespace); err != nil {
		return nil, fmt.Errorf("failed to get namespace %s: %w", podObj.GetNamespace(), err)
	}

	// Find which CRQ (if any) selects this namespace
	crq, err := v.crqClient.GetCRQByNamespace(ctx, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get CRQ for namespace %s: %w", namespace.Name, err)
	}

	// If no CRQ selects this namespace, allow the pod
	if crq == nil {
		return nil, nil
	}

	// Check if adding this pod would exceed any quotas
	if err := v.validatePodAgainstQuota(ctx, podObj, crq); err != nil {
		podlog.Info("Pod creation denied due to quota violation", "pod", podObj.Name, "namespace", podObj.Namespace, "crq", crq.Name, "error", err)
		return nil, err
	}

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Pod.
func (v *PodCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	newPod, ok := newObj.(*corev1.Pod)
	if !ok {
		return nil, fmt.Errorf("expected a Pod object for the newObj but got %T", newObj)
	}

	oldPod, ok := oldObj.(*corev1.Pod)
	if !ok {
		return nil, fmt.Errorf("expected a Pod object for the oldObj but got %T", oldObj)
	}

	podlog.Info("Validation for Pod upon update", "name", newPod.GetName(), "namespace", newPod.GetNamespace())

	// If the pod spec hasn't changed (only status updates), allow it
	if pod.SpecEqual(oldPod, newPod) {
		return nil, nil
	}

	// If the new pod is in a terminal state, allow the update
	// Terminal pods don't consume compute resources
	if pod.IsTerminal(newPod) {
		return nil, nil
	}

	// Get the namespace for this pod
	namespace := &corev1.Namespace{}
	if err := v.Get(ctx, types.NamespacedName{Name: newPod.GetNamespace()}, namespace); err != nil {
		return nil, fmt.Errorf("failed to get namespace %s: %w", newPod.GetNamespace(), err)
	}

	// Find which CRQ (if any) selects this namespace
	crq, err := v.crqClient.GetCRQByNamespace(ctx, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get CRQ for namespace %s: %w", namespace.Name, err)
	}

	// If no CRQ selects this namespace, allow the update
	if crq == nil {
		return nil, nil
	}

	// Check if the resource usage delta would exceed quotas
	if err := v.validatePodUpdateAgainstQuota(ctx, oldPod, newPod, crq); err != nil {
		podlog.Info("Pod update denied due to quota violation", "pod", newPod.Name, "namespace", newPod.Namespace, "crq", crq.Name, "error", err)
		return nil, err
	}

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Pod.
func (v *PodCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	podObj, ok := obj.(*corev1.Pod)
	if !ok {
		return nil, fmt.Errorf("expected a Pod object but got %T", obj)
	}
	podlog.Info("Validation for Pod upon deletion", "name", podObj.GetName())

	// Pod deletions don't need quota validation
	return nil, nil
}

// validatePodAgainstQuota checks if creating the pod would exceed CRQ limits
func (v *PodCustomValidator) validatePodAgainstQuota(ctx context.Context, newPod *corev1.Pod, crq *quotav1alpha1.ClusterResourceQuota) error {
	// Calculate resources that the new pod would consume
	podResources := make(map[corev1.ResourceName]resource.Quantity)

	// Calculate pod resource usage for each resource type in the CRQ
	for resourceName := range crq.Spec.Hard {
		podUsage := pod.CalculateResourceUsage(newPod, resourceName)
		if !podUsage.IsZero() {
			podResources[resourceName] = podUsage
		}
	}

	// If pod doesn't use any tracked resources, allow it
	if len(podResources) == 0 {
		return nil
	}

	// Get current CRQ to check current usage
	currentCRQ := &quotav1alpha1.ClusterResourceQuota{}
	if err := v.Get(ctx, types.NamespacedName{Name: crq.Name}, currentCRQ); err != nil {
		return fmt.Errorf("failed to get current CRQ status: %w", err)
	}

	// Check each resource against quota limits
	for resourceName, podUsage := range podResources {
		hardLimit, hasLimit := currentCRQ.Spec.Hard[resourceName]
		if !hasLimit {
			continue // No limit set for this resource
		}

		currentUsage, hasUsage := currentCRQ.Status.Total.Used[resourceName]
		if !hasUsage {
			currentUsage = resource.Quantity{}
		}

		// Calculate what the new total would be
		newTotal := currentUsage.DeepCopy()
		newTotal.Add(podUsage)

		// Check if it would exceed the limit
		if newTotal.Cmp(hardLimit) > 0 {
			return fmt.Errorf("pod would exceed ClusterResourceQuota %s limit for %s: current usage %s + pod usage %s = %s > limit %s",
				crq.Name, resourceName, currentUsage.String(), podUsage.String(), newTotal.String(), hardLimit.String())
		}
	}

	return nil
}

// validatePodUpdateAgainstQuota checks if updating the pod would exceed CRQ limits
func (v *PodCustomValidator) validatePodUpdateAgainstQuota(ctx context.Context, oldPod, newPod *corev1.Pod, crq *quotav1alpha1.ClusterResourceQuota) error {
	// Calculate the resource difference between old and new pod
	resourceDelta := make(map[corev1.ResourceName]resource.Quantity)

	// Calculate resource delta for each resource type in the CRQ
	for resourceName := range crq.Spec.Hard {
		oldUsage := pod.CalculateResourceUsage(oldPod, resourceName)
		newUsage := pod.CalculateResourceUsage(newPod, resourceName)

		// Calculate the delta (new - old)
		delta := newUsage.DeepCopy()
		delta.Sub(oldUsage)

		// Only track resources where there's a change
		if !delta.IsZero() {
			resourceDelta[resourceName] = delta
		}
	}

	// If no resource changes, allow the update
	if len(resourceDelta) == 0 {
		return nil
	}

	// Get current CRQ to check current usage
	currentCRQ := &quotav1alpha1.ClusterResourceQuota{}
	if err := v.Get(ctx, types.NamespacedName{Name: crq.Name}, currentCRQ); err != nil {
		return fmt.Errorf("failed to get current CRQ status: %w", err)
	}

	// Check each resource delta against quota limits
	for resourceName, delta := range resourceDelta {
		// Skip if the delta is negative (resource usage is decreasing)
		if delta.Sign() <= 0 {
			continue
		}

		hardLimit, hasLimit := currentCRQ.Spec.Hard[resourceName]
		if !hasLimit {
			continue // No limit set for this resource
		}

		currentUsage, hasUsage := currentCRQ.Status.Total.Used[resourceName]
		if !hasUsage {
			currentUsage = resource.Quantity{}
		}

		// Calculate what the new total would be
		newTotal := currentUsage.DeepCopy()
		newTotal.Add(delta)

		// Check if it would exceed the limit
		if newTotal.Cmp(hardLimit) > 0 {
			return fmt.Errorf("pod update would exceed ClusterResourceQuota %s limit for %s: current usage %s + delta %s = %s > limit %s",
				crq.Name, resourceName, currentUsage.String(), delta.String(), newTotal.String(), hardLimit.String())
		}
	}

	return nil
}
