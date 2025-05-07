package models

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterResourceQuota represents a resource quota that spans multiple namespaces
type ClusterResourceQuota struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterResourceQuotaSpec   `json:"spec,omitempty"`
	Status ClusterResourceQuotaStatus `json:"status,omitempty"`
}

// ClusterResourceQuotaSpec defines the desired state of ClusterResourceQuota
type ClusterResourceQuotaSpec struct {
	Namespaces []string          `json:"namespaces"`
	Hard       ResourceQuotaHard `json:"hard"`
}

// ResourceQuotaHard defines the hard limits for resources
type ResourceQuotaHard struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
}

// ClusterResourceQuotaStatus defines the observed state of ClusterResourceQuota
type ClusterResourceQuotaStatus struct {
	Total      ResourceQuotaStatus `json:"total,omitempty"`
	Namespaces []NamespaceStatus   `json:"namespaces,omitempty"`
}

// ResourceQuotaStatus represents the current usage of resources
type ResourceQuotaStatus struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
}

// NamespaceStatus represents the status of a namespace in the quota
type NamespaceStatus struct {
	Namespace string         `json:"namespace"`
	Status    ResourceStatus `json:"status"`
}

// ResourceStatus represents the resource usage in a namespace
type ResourceStatus struct {
	Used ResourceQuotaStatus `json:"used"`
}

// ClusterResourceQuotaList contains a list of ClusterResourceQuota
type ClusterResourceQuotaList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterResourceQuota `json:"items"`
}
