package services

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlclientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("ServiceResourceCalculator", func() {
	var (
		ctx    context.Context
		client ctrlclient.Client
		calc   *ServiceResourceCalculator
		logger *zap.Logger
	)

	BeforeEach(func() {
		var err error
		logger, err = zap.NewDevelopment()
		Expect(err).ToNot(HaveOccurred())
		ctx = context.Background()
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		client = ctrlclientfake.NewClientBuilder().WithScheme(scheme).WithObjects(
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc1", Namespace: "ns1"},
				Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP},
			},
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc2", Namespace: "ns1"},
				Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort},
			},
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc3", Namespace: "ns1"},
				Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
			},
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc4", Namespace: "ns1"},
				Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeExternalName},
			},
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc5", Namespace: "ns2"},
				Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP},
			},
		).Build()
		calc = NewServiceResourceCalculator(client, logger)
	})

	Describe("CalculateUsage", func() {
		It("returns correct count for ResourceServices in ns1", func() {
			q, err := calc.CalculateUsage(ctx, "ns1", usage.ResourceServices)
			Expect(err).ToNot(HaveOccurred())
			Expect(q.Value()).To(Equal(int64(4)))
		})

		It("returns correct count for ResourceServicesLoadBalancers in ns1", func() {
			q, err := calc.CalculateUsage(ctx, "ns1", usage.ResourceServicesLoadBalancers)
			Expect(err).ToNot(HaveOccurred())
			Expect(q.Value()).To(Equal(int64(1)))
		})

		It("returns correct count for ResourceServicesNodePorts in ns1", func() {
			q, err := calc.CalculateUsage(ctx, "ns1", usage.ResourceServicesNodePorts)
			Expect(err).ToNot(HaveOccurred())
			Expect(q.Value()).To(Equal(int64(1)))
		})

		It("returns zero for unsupported resource name", func() {
			q, err := calc.CalculateUsage(ctx, "ns1", corev1.ResourceName("unsupported"))
			Expect(err).ToNot(HaveOccurred())
			Expect(q.Value()).To(Equal(int64(0)))
		})

		It("returns correct count for ResourceServices in ns2 (single service)", func() {
			q, err := calc.CalculateUsage(ctx, "ns2", usage.ResourceServices)
			Expect(err).ToNot(HaveOccurred())
			Expect(q.Value()).To(Equal(int64(1)))
		})

		It("returns zero for other service types in ns2", func() {
			q, err := calc.CalculateUsage(ctx, "ns2", usage.ResourceServicesLoadBalancers)
			Expect(err).ToNot(HaveOccurred())
			Expect(q.Value()).To(Equal(int64(0)))
			q, err = calc.CalculateUsage(ctx, "ns2", usage.ResourceServicesNodePorts)
			Expect(err).ToNot(HaveOccurred())
			Expect(q.Value()).To(Equal(int64(0)))
		})
	})
})
