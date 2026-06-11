package services

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("CalculateUsageFromServices", func() {
	makeServices := func() []corev1.Service {
		return []corev1.Service{
			{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP}},
			{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort}},
			{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer}},
			{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeExternalName}},
		}
	}

	It("counts every service for ResourceServices", func() {
		q := CalculateUsageFromServices(makeServices(), usage.ResourceServices)
		Expect(q.Value()).To(Equal(int64(4)))
	})

	It("counts only LoadBalancer services for ResourceServicesLoadBalancers", func() {
		q := CalculateUsageFromServices(makeServices(), usage.ResourceServicesLoadBalancers)
		Expect(q.Value()).To(Equal(int64(1)))
	})

	It("counts only NodePort services for ResourceServicesNodePorts", func() {
		q := CalculateUsageFromServices(makeServices(), usage.ResourceServicesNodePorts)
		Expect(q.Value()).To(Equal(int64(1)))
	})

	It("returns zero for unsupported resource names", func() {
		q := CalculateUsageFromServices(makeServices(), corev1.ResourceName("unsupported"))
		Expect(q.Value()).To(Equal(int64(0)))
	})

	It("returns zero on an empty slice", func() {
		q := CalculateUsageFromServices(nil, usage.ResourceServices)
		Expect(q.Value()).To(Equal(int64(0)))
	})
})
