package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ResourceList is a set of (resource name, quantity) pairs.
type ResourceList corev1.ResourceList

// ResourceQuotaStatus defines the enforced hard limits and observed use.
type ResourceQuotaStatus struct {
	// Hard is the set of enforced hard limits for each named resource (see ClusterResourceQuotaSpec for examples).
	// +optional
	Hard ResourceList `json:"hard,omitempty"`

	// Used is the current observed total usage of the resource in the namespace.
	// For object count quotas, this is the current count of each resource type (e.g., pods, services.loadbalancers, ingresses.nginx, etc.).
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
	// 'pods': '10' (Pod count)
	// 'services': '5' (Service count)
	// 'services.loadbalancers': '2' (Service type=LoadBalancer count)
	// 'services.nodeports': '3' (Service type=NodePort count)
	// 'configmaps': '20' (ConfigMap count)
	// 'secrets': '15' (Secret count)
	// 'persistentvolumeclaims': '8' (PVC count)
	// 'replicationcontrollers': '4' (ReplicationController count)
	// 'deployments.apps': '6' (Deployment count)
	// 'statefulsets.apps': '2' (StatefulSet count)
	// 'daemonsets.apps': '2' (DaemonSet count)
	// 'jobs.batch': '5' (Job count)
	// 'cronjobs.batch': '3' (CronJob count)
	// 'horizontalpodautoscalers.autoscaling': '2' (HPA count)
	// 'ingresses.networking.k8s.io': '3' (Ingress count)
	//
	// ...and so on for all supported native and extended resource types.
	// +optional
	Hard ResourceList `json:"hard,omitempty"`

	// NamespaceSelector selects the namespaces to which this quota applies.
	// This is specific to ClusterResourceQuota and allows quota limits to span across
	// multiple namespaces that match the selector.
	// +required
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector"`

	// ScopeSelector is a collection of scope filters expressed using ScopeSelectorOperator
	// in combination with possible values, ANDed with Scopes. Scopes filter pods only:
	// non-pod resources in Hard are rejected at admission when any scope is set.
	// Example: scopeSelector.matchExpressions: [{operator: In, scopeName: PriorityClass, values: ['high']}]
	// +optional
	// +kubebuilder:validation:XValidation:rule="self.matchExpressions.all(m, m.scopeName in ['Terminating','NotTerminating','BestEffort','NotBestEffort','PriorityClass','CrossNamespacePodAffinity'])",message="scopeSelector: invalid scope name"
	// +kubebuilder:validation:XValidation:rule="self.matchExpressions.all(m, m.operator in ['In','NotIn','Exists','DoesNotExist'])",message="scopeSelector: invalid operator"
	// +kubebuilder:validation:XValidation:rule="self.matchExpressions.all(m, m.scopeName == 'PriorityClass' || m.operator == 'Exists')",message="scopeSelector: only the PriorityClass scope supports operators other than Exists"
	// +kubebuilder:validation:XValidation:rule="self.matchExpressions.all(m, (m.operator == 'In' || m.operator == 'NotIn') == (size(m.values) > 0))",message="scopeSelector: values must be non-empty for In/NotIn and empty for Exists/DoesNotExist"
	// +kubebuilder:validation:XValidation:rule="!(self.matchExpressions.exists(m, m.scopeName == 'BestEffort') && self.matchExpressions.exists(m, m.scopeName == 'NotBestEffort'))",message="scopeSelector: BestEffort and NotBestEffort scopes are mutually exclusive"
	// +kubebuilder:validation:XValidation:rule="!(self.matchExpressions.exists(m, m.scopeName == 'Terminating') && self.matchExpressions.exists(m, m.scopeName == 'NotTerminating'))",message="scopeSelector: Terminating and NotTerminating scopes are mutually exclusive"
	ScopeSelector *corev1.ScopeSelector `json:"scopeSelector,omitempty"`

	// Scopes is a collection of filters that must all match each pod tracked by the quota.
	// If not specified, the quota matches all pods. Scopes filter pods only.
	// Available scopes are:
	// - Terminating: match pods where spec.activeDeadlineSeconds >= 0
	// - NotTerminating: match pods where spec.activeDeadlineSeconds is nil
	// - BestEffort: match pods that have best effort quality of service
	// - NotBestEffort: match pods that do not have best effort quality of service
	// - PriorityClass: match pods that have any priority class set
	// - CrossNamespacePodAffinity: match pods with cross-namespace pod (anti)affinity terms
	// +optional
	// +kubebuilder:validation:items:Enum=Terminating;NotTerminating;BestEffort;NotBestEffort;PriorityClass;CrossNamespacePodAffinity
	// +kubebuilder:validation:XValidation:rule="!('BestEffort' in self && 'NotBestEffort' in self)",message="BestEffort and NotBestEffort scopes are mutually exclusive"
	// +kubebuilder:validation:XValidation:rule="!('Terminating' in self && 'NotTerminating' in self)",message="Terminating and NotTerminating scopes are mutually exclusive"
	Scopes []corev1.ResourceQuotaScope `json:"scopes,omitempty"`
}

// ClusterResourceQuotaStatus defines the observed state of ClusterResourceQuota.
type ClusterResourceQuotaStatus struct {
	// Total defines the actual enforced quota and its current usage across all namespaces
	// +optional
	Total ResourceQuotaStatus `json:"total"`

	// Namespaces slices the usage by namespace
	// +optional
	Namespaces []ResourceQuotaStatusByNamespace `json:"namespaces,omitempty"`
}

func (crqs *ClusterResourceQuotaStatus) GetNamespaces() []string {
	if crqs == nil || len(crqs.Namespaces) == 0 {
		return nil
	}

	nsList := make([]string, 0, len(crqs.Namespaces))
	for _, nsStatus := range crqs.Namespaces {
		nsList = append(nsList, nsStatus.Namespace)
	}
	return nsList
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=crq
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Namespaces",type="string",JSONPath=".status.namespaces[*].namespace",priority=1

// ClusterResourceQuota is the Schema for the clusterresourcequotas API.
// It extends the standard Kubernetes ResourceQuota by allowing it to be applied across multiple
// namespaces that match a label selector.
//
// Supported object count resources (for use in the 'hard' and 'used' fields):
//   - pods
//   - services
//   - services.loadbalancers
//   - services.nodeports
//   - configmaps
//   - secrets
//   - persistentvolumeclaims
//   - replicationcontrollers
//   - deployments.apps
//   - statefulsets.apps
//   - daemonsets.apps
//   - jobs.batch
//   - cronjobs.batch
//   - horizontalpodautoscalers.autoscaling
//   - ingresses.networking.k8s.io
//
// You may specify quotas for any of these resources. See the Helm chart documentation for details and examples.
type ClusterResourceQuota struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec ClusterResourceQuotaSpec `json:"spec"`
	// +optional
	Status ClusterResourceQuotaStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterResourceQuotaList contains a list of ClusterResourceQuota.
type ClusterResourceQuotaList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []ClusterResourceQuota `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterResourceQuota{}, &ClusterResourceQuotaList{})
}
