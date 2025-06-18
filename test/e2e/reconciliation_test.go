package e2e

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
)

const (
	Timeout  = time.Second * 10
	Interval = time.Millisecond * 250
)

var _ = Describe("ClusterResourceQuota Reconciliation", func() {
	It("Should reconcile and update status when a Pod is created in a selected namespace", func() {
		ctx := context.Background()
		crqName := "test-crq-reconciliation"
		nsName := "test-ns-reconciliation"
		label := map[string]string{"crq-test": "reconciliation"}

		// 1. Create a ClusterResourceQuota to track pods
		crq := &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: crqName,
			},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: label,
				},
				Hard: quotav1alpha1.ResourceList{
					corev1.ResourcePods: resource.MustParse("5"),
				},
			},
		}
		Expect(k8sClient.Create(ctx, crq)).To(Succeed())

		// 2. Create a namespace that matches the selector
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   nsName,
				Labels: label,
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		// 3. Create a Pod in the namespace
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: nsName,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "test-container",
						Image: "busybox",
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())

		// 4. Wait and verify that the CRQ status is updated
		Eventually(func() error {
			updatedCrq := &quotav1alpha1.ClusterResourceQuota{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: crqName}, updatedCrq); err != nil {
				return err
			}

			if len(updatedCrq.Status.Namespaces) == 0 {
				return fmt.Errorf("CRQ status is not updated yet")
			}

			// Since the usage calculation is a placeholder, we just check if the namespace appears in the status
			found := false
			for _, nsStatus := range updatedCrq.Status.Namespaces {
				if nsStatus.Namespace == nsName {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("namespace %s not found in CRQ status", nsName)
			}

			return nil
		}, Timeout, Interval).Should(Succeed())

		// Cleanup
		Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
		Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
		Expect(k8sClient.Delete(ctx, crq)).To(Succeed())
	})
})
