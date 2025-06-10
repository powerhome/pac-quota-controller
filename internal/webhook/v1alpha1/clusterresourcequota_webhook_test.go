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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"context"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("ClusterResourceQuota Webhook", func() {
	var (
		obj       *quotav1alpha1.ClusterResourceQuota
		validator ClusterResourceQuotaCustomValidator
	)

	It("should deny creation if a namespace is already owned by another CRQ", func() {
		// Existing CRQ owns ns1
		existingCRQ := &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: "crq-1"},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"env": "prod"}},
			},
			Status: quotav1alpha1.ClusterResourceQuotaStatus{
				Namespaces: []quotav1alpha1.ResourceQuotaStatusByNamespace{{Namespace: "ns1"}},
			},
		}
		ns1 := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns1", Labels: map[string]string{"env": "prod"}}}
		// New CRQ tries to select ns1
		obj = &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: "crq-2"},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"env": "prod"}},
			},
			Status: quotav1alpha1.ClusterResourceQuotaStatus{
				Namespaces: []quotav1alpha1.ResourceQuotaStatusByNamespace{{Namespace: "ns1"}},
			},
		}
		fakeClient := fake.NewClientBuilder().WithObjects(existingCRQ, ns1).Build()
		validator = ClusterResourceQuotaCustomValidator{Client: fakeClient}
		_, err := validator.ValidateCreate(context.Background(), obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("namespace 'ns1' is already owned"))
	})

	It("should allow creation if no namespace is owned by another CRQ", func() {
		// Existing CRQ owns ns2, new CRQ selects ns1
		existingCRQ := &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: "crq-1"},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"env": "prod"}},
			},
			Status: quotav1alpha1.ClusterResourceQuotaStatus{
				Namespaces: []quotav1alpha1.ResourceQuotaStatusByNamespace{{Namespace: "ns2"}},
			},
		}
		ns1 := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns1", Labels: map[string]string{"env": "prod"}}}
		obj = &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: "crq-2"},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"env": "prod"}},
			},
		}
		fakeClient := fake.NewClientBuilder().WithObjects(existingCRQ, ns1).Build()
		validator = ClusterResourceQuotaCustomValidator{Client: fakeClient}
		_, err := validator.ValidateCreate(context.Background(), obj)
		Expect(err).NotTo(HaveOccurred())
	})
})
