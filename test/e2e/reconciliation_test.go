package e2e

import (
	"context"
	"fmt"
	"math/rand"
	"slices"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	Timeout  = time.Second * 10
	Interval = time.Millisecond * 250
)

var _ = Describe("ClusterResourceQuota Reconciliation", func() {
	var (
		suffix  string
		crqName string
		nsName  string
	)

	BeforeEach(func() {
		suffix = strconv.Itoa(rand.Intn(1000000))
		crqName = "crq-" + suffix
		nsName = "test-ns-" + suffix
	})

	It("Should reconcile and update status when a Pod is created in a selected namespace", func() {
		ctx := context.Background()
		label := map[string]string{"crq-test": "reconciliation-" + suffix}

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
		// TODO: This does nothing so far
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod-" + suffix,
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
			if !slices.Contains(updatedCrq.Status.GetNamespaces(), nsName) {
				return fmt.Errorf("namespace %s not found in CRQ status", nsName)
			}

			return nil
		}, Timeout, Interval).Should(Succeed())

		// Cleanup
		Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
		Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
		Expect(k8sClient.Delete(ctx, crq)).To(Succeed())
	})

	It("should not select a namespace with the exclude label, but select it when the label is removed", func() {
		excludedNsName := "test-ns-excluded-" + suffix

		ensureNamespaceDeleted(excludedNsName)
		ensureCRQDeleted(crqName)

		// This namespace matches the selector but has the exclude label
		excludedNs := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: excludedNsName,
				Labels: map[string]string{
					"quota": "test-" + suffix,
					// The default exclude label key
					"pac-quota-controller.powerapp.cloud/exclude": "true",
				},
			},
		}
		Expect(k8sClient.Create(ctx, excludedNs)).To(Succeed())

		crq := &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: crqName,
			},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"quota": "test-" + suffix},
				},
				Hard: quotav1alpha1.ResourceList{},
			},
		}
		Expect(k8sClient.Create(ctx, crq)).To(Succeed())

		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, excludedNs)
			_ = k8sClient.Delete(ctx, crq)
		})

		By("ensuring the excluded namespace is not in the CRQ status")
		Consistently(func() []string {
			return getCRQStatusNamespaces(crqName)
		}, "5s", "1s").ShouldNot(ContainElement(excludedNsName))

		By("removing the exclude label from the namespace")
		Eventually(func() error {
			err := k8sClient.Get(ctx, client.ObjectKey{Name: excludedNsName}, excludedNs)
			if err != nil {
				return err
			}
			delete(excludedNs.Labels, "pac-quota-controller.powerapp.cloud/exclude")
			return k8sClient.Update(ctx, excludedNs)
		}, "10s", "1s").Should(Succeed())

		By("waiting for the namespace to appear in CRQ status after label removal")
		Eventually(func() []string {
			return getCRQStatusNamespaces(crqName)
		}, "10s", "1s").Should(ContainElement(excludedNsName))
	})

	It("should remove a namespace from status when the exclude label is added", func() {
		matchingNsName := "test-ns-to-be-excluded-" + suffix
		ensureNamespaceDeleted(matchingNsName)
		ensureCRQDeleted(crqName)

		matchingNs := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   matchingNsName,
				Labels: map[string]string{"quota": "test-" + suffix},
			},
		}
		Expect(k8sClient.Create(ctx, matchingNs)).To(Succeed())

		crq := &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: crqName},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"quota": "test-" + suffix},
				},
				Hard: quotav1alpha1.ResourceList{},
			},
		}
		Expect(k8sClient.Create(ctx, crq)).To(Succeed())

		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, crq)
			_ = k8sClient.Delete(ctx, matchingNs)
		})

		By("waiting for the namespace to appear in CRQ status")
		Eventually(func() []string {
			return getCRQStatusNamespaces(crqName)
		}, "10s", "1s").Should(ContainElement(matchingNsName))

		By("adding the exclude label to the namespace")
		Eventually(func() error {
			err := k8sClient.Get(ctx, client.ObjectKey{Name: matchingNsName}, matchingNs)
			if err != nil {
				return err
			}
			matchingNs.Labels["pac-quota-controller.powerapp.cloud/exclude"] = "true"
			return k8sClient.Update(ctx, matchingNs)
		}, "10s", "1s").Should(Succeed())

		By("waiting for the namespace to be removed from CRQ status")
		Eventually(func() []string {
			return getCRQStatusNamespaces(crqName)
		}, "10s", "1s").ShouldNot(ContainElement(matchingNsName))
	})
})
