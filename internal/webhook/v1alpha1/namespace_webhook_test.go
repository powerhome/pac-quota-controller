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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes"
)

var _ = Describe("Namespace Webhook", func() {
	var (
		ctx       context.Context
		validator NamespaceCustomValidator
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("ValidateCreate", func() {
		It("should always allow namespace creation", func() {
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-namespace",
					Labels: map[string]string{"quota": "limited"},
				},
			}

			fakeClient := fake.NewClientBuilder().Build()
			validator = NamespaceCustomValidator{
				Client:    fakeClient,
				crqClient: kubernetes.NewCRQClient(fakeClient),
			}

			warnings, err := validator.ValidateCreate(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeNil())
		})

		It("should reject invalid objects", func() {
			notANamespace := &corev1.Pod{}

			fakeClient := fake.NewClientBuilder().Build()
			validator = NamespaceCustomValidator{
				Client:    fakeClient,
				crqClient: kubernetes.NewCRQClient(fakeClient),
			}

			_, err := validator.ValidateCreate(ctx, notANamespace)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("expected a Namespace object"))
		})
	})

	Describe("ValidateUpdate", func() {
		It("should allow updates when labels don't change", func() {
			oldNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-namespace",
					Labels: map[string]string{"quota": "limited"},
				},
			}
			newNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-namespace",
					Labels: map[string]string{"quota": "limited"}, // Same labels
				},
				Spec: corev1.NamespaceSpec{
					Finalizers: []corev1.FinalizerName{"test-finalizer"}, // Different spec
				},
			}

			fakeClient := fake.NewClientBuilder().Build()
			validator = NamespaceCustomValidator{
				Client:    fakeClient,
				crqClient: kubernetes.NewCRQClient(fakeClient),
			}

			warnings, err := validator.ValidateUpdate(ctx, oldNS, newNS)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeNil())
		})

		It("should allow updates when labels change but no CRQs exist", func() {
			oldNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-namespace",
					Labels: map[string]string{"quota": "limited"},
				},
			}
			newNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-namespace",
					Labels: map[string]string{"quota": "unlimited"}, // Different labels
				},
			}

			fakeClient := fake.NewClientBuilder().Build() // No CRQs in cluster
			validator = NamespaceCustomValidator{
				Client:    fakeClient,
				crqClient: kubernetes.NewCRQClient(fakeClient),
			}

			warnings, err := validator.ValidateUpdate(ctx, oldNS, newNS)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeNil())
		})

		It("should allow updates when labels change and namespace matches only one CRQ", func() {
			// Create one CRQ that will match the new namespace labels
			crq := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "test-crq"},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"quota": "unlimited"},
					},
				},
			}

			oldNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-namespace",
					Labels: map[string]string{"quota": "limited"},
				},
			}
			newNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-namespace",
					Labels: map[string]string{"quota": "unlimited"}, // Will match the CRQ
				},
			}

			fakeClient := fake.NewClientBuilder().WithObjects(crq).Build()
			validator = NamespaceCustomValidator{
				Client:    fakeClient,
				crqClient: kubernetes.NewCRQClient(fakeClient),
			}

			warnings, err := validator.ValidateUpdate(ctx, oldNS, newNS)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeNil())
		})

		It("should deny updates when labels change and namespace would be selected by multiple CRQs", func() {
			// Create two CRQs that will both match the new namespace labels
			crq1 := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "test-crq-1"},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"quota": "unlimited"},
					},
				},
			}
			crq2 := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "test-crq-2"},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"quota": "unlimited"},
					},
				},
			}

			oldNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-namespace",
					Labels: map[string]string{"quota": "limited"},
				},
			}
			newNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-namespace",
					Labels: map[string]string{"quota": "unlimited"}, // Will match both CRQs
				},
			}

			fakeClient := fake.NewClientBuilder().WithObjects(crq1, crq2).Build()
			validator = NamespaceCustomValidator{
				Client:    fakeClient,
				crqClient: kubernetes.NewCRQClient(fakeClient),
			}

			warnings, err := validator.ValidateUpdate(ctx, oldNS, newNS)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("would be selected by multiple ClusterResourceQuotas"))
			Expect(err.Error()).To(ContainSubstring("test-namespace"))
			Expect(warnings).To(BeNil())
		})

		It("should handle complex label selector scenarios", func() {
			// Create CRQs with different types of selectors
			crq1 := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "env-prod-crq"},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "prod"},
					},
				},
			}
			crq2 := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "team-frontend-crq"},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"team": "frontend"},
					},
				},
			}

			oldNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-namespace",
					Labels: map[string]string{"env": "staging"},
				},
			}
			newNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-namespace",
					Labels: map[string]string{
						"env":  "prod",     // Matches crq1
						"team": "frontend", // Matches crq2
					},
				},
			}

			fakeClient := fake.NewClientBuilder().WithObjects(crq1, crq2).Build()
			validator = NamespaceCustomValidator{
				Client:    fakeClient,
				crqClient: kubernetes.NewCRQClient(fakeClient),
			}

			warnings, err := validator.ValidateUpdate(ctx, oldNS, newNS)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("would be selected by multiple ClusterResourceQuotas"))
			Expect(warnings).To(BeNil())
		})

		It("should reject invalid old namespace objects", func() {
			notANamespace := &corev1.Pod{}
			newNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-namespace"},
			}

			fakeClient := fake.NewClientBuilder().Build()
			validator = NamespaceCustomValidator{
				Client:    fakeClient,
				crqClient: kubernetes.NewCRQClient(fakeClient),
			}

			_, err := validator.ValidateUpdate(ctx, notANamespace, newNS)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("expected a Namespace object for the oldObj"))
		})

		It("should reject invalid new namespace objects", func() {
			oldNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-namespace"},
			}
			notANamespace := &corev1.Pod{}

			fakeClient := fake.NewClientBuilder().Build()
			validator = NamespaceCustomValidator{
				Client:    fakeClient,
				crqClient: kubernetes.NewCRQClient(fakeClient),
			}

			_, err := validator.ValidateUpdate(ctx, oldNS, notANamespace)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("expected a Namespace object for the newObj"))
		})

		It("should handle namespaces with nil labels", func() {
			oldNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-namespace",
					Labels: nil, // nil labels
				},
			}
			newNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-namespace",
					Labels: map[string]string{"quota": "limited"}, // non-nil labels
				},
			}

			crq := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "test-crq"},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"quota": "limited"},
					},
				},
			}

			fakeClient := fake.NewClientBuilder().WithObjects(crq).Build()
			validator = NamespaceCustomValidator{
				Client:    fakeClient,
				crqClient: kubernetes.NewCRQClient(fakeClient),
			}

			warnings, err := validator.ValidateUpdate(ctx, oldNS, newNS)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeNil())
		})
	})

	Describe("ValidateDelete", func() {
		It("should always allow namespace deletion", func() {
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-namespace",
					Labels: map[string]string{"quota": "limited"},
				},
			}

			fakeClient := fake.NewClientBuilder().Build()
			validator = NamespaceCustomValidator{
				Client:    fakeClient,
				crqClient: kubernetes.NewCRQClient(fakeClient),
			}

			warnings, err := validator.ValidateDelete(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeNil())
		})

		// TODO: add tests to check the exception of listing crqs

		It("should reject invalid objects", func() {
			notANamespace := &corev1.Pod{}

			fakeClient := fake.NewClientBuilder().Build()
			validator = NamespaceCustomValidator{
				Client:    fakeClient,
				crqClient: kubernetes.NewCRQClient(fakeClient),
			}

			_, err := validator.ValidateDelete(ctx, notANamespace)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("expected a Namespace object"))
		})
	})
})
