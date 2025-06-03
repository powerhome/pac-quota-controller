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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ResourceList is a set of (resource name, quantity) pairs.
type ResourceList corev1.ResourceList

// ResourceQuotaStatus defines the enforced hard limits and observed use.
type ResourceQuotaStatus struct {
	// Hard is the set of enforced hard limits for each named resource.
	// +optional
	Hard ResourceList `json:"hard,omitempty"`

	// Used is the current observed total usage of the resource in the namespace.
	// +optional
	Used ResourceList `json:"used,omitempty"`
}

// ResourceQuotaStatusByNamespace gives status for a particular namespace
type ResourceQuotaStatusByNamespace struct {
	// Namespace the namespace this status applies to
	Namespace string `json:"namespace"`

	// Status indicates how many resources have been consumed by this namespace
	Status ResourceQuotaStatus `json:"status"`
}

// ClusterResourceQuotaSpec defines the desired state of ClusterResourceQuota.
type ClusterResourceQuotaSpec struct {
	// Hard is the set of desired hard limits for each named resource.
	// For example:
	// 'pods': '10'
	// 'requests.cpu': '1'
	// 'requests.memory': 1Gi
	// +optional
	Hard ResourceList `json:"hard,omitempty"`

	// NamespaceSelector selects the namespaces to which this quota applies.
	// This is specific to ClusterResourceQuota and allows quota limits to span across
	// multiple namespaces that match the selector.
	// +required
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector"`

	// ScopeSelector is also a collection of filters like scopes that must match each object tracked by a quota
	// but expressed using ScopeSelectorOperator in combination with possible values.
	// For example, to select objects where any container has a resource request that exceeds 100m CPU,
	// you would use: scopeSelector.matchExpressions: [{operator: In, scopeName: PriorityClass, values: ['high']}]
	// +optional
	ScopeSelector *corev1.ScopeSelector `json:"scopeSelector,omitempty"`

	// A collection of filters that must match each object tracked by a quota.
	// If not specified, the quota matches all objects.
	// Available scopes are:
	// - Terminating: match pods where spec.activeDeadlineSeconds >= 0
	// - NotTerminating: match pods where spec.activeDeadlineSeconds is nil
	// - BestEffort: match pods that have best effort quality of service
	// - NotBestEffort: match pods that do not have best effort quality of service
	// - PriorityClass: match pods that have the specified priority class
	// - CrossNamespacePodAffinity: match pods that have cross-namespace pod affinity terms
	// +optional
	Scopes []corev1.ResourceQuotaScope `json:"scopes,omitempty"`
}

// ClusterResourceQuotaStatus defines the observed state of ClusterResourceQuota.
type ClusterResourceQuotaStatus struct {
	// Total defines the actual enforced quota and its current usage across all namespaces
	// +optional
	Total ResourceQuotaStatus `json:"total,omitempty"`

	// Namespaces slices the usage by namespace
	// +optional
	Namespaces []ResourceQuotaStatusByNamespace `json:"namespaces,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=crq
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Namespaces",type="string",JSONPath=".status.namespaces[*].namespace",priority=1

// ClusterResourceQuota is the Schema for the clusterresourcequotas API.
// It extends the standard Kubernetes ResourceQuota by allowing it to be applied across multiple
// namespaces that match a label selector.
type ClusterResourceQuota struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterResourceQuotaSpec   `json:"spec,omitempty"`
	Status ClusterResourceQuotaStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterResourceQuotaList contains a list of ClusterResourceQuota.
type ClusterResourceQuotaList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterResourceQuota `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterResourceQuota{}, &ClusterResourceQuotaList{})
}
