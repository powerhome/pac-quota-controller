package e2e

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	testutils "github.com/powerhome/pac-quota-controller/test/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Service Quota Webhook", func() {
	var (
		testNamespace string
		testCRQName   string
		testSuffix    string
		ns            *corev1.Namespace
		crq           *quotav1alpha1.ClusterResourceQuota
	)

	BeforeEach(func() {
		testSuffix = testutils.GenerateTestSuffix()
		testNamespace = testutils.GenerateResourceName("service-webhook-ns-" + testSuffix)
		testCRQName = testutils.GenerateResourceName("service-webhook-crq-" + testSuffix)

		var err error
		// Use a unique label key and value for each test to avoid selector collisions
		uniqueLabelKey := "service-webhook-test-" + testSuffix
		uniqueLabelValue := "test-label-" + testSuffix
		ns, err = testutils.CreateNamespace(ctx, k8sClient, testNamespace, map[string]string{
			uniqueLabelKey: uniqueLabelValue,
		})
		Expect(err).NotTo(HaveOccurred())

		crq, err = testutils.CreateClusterResourceQuota(ctx, k8sClient, testCRQName, &metav1.LabelSelector{
			MatchLabels: map[string]string{
				uniqueLabelKey: uniqueLabelValue,
			},
		}, quotav1alpha1.ResourceList{
			corev1.ResourceName("services"):               resource.MustParse("2"),
			corev1.ResourceName("services.loadbalancers"): resource.MustParse("1"),
			corev1.ResourceName("services.nodeports"):     resource.MustParse("1"),
		})
		Expect(err).NotTo(HaveOccurred())

		// Wait for CRQ status to include the test namespace before proceeding
		By("Waiting for CRQ status to include the test namespace")
		err = testutils.WaitForCRQStatus(ctx, k8sClient, testCRQName, []string{testNamespace}, 10*time.Second, 1*time.Second)
		Expect(err).NotTo(
			HaveOccurred(),
			"CRQ status did not include the test namespace in time; check CRQ selector and namespace labels",
		)
	})

	AfterEach(func() {
		// Clean up all services in the test namespace except the default 'kubernetes' service
		svcList := &corev1.ServiceList{}
		err := k8sClient.List(ctx, svcList, ctrlclient.InNamespace(testNamespace))
		Expect(err).NotTo(HaveOccurred())
		for _, svc := range svcList.Items {
			if svc.Name == "kubernetes" {
				continue
			}
			_ = k8sClient.Delete(ctx, &svc)
		}
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, ns)
			_ = k8sClient.Delete(ctx, crq)
		})
	})

	Context("Service Creation Webhook", func() {
		// ClusterIP services are not counted for quota, so only test creation is allowed (no denial test)
		It("should allow ClusterIP service creation (not counted for quota)", func() {
			service := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service-1-" + testSuffix,
					Namespace: testNamespace,
				},
				Spec: corev1.ServiceSpec{
					Type:  corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{{Port: 80}},
				},
			}
			err := k8sClient.Create(ctx, service)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should allow NodePort service creation within quota limits", func() {
			service := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-nodeport-" + testSuffix,
					Namespace: testNamespace,
				},
				Spec: corev1.ServiceSpec{
					Type:  corev1.ServiceTypeNodePort,
					Ports: []corev1.ServicePort{{Port: 30000, NodePort: 30001}},
				},
			}
			err := k8sClient.Create(ctx, service)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should deny NodePort service creation when exceeding nodeport quota", func() {
			// Create one NodePort service to reach the nodeport quota
			service := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-nodeport-1-" + testSuffix,
					Namespace: testNamespace,
				},
				Spec: corev1.ServiceSpec{
					Type:  corev1.ServiceTypeNodePort,
					Ports: []corev1.ServicePort{{Port: 30000, NodePort: 30001}},
				},
			}
			Expect(k8sClient.Create(ctx, service)).To(Succeed())
			// Second NodePort service should be denied
			service2 := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-nodeport-2-" + testSuffix,
					Namespace: testNamespace,
				},
				Spec: corev1.ServiceSpec{
					Type:  corev1.ServiceTypeNodePort,
					Ports: []corev1.ServicePort{{Port: 30002, NodePort: 30003}},
				},
			}
			err := k8sClient.Create(ctx, service2)
			Expect(err).To(HaveOccurred())
		})

		It("should allow LoadBalancer service creation within quota limits", func() {
			service := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-lb-" + testSuffix,
					Namespace: testNamespace,
				},
				Spec: corev1.ServiceSpec{
					Type:  corev1.ServiceTypeLoadBalancer,
					Ports: []corev1.ServicePort{{Port: 80}},
				},
			}
			err := k8sClient.Create(ctx, service)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should deny LoadBalancer service creation when exceeding loadbalancer quota", func() {
			// Create one LoadBalancer service to reach the quota
			service := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-lb-1-" + testSuffix,
					Namespace: testNamespace,
				},
				Spec: corev1.ServiceSpec{
					Type:  corev1.ServiceTypeLoadBalancer,
					Ports: []corev1.ServicePort{{Port: 80}},
				},
			}
			Expect(k8sClient.Create(ctx, service)).To(Succeed())
			// Second LoadBalancer service should be denied
			service2 := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-lb-2-" + testSuffix,
					Namespace: testNamespace,
				},
				Spec: corev1.ServiceSpec{
					Type:  corev1.ServiceTypeLoadBalancer,
					Ports: []corev1.ServicePort{{Port: 80}},
				},
			}
			err := k8sClient.Create(ctx, service2)
			Expect(err).To(HaveOccurred())
		})

		// ExternalName services are not counted for quota, so only test creation is allowed (no denial test)
		It("should allow ExternalName service creation (not counted for quota)", func() {
			service := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-externalname-" + testSuffix,
					Namespace: testNamespace,
				},
				Spec: corev1.ServiceSpec{
					Type:         corev1.ServiceTypeExternalName,
					ExternalName: "example.com",
				},
			}
			err := k8sClient.Create(ctx, service)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should allow service creation in namespace not matching CRQ selector", func() {
			// Create a namespace with a label that does not match the CRQ selector
			otherNsName := testutils.GenerateResourceName("service-webhook-other-ns-" + testSuffix)
			// Use a unique label key for the non-matching namespace as well
			// Use a unique label key/value for the non-matching namespace to ensure it does not match the CRQ selector
			otherNs, err := testutils.CreateNamespace(ctx, k8sClient, otherNsName, map[string]string{
				"service-webhook-other-test-" + testSuffix: "other-label-value-" + testSuffix,
			})
			Expect(err).NotTo(HaveOccurred())
			service := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service-other-" + testSuffix,
					Namespace: otherNsName,
				},
				Spec: corev1.ServiceSpec{
					Type:  corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{{Port: 80}},
				},
			}
			err = k8sClient.Create(ctx, service)
			Expect(err).NotTo(HaveOccurred())
			_ = k8sClient.Delete(ctx, otherNs)
		})

		It("should allow service creation when no CRQ exists", func() {
			// Create a namespace with a unique label
			noCrqNsName := testutils.GenerateResourceName("service-webhook-nocrq-ns-" + testSuffix)
			// Use a unique label key/value for the no-CRQ namespace to ensure it does not match any CRQ selector
			noCrqNs, err := testutils.CreateNamespace(ctx, k8sClient, noCrqNsName, map[string]string{
				"service-webhook-nocrq-test-" + testSuffix: "nocrq-label-value-" + testSuffix,
			})
			Expect(err).NotTo(HaveOccurred())
			service := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service-nocrq-" + testSuffix,
					Namespace: noCrqNsName,
				},
				Spec: corev1.ServiceSpec{
					Type:  corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{{Port: 80}},
				},
			}
			err = k8sClient.Create(ctx, service)
			Expect(err).NotTo(HaveOccurred())
			_ = k8sClient.Delete(ctx, noCrqNs)
		})

		It("should deny updating a service to a type that exceeds subtype quota", func() {
			// Create a ClusterIP service (within quota)
			service := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-update-service-" + testSuffix,
					Namespace: testNamespace,
				},
				Spec: corev1.ServiceSpec{
					Type:  corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{{Port: 80}},
				},
			}
			Expect(k8sClient.Create(ctx, service)).To(Succeed())
			// Fill the loadbalancer quota
			lbService := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-update-lb-" + testSuffix,
					Namespace: testNamespace,
				},
				Spec: corev1.ServiceSpec{
					Type:  corev1.ServiceTypeLoadBalancer,
					Ports: []corev1.ServicePort{{Port: 80}},
				},
			}
			Expect(k8sClient.Create(ctx, lbService)).To(Succeed())
			// Try to update the ClusterIP service to LoadBalancer (should fail)
			var fetched corev1.Service
			Expect(
				k8sClient.Get(
					ctx,
					ctrlclient.ObjectKey{
						Name:      service.Name,
						Namespace: testNamespace,
					},
					&fetched,
				),
			).To(Succeed())
			fetched.Spec.Type = corev1.ServiceTypeLoadBalancer
			err := k8sClient.Update(ctx, &fetched)
			Expect(err).To(HaveOccurred())
		})
	})
})
