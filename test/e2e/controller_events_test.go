package e2e

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/test/utils"
)

var _ = Describe("Controller Events E2E", Ordered, func() {
	var (
		crqName        string
		controllerNS   string
		testNamespace1 string
		testNamespace2 string
		ctx            context.Context
	)

	BeforeAll(func() {
		ctx = context.Background()
		crqName = "test-events-crq"
		controllerNS = "pac-quota-controller-system" // Update this to match your actual controller namespace
		testNamespace1 = "test-events-ns1"
		testNamespace2 = "test-events-ns2"

		// Ensure test namespaces exist
		By("Creating test namespaces")
		ns1 := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace1,
				Labels: map[string]string{
					"team": "events-test",
				},
			},
		}
		Expect(k8sClient.Create(ctx, ns1)).To(Succeed())

		ns2 := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace2,
				Labels: map[string]string{
					"team": "events-test",
				},
			},
		}
		Expect(k8sClient.Create(ctx, ns2)).To(Succeed())
	})

	AfterAll(func() {
		By("Cleaning up test namespaces")
		Expect(k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace1}})).To(Succeed())
		Expect(k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace2}})).To(Succeed())
	})

	Describe("ClusterResourceQuota Event Generation", func() {
		var testCRQ *quotav1alpha1.ClusterResourceQuota

		BeforeEach(func() {
			testCRQ = &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name: crqName,
				},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"team": "events-test",
						},
					},
					Hard: quotav1alpha1.ResourceList{
						corev1.ResourceRequestsCPU:    resource.MustParse("2"),
						corev1.ResourceRequestsMemory: resource.MustParse("4Gi"),
						corev1.ResourcePods:           resource.MustParse("10"),
					},
				},
			}
		})

		AfterEach(func() {
			By("Cleaning up test CRQ")
			Expect(k8sClient.Delete(ctx, testCRQ)).To(Succeed())
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(testCRQ), testCRQ)
				return errors.IsNotFound(err)
			}, time.Minute, time.Second).Should(BeTrue(), "CRQ should be deleted")
		})

		It("should generate QuotaReconciled events during normal operation", func() {
			By("Creating the ClusterResourceQuota")
			Expect(k8sClient.Create(ctx, testCRQ)).To(Succeed())

			By("Waiting for CRQ to be reconciled")
			Eventually(func() bool {
				var crq quotav1alpha1.ClusterResourceQuota
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(testCRQ), &crq)
				if err != nil {
					return false
				}
				return len(crq.Status.Namespaces) > 0
			}, 30*time.Second, 2*time.Second).Should(BeTrue(), "CRQ should be reconciled")

			By("Waiting for QuotaReconciled event")
			Eventually(func() error {
				return utils.WaitForCRQEvent(ctx, clientSet, controllerNS, crqName, "QuotaReconciled", 10*time.Second)
			}, 30*time.Second, 2*time.Second).Should(Succeed(), "QuotaReconciled event should be recorded")

			By("Validating QuotaReconciled event content")
			events, err := utils.GetEventsByReason(ctx, clientSet, controllerNS, crqName, "QuotaReconciled")
			Expect(err).ToNot(HaveOccurred())
			Expect(events).ToNot(BeEmpty(), "Should have at least one QuotaReconciled event")

			// Validate the latest event content
			latestEvent := events[len(events)-1]
			Expect(utils.ValidateEventContent(latestEvent, "QuotaReconciled", "Normal", "reconciled successfully")).To(Succeed())
		})

		It("should generate NamespaceAdded and NamespaceRemoved events", func() {
			By("Creating the ClusterResourceQuota")
			Expect(k8sClient.Create(ctx, testCRQ)).To(Succeed())

			By("Waiting for NamespaceAdded events")
			Eventually(func() error {
				return utils.WaitForCRQEvent(ctx, clientSet, controllerNS, crqName, "NamespaceAdded", 10*time.Second)
			}, 30*time.Second, 2*time.Second).Should(Succeed(), "NamespaceAdded event should be recorded")

			By("Validating NamespaceAdded events")
			addedEvents, err := utils.GetEventsByReason(ctx, clientSet, controllerNS, crqName, "NamespaceAdded")
			Expect(err).ToNot(HaveOccurred())
			Expect(len(addedEvents)).To(BeNumerically(">=", 2), "Should have events for both test namespaces")

			// Check that both namespaces are mentioned in events
			foundNS1, foundNS2 := false, false
			for _, event := range addedEvents {
				if strings.Contains(event.Message, testNamespace1) {
					foundNS1 = true
				}
				if strings.Contains(event.Message, testNamespace2) {
					foundNS2 = true
				}
			}
			Expect(foundNS1).To(BeTrue(), "Should have NamespaceAdded event for "+testNamespace1)
			Expect(foundNS2).To(BeTrue(), "Should have NamespaceAdded event for "+testNamespace2)

			By("Removing a namespace from the CRQ scope")
			// Update the namespace label to remove it from scope
			var ns1 corev1.Namespace
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: testNamespace1}, &ns1)).To(Succeed())
			ns1.Labels["team"] = "different-team"
			Expect(k8sClient.Update(ctx, &ns1)).To(Succeed())

			By("Waiting for NamespaceRemoved event")
			Eventually(func() error {
				return utils.WaitForCRQEvent(ctx, clientSet, controllerNS, crqName, "NamespaceRemoved", 10*time.Second)
			}, 30*time.Second, 2*time.Second).Should(Succeed(), "NamespaceRemoved event should be recorded")

			By("Validating NamespaceRemoved event")
			removedEvents, err := utils.GetEventsByReason(ctx, clientSet, controllerNS, crqName, "NamespaceRemoved")
			Expect(err).ToNot(HaveOccurred())
			Expect(removedEvents).ToNot(BeEmpty(), "Should have NamespaceRemoved event")

			latestRemovedEvent := removedEvents[len(removedEvents)-1]
			Expect(utils.ValidateEventContent(latestRemovedEvent, "NamespaceRemoved", "Normal", testNamespace1)).To(Succeed())
		})

		It("should generate QuotaExceeded events when quota limits are violated", func() {
			By("Creating a CRQ with very low limits")
			lowLimitCRQ := testCRQ.DeepCopy()
			lowLimitCRQ.Name = crqName + "-low"
			lowLimitCRQ.Spec.Hard = quotav1alpha1.ResourceList{
				corev1.ResourceRequestsCPU: resource.MustParse("100m"), // Very low limit
				corev1.ResourcePods:        resource.MustParse("1"),    // Very low limit
			}

			Expect(k8sClient.Create(ctx, lowLimitCRQ)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, lowLimitCRQ)).To(Succeed())
			}()

			By("Creating pods that will exceed the quota")
			// Create multiple pods to exceed the pod limit
			for i := 0; i < 3; i++ {
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("test-pod-%d", i),
						Namespace: testNamespace1,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: "nginx:latest",
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU: resource.MustParse("50m"),
									},
								},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			}

			By("Waiting for QuotaExceeded event")
			Eventually(func() error {
				return utils.WaitForCRQEvent(ctx, clientSet, controllerNS, lowLimitCRQ.Name, "QuotaExceeded", 10*time.Second)
			}, 60*time.Second, 5*time.Second).Should(Succeed(), "QuotaExceeded event should be recorded")

			By("Validating QuotaExceeded event content")
			exceededEvents, err := utils.GetEventsByReason(ctx, clientSet, controllerNS, lowLimitCRQ.Name, "QuotaExceeded")
			Expect(err).ToNot(HaveOccurred())
			Expect(exceededEvents).ToNot(BeEmpty(), "Should have QuotaExceeded event")

			latestExceededEvent := exceededEvents[len(exceededEvents)-1]
			Expect(utils.ValidateEventContent(latestExceededEvent, "QuotaExceeded", "Warning", "exceeded quota")).To(Succeed())
		})

		It("should generate InvalidSelector events for malformed selectors", func() {
			By("Creating a CRQ with an invalid selector")
			invalidCRQ := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name: crqName + "-invalid",
				},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "invalid-key",
								Operator: "InvalidOperator", // This will cause an error
								Values:   []string{"value"},
							},
						},
					},
					Hard: quotav1alpha1.ResourceList{
						corev1.ResourcePods: resource.MustParse("10"),
					},
				},
			}

			Expect(k8sClient.Create(ctx, invalidCRQ)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, invalidCRQ)).To(Succeed())
			}()

			By("Waiting for InvalidSelector event")
			Eventually(func() error {
				return utils.WaitForCRQEvent(ctx, clientSet, controllerNS, invalidCRQ.Name, "InvalidSelector", 10*time.Second)
			}, 30*time.Second, 2*time.Second).Should(Succeed(), "InvalidSelector event should be recorded")

			By("Validating InvalidSelector event content")
			invalidEvents, err := utils.GetEventsByReason(ctx, clientSet, controllerNS, invalidCRQ.Name, "InvalidSelector")
			Expect(err).ToNot(HaveOccurred())
			Expect(invalidEvents).ToNot(BeEmpty(), "Should have InvalidSelector event")

			latestInvalidEvent := invalidEvents[len(invalidEvents)-1]
			Expect(utils.ValidateEventContent(
				latestInvalidEvent,
				"InvalidSelector",
				"Warning",
				"Invalid namespace selector",
			)).To(Succeed())
		})
	})

	Describe("Event Metadata Validation", func() {
		It("should include proper PAC-specific labels on events", func() {
			By("Creating a test CRQ")
			testCRQ := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name: crqName + "-metadata",
				},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"team": "events-test",
						},
					},
					Hard: quotav1alpha1.ResourceList{
						corev1.ResourcePods: resource.MustParse("10"),
					},
				},
			}

			Expect(k8sClient.Create(ctx, testCRQ)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, testCRQ)).To(Succeed())
			}()

			By("Waiting for any event to be generated")
			Eventually(func() error {
				events, err := utils.GetCRQEvents(ctx, clientSet, controllerNS, testCRQ.Name)
				if err != nil {
					return err
				}
				if len(events) == 0 {
					return fmt.Errorf("no events found yet")
				}
				return nil
			}, 30*time.Second, 2*time.Second).Should(Succeed(), "At least one event should be generated")

			By("Validating event metadata and labels")
			events, err := utils.GetCRQEvents(ctx, clientSet, controllerNS, testCRQ.Name)
			Expect(err).ToNot(HaveOccurred())
			Expect(events).ToNot(BeEmpty(), "Should have at least one event")

			// Check the first event for proper metadata
			event := events[0]
			Expect(event.InvolvedObject.Kind).To(Equal("ClusterResourceQuota"))
			Expect(event.InvolvedObject.Name).To(Equal(testCRQ.Name))
			Expect(event.Source.Component).To(Equal("pac-quota-controller"))

			// Check for PAC-specific annotations (if any are set)
			if event.Annotations != nil {
				// Validate any PAC-specific annotations that should be present
				if source, exists := event.Annotations["quota.pac.io/event-source"]; exists {
					Expect(source).To(Equal("controller"))
				}
				if crqName, exists := event.Annotations["quota.pac.io/crq-name"]; exists {
					Expect(crqName).To(Equal(testCRQ.Name))
				}
			}
		})
	})
})
