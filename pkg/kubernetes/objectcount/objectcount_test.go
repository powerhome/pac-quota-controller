package objectcount

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

var _ = Describe("ObjectCountCalculator", func() {
	var (
		ctx    context.Context
		scheme *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		_ = corev1.AddToScheme(scheme)
		_ = appsv1.AddToScheme(scheme)
		_ = batchv1.AddToScheme(scheme)
		_ = autoscalingv1.AddToScheme(scheme)
		_ = networkingv1.AddToScheme(scheme)
	})

	DescribeTable("CalculateTotalUsage for all supported resources",
		func(resourceName string, object runtime.Object, expected int64) {
			ns := "ns1"
			rn := corev1.ResourceName(resourceName)
			client := fake.NewSimpleClientset(object)
			calc := NewObjectCountCalculator(client)
			usage, err := calc.CalculateTotalUsage(ctx, rn, ns)
			Expect(err).ToNot(HaveOccurred())
			q := usage[rn]
			Expect(q.Value()).To(Equal(expected))
		},
		Entry("configmaps", "configmaps", &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm1", Namespace: "ns1"}}, int64(1)),
		Entry("secrets", "secrets", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "ns1"}}, int64(1)),
		Entry("replicationcontrollers", "replicationcontrollers", &corev1.ReplicationController{ObjectMeta: metav1.ObjectMeta{Name: "rc1", Namespace: "ns1"}}, int64(1)),
		Entry("deployments.apps", "deployments.apps", &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "dep1", Namespace: "ns1"}}, int64(1)),
		Entry("statefulsets.apps", "statefulsets.apps", &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "st1", Namespace: "ns1"}}, int64(1)),
		Entry("daemonsets.apps", "daemonsets.apps", &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "ds1", Namespace: "ns1"}}, int64(1)),
		Entry("jobs.batch", "jobs.batch", &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "job1", Namespace: "ns1"}}, int64(1)),
		Entry("cronjobs.batch", "cronjobs.batch", &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "cj1", Namespace: "ns1"}}, int64(1)),
		Entry("horizontalpodautoscalers.autoscaling", "horizontalpodautoscalers.autoscaling", &autoscalingv1.HorizontalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: "hpa1", Namespace: "ns1"}}, int64(1)),
		Entry("ingresses.networking.k8s.io", "ingresses.networking.k8s.io", &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "ing1", Namespace: "ns1"}}, int64(1)),
	)

	It("should count multiple resources of the same type", func() {
		ns := "ns1"
		rn := corev1.ResourceName("configmaps")
		cm1 := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm1", Namespace: ns}}
		cm2 := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm2", Namespace: ns}}
		client := fake.NewSimpleClientset(cm1, cm2)
		calc := NewObjectCountCalculator(client)
		usage, err := calc.CalculateTotalUsage(ctx, rn, ns)
		Expect(err).ToNot(HaveOccurred())
		q := usage[rn]
		Expect(q.Value()).To(Equal(int64(2)))
	})

	It("should count multiple resource types in the same namespace", func() {
		ns := "ns1"
		rnCM := corev1.ResourceName("configmaps")
		rnSecret := corev1.ResourceName("secrets")
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm1", Namespace: ns}}
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: ns}}
		client := fake.NewSimpleClientset(cm, secret)
		calcCM := NewObjectCountCalculator(client)
		calcSecret := NewObjectCountCalculator(client)
		usageCM, err := calcCM.CalculateTotalUsage(ctx, rnCM, ns)
		Expect(err).ToNot(HaveOccurred())
		usageSecret, err := calcSecret.CalculateTotalUsage(ctx, rnSecret, ns)
		Expect(err).ToNot(HaveOccurred())
		qCM := usageCM[rnCM]
		qSecret := usageSecret[rnSecret]
		Expect((&qCM).Value()).To(Equal(int64(1)))
		Expect((&qSecret).Value()).To(Equal(int64(1)))
	})

	It("should return zero for no resources present", func() {
		ns := "ns1"
		rn := corev1.ResourceName("configmaps")
		client := fake.NewSimpleClientset()
		calc := NewObjectCountCalculator(client)
		usage, err := calc.CalculateTotalUsage(ctx, rn, ns)
		Expect(err).ToNot(HaveOccurred())
		q := usage[rn]
		Expect(q.Value()).To(Equal(int64(0)))
	})

	It("should return zero for inexistent resource type", func() {
		ns := "ns1"
		rn := corev1.ResourceName("nonexistent")
		client := fake.NewSimpleClientset()
		calc := NewObjectCountCalculator(client)
		usage, err := calc.CalculateTotalUsage(ctx, rn, ns)
		Expect(err).ToNot(HaveOccurred())
		q := usage[rn]
		Expect(q.Value()).To(Equal(int64(0)))
	})
})
