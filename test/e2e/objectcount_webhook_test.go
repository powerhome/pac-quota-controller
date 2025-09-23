package e2e

import (
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	testutils "github.com/powerhome/pac-quota-controller/test/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("ClusterResourceQuota Object Count Webhook E2E", func() {
	var (
		testNamespace string
		testCRQName   string
		testSuffix    string
		ns            *corev1.Namespace
		crq           *quotav1alpha1.ClusterResourceQuota
	)

	BeforeEach(func() {
		testSuffix = testutils.GenerateTestSuffix()
		testNamespace = testutils.GenerateResourceName("objectcount-ns-" + testSuffix)
		testCRQName = testutils.GenerateResourceName("objectcount-crq-" + testSuffix)
		ns, _ = testutils.CreateNamespace(ctx, k8sClient, testNamespace, map[string]string{"objectcount-test": "test-label-" + testSuffix})
		crq, _ = testutils.CreateClusterResourceQuota(ctx, k8sClient, testCRQName, &metav1.LabelSelector{
			MatchLabels: map[string]string{"objectcount-test": "test-label-" + testSuffix},
		}, quotav1alpha1.ResourceList{
			"configmaps":                           resource.MustParse("2"),
			"secrets":                              resource.MustParse("1"),
			"replicationcontrollers":               resource.MustParse("1"),
			"deployments.apps":                     resource.MustParse("1"),
			"statefulsets.apps":                    resource.MustParse("1"),
			"daemonsets.apps":                      resource.MustParse("1"),
			"jobs.batch":                           resource.MustParse("1"),
			"cronjobs.batch":                       resource.MustParse("1"),
			"horizontalpodautoscalers.autoscaling": resource.MustParse("1"),
			"ingresses.networking.k8s.io":          resource.MustParse("1"),
		})
	})

	AfterEach(func() {
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, ns) })
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, crq) })
	})

	Context("Object Count Quota", func() {
		It("should allow creation under quota for configmaps", func() {
			cmName := testutils.GenerateResourceName("cm-under-quota-" + testSuffix)
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cmName,
					Namespace: testNamespace,
				},
				Data: map[string]string{"key": "value"},
			}
			err := k8sClient.Create(ctx, cm)
			Expect(err).ToNot(HaveOccurred(), "ConfigMap creation under quota should be allowed")
		})
		It("should allow creation at quota for configmaps", func() {
			// Create two configmaps (quota is 2)
			for i := range 2 {
				cmName := testutils.GenerateResourceName("cm-at-quota-" + testSuffix + "-" + strconv.Itoa(i))
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      cmName,
						Namespace: testNamespace,
					},
					Data: map[string]string{"key": "value"},
				}
				err := k8sClient.Create(ctx, cm)
				Expect(err).ToNot(HaveOccurred(), "ConfigMap creation at quota should be allowed")
			}
		})
		It("should deny creation over quota for configmaps", func() {
			// Create up to quota
			for i := range 2 {
				cmName := testutils.GenerateResourceName("cm-over-quota-" + testSuffix + "-" + strconv.Itoa(i))
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      cmName,
						Namespace: testNamespace,
					},
					Data: map[string]string{"key": "value"},
				}
				err := k8sClient.Create(ctx, cm)
				Expect(err).ToNot(HaveOccurred(), "ConfigMap creation up to quota should be allowed")
			}
			// Attempt to create one more, should fail
			cmName := testutils.GenerateResourceName("cm-over-quota-" + testSuffix + "-extra")
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cmName,
					Namespace: testNamespace,
				},
				Data: map[string]string{"key": "value"},
			}
			err := k8sClient.Create(ctx, cm)
			Expect(err).To(HaveOccurred(), "ConfigMap creation over quota should be denied")
		})
		It("should allow creation under quota for secrets", func() {
			secretName := testutils.GenerateResourceName("secret-under-quota-" + testSuffix)
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: testNamespace,
				},
				Data: map[string][]byte{"key": []byte("value")},
			}
			err := k8sClient.Create(ctx, secret)
			Expect(err).ToNot(HaveOccurred(), "Secret creation under quota should be allowed")
		})
		It("should deny creation over quota for secrets", func() {
			secretName := testutils.GenerateResourceName("secret-over-quota-" + testSuffix)
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: testNamespace,
				},
				Data: map[string][]byte{"key": []byte("value")},
			}
			err := k8sClient.Create(ctx, secret)
			Expect(err).ToNot(HaveOccurred(), "Secret creation up to quota should be allowed")
			// Attempt to create one more, should fail
			secretNameExtra := testutils.GenerateResourceName("secret-over-quota-" + testSuffix + "-extra")
			secretExtra := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretNameExtra,
					Namespace: testNamespace,
				},
				Data: map[string][]byte{"key": []byte("value")},
			}
			err = k8sClient.Create(ctx, secretExtra)
			Expect(err).To(HaveOccurred(), "Secret creation over quota should be denied")
		})
		It("should allow creation under quota for replicationcontrollers", func() {
			rcName := testutils.GenerateResourceName("rc-under-quota-" + testSuffix)
			rc := testutils.NewReplicationController(rcName, testNamespace, 1)
			err := k8sClient.Create(ctx, rc)
			Expect(err).ToNot(HaveOccurred(), "ReplicationController creation under quota should be allowed")
		})
		It("should deny creation over quota for replicationcontrollers", func() {
			rcName := testutils.GenerateResourceName("rc-over-quota-" + testSuffix)
			rc := testutils.NewReplicationController(rcName, testNamespace, 1)
			err := k8sClient.Create(ctx, rc)
			Expect(err).ToNot(HaveOccurred(), "ReplicationController creation up to quota should be allowed")
			rcNameExtra := testutils.GenerateResourceName("rc-over-quota-" + testSuffix + "-extra")
			rcExtra := testutils.NewReplicationController(rcNameExtra, testNamespace, 1)
			err = k8sClient.Create(ctx, rcExtra)
			Expect(err).To(HaveOccurred(), "ReplicationController creation over quota should be denied")
		})
		It("should allow creation under quota for deployments.apps", func() {
			depName := testutils.GenerateResourceName("dep-under-quota-" + testSuffix)
			dep := testutils.NewDeployment(depName, testNamespace, 1)
			err := k8sClient.Create(ctx, dep)
			Expect(err).ToNot(HaveOccurred(), "Deployment creation under quota should be allowed")
		})
		It("should deny creation over quota for deployments.apps", func() {
			depName := testutils.GenerateResourceName("dep-over-quota-" + testSuffix)
			dep := testutils.NewDeployment(depName, testNamespace, 1)
			err := k8sClient.Create(ctx, dep)
			Expect(err).ToNot(HaveOccurred(), "Deployment creation up to quota should be allowed")
			depNameExtra := testutils.GenerateResourceName("dep-over-quota-" + testSuffix + "-extra")
			depExtra := testutils.NewDeployment(depNameExtra, testNamespace, 1)
			err = k8sClient.Create(ctx, depExtra)
			Expect(err).To(HaveOccurred(), "Deployment creation over quota should be denied")
		})
		It("should allow creation under quota for statefulsets.apps", func() {
			ssName := testutils.GenerateResourceName("ss-under-quota-" + testSuffix)
			ss := testutils.NewStatefulSet(ssName, testNamespace, 1)
			err := k8sClient.Create(ctx, ss)
			Expect(err).ToNot(HaveOccurred(), "StatefulSet creation under quota should be allowed")
		})
		It("should deny creation over quota for statefulsets.apps", func() {
			ssName := testutils.GenerateResourceName("ss-over-quota-" + testSuffix)
			ss := testutils.NewStatefulSet(ssName, testNamespace, 1)
			err := k8sClient.Create(ctx, ss)
			Expect(err).ToNot(HaveOccurred(), "StatefulSet creation up to quota should be allowed")
			ssNameExtra := testutils.GenerateResourceName("ss-over-quota-" + testSuffix + "-extra")
			ssExtra := testutils.NewStatefulSet(ssNameExtra, testNamespace, 1)
			err = k8sClient.Create(ctx, ssExtra)
			Expect(err).To(HaveOccurred(), "StatefulSet creation over quota should be denied")
		})
		It("should allow creation under quota for daemonsets.apps", func() {
			dsName := testutils.GenerateResourceName("ds-under-quota-" + testSuffix)
			ds := testutils.NewDaemonSet(dsName, testNamespace)
			err := k8sClient.Create(ctx, ds)
			Expect(err).ToNot(HaveOccurred(), "DaemonSet creation under quota should be allowed")
		})
		It("should deny creation over quota for daemonsets.apps", func() {
			dsName := testutils.GenerateResourceName("ds-over-quota-" + testSuffix)
			ds := testutils.NewDaemonSet(dsName, testNamespace)
			err := k8sClient.Create(ctx, ds)
			Expect(err).ToNot(HaveOccurred(), "DaemonSet creation up to quota should be allowed")
			dsNameExtra := testutils.GenerateResourceName("ds-over-quota-" + testSuffix + "-extra")
			dsExtra := testutils.NewDaemonSet(dsNameExtra, testNamespace)
			err = k8sClient.Create(ctx, dsExtra)
			Expect(err).To(HaveOccurred(), "DaemonSet creation over quota should be denied")
		})
		It("should allow creation under quota for jobs.batch", func() {
			jobName := testutils.GenerateResourceName("job-under-quota-" + testSuffix)
			command := []string{"echo", "hello"}
			requests := corev1.ResourceList{}
			limits := corev1.ResourceList{}
			_, err := testutils.CreateJob(ctx, k8sClient, testNamespace, jobName, command, requests, limits)
			Expect(err).ToNot(HaveOccurred(), "Job creation under quota should be allowed")
		})
		It("should deny creation over quota for jobs.batch", func() {
			jobName := testutils.GenerateResourceName("job-over-quota-" + testSuffix)
			command := []string{"echo", "hello"}
			requests := corev1.ResourceList{}
			limits := corev1.ResourceList{}
			_, err := testutils.CreateJob(ctx, k8sClient, testNamespace, jobName, command, requests, limits)
			Expect(err).ToNot(HaveOccurred(), "Job creation up to quota should be allowed")
			jobNameExtra := testutils.GenerateResourceName("job-over-quota-" + testSuffix + "-extra")
			_, err = testutils.CreateJob(ctx, k8sClient, testNamespace, jobNameExtra, command, requests, limits)
			Expect(err).To(HaveOccurred(), "Job creation over quota should be denied")
		})
		It("should allow creation under quota for cronjobs.batch", func() {
			cjName := testutils.GenerateResourceName("cj-under-quota-" + testSuffix)
			cj := testutils.NewCronJob(cjName, testNamespace)
			err := k8sClient.Create(ctx, cj)
			Expect(err).ToNot(HaveOccurred(), "CronJob creation under quota should be allowed")
		})
		It("should deny creation over quota for cronjobs.batch", func() {
			cjName := testutils.GenerateResourceName("cj-over-quota-" + testSuffix)
			cj := testutils.NewCronJob(cjName, testNamespace)
			err := k8sClient.Create(ctx, cj)
			Expect(err).ToNot(HaveOccurred(), "CronJob creation up to quota should be allowed")
			cjNameExtra := testutils.GenerateResourceName("cj-over-quota-" + testSuffix + "-extra")
			cjExtra := testutils.NewCronJob(cjNameExtra, testNamespace)
			err = k8sClient.Create(ctx, cjExtra)
			Expect(err).To(HaveOccurred(), "CronJob creation over quota should be denied")
		})
		It("should allow creation under quota for hpas.autoscaling", func() {
			hpaName := testutils.GenerateResourceName("hpa-under-quota-" + testSuffix)
			hpa := testutils.NewHPA(hpaName, testNamespace)
			err := k8sClient.Create(ctx, hpa)
			Expect(err).ToNot(HaveOccurred(), "HPA creation under quota should be allowed")
		})
		It("should deny creation over quota for hpas.autoscaling", func() {
			hpaName := testutils.GenerateResourceName("hpa-over-quota-" + testSuffix)
			hpa := testutils.NewHPA(hpaName, testNamespace)
			err := k8sClient.Create(ctx, hpa)
			Expect(err).ToNot(HaveOccurred(), "HPA creation up to quota should be allowed")
			hpaNameExtra := testutils.GenerateResourceName("hpa-over-quota-" + testSuffix + "-extra")
			hpaExtra := testutils.NewHPA(hpaNameExtra, testNamespace)
			err = k8sClient.Create(ctx, hpaExtra)
			Expect(err).To(HaveOccurred(), "HPA creation over quota should be denied")
		})
		It("should allow creation under quota for ingresses.networking.k8s.io", func() {
			ingName := testutils.GenerateResourceName("ing-under-quota-" + testSuffix)
			ing := testutils.NewIngress(ingName, testNamespace)
			err := k8sClient.Create(ctx, ing)
			Expect(err).ToNot(HaveOccurred(), "Ingress creation under quota should be allowed")
		})
		It("should deny creation over quota for ingresses.networking.k8s.io", func() {
			ingName := testutils.GenerateResourceName("ing-over-quota-" + testSuffix)
			ing := testutils.NewIngress(ingName, testNamespace)
			err := k8sClient.Create(ctx, ing)
			Expect(err).ToNot(HaveOccurred(), "Ingress creation up to quota should be allowed")
			ingNameExtra := testutils.GenerateResourceName("ing-over-quota-" + testSuffix + "-extra")
			ingExtra := testutils.NewIngress(ingNameExtra, testNamespace)
			err = k8sClient.Create(ctx, ingExtra)
			Expect(err).To(HaveOccurred(), "Ingress creation over quota should be denied")
		})
		It("should allow mixed resources under quota", func() {
			// Create one of each resource, all under quota
			resources := []struct {
				name string
				obj  client.Object
			}{
				{"cm-mixed-under-", &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: testutils.GenerateResourceName("cm-mixed-under-" + testSuffix), Namespace: testNamespace}}},
				{"secret-mixed-under-", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: testutils.GenerateResourceName("secret-mixed-under-" + testSuffix), Namespace: testNamespace}}},
				{"rc-mixed-under-", testutils.NewReplicationController(testutils.GenerateResourceName("rc-mixed-under-"+testSuffix), testNamespace, 1)},
				{"dep-mixed-under-", testutils.NewDeployment(testutils.GenerateResourceName("dep-mixed-under-"+testSuffix), testNamespace, 1)},
				{"ss-mixed-under-", testutils.NewStatefulSet(testutils.GenerateResourceName("ss-mixed-under-"+testSuffix), testNamespace, 1)},
				{"ds-mixed-under-", testutils.NewDaemonSet(testutils.GenerateResourceName("ds-mixed-under-"+testSuffix), testNamespace)},
				{"cj-mixed-under-", testutils.NewCronJob(testutils.GenerateResourceName("cj-mixed-under-"+testSuffix), testNamespace)},
				{"hpa-mixed-under-", testutils.NewHPA(testutils.GenerateResourceName("hpa-mixed-under-"+testSuffix), testNamespace)},
				{"ing-mixed-under-", testutils.NewIngress(testutils.GenerateResourceName("ing-mixed-under-"+testSuffix), testNamespace)},
			}
			for _, r := range resources {
				err := k8sClient.Create(ctx, r.obj)
				Expect(err).ToNot(HaveOccurred(), r.name+" creation under quota should be allowed")
			}
			// Job: use CreateJob helper
			jobName := testutils.GenerateResourceName("job-mixed-under-" + testSuffix)
			command := []string{"echo", "hello"}
			requests := corev1.ResourceList{}
			limits := corev1.ResourceList{}
			_, err := testutils.CreateJob(ctx, k8sClient, testNamespace, jobName, command, requests, limits)
			Expect(err).ToNot(HaveOccurred(), "Job creation under quota should be allowed")
		})
		It("should deny mixed resources over quota", func() {
			// Fill up quota for configmaps and secrets, then try to create one more of each
			for i := range 2 {
				cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: testutils.GenerateResourceName("cm-mixed-over-" + testSuffix + "-" + strconv.Itoa(i)), Namespace: testNamespace}}
				err := k8sClient.Create(ctx, cm)
				Expect(err).ToNot(HaveOccurred(), "ConfigMap creation up to quota should be allowed")
			}
			cmExtra := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: testutils.GenerateResourceName("cm-mixed-over-" + testSuffix + "-extra"), Namespace: testNamespace}}
			err := k8sClient.Create(ctx, cmExtra)
			Expect(err).To(HaveOccurred(), "ConfigMap creation over quota should be denied")
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: testutils.GenerateResourceName("secret-mixed-over-" + testSuffix), Namespace: testNamespace}}
			err = k8sClient.Create(ctx, secret)
			Expect(err).ToNot(HaveOccurred(), "Secret creation up to quota should be allowed")
			secretExtra := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: testutils.GenerateResourceName("secret-mixed-over-" + testSuffix + "-extra"), Namespace: testNamespace}}
			err = k8sClient.Create(ctx, secretExtra)
			Expect(err).To(HaveOccurred(), "Secret creation over quota should be denied")
		})
		It("should deny creation with missing namespace", func() {
			cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: testutils.GenerateResourceName("cm-missing-ns-" + testSuffix)}}
			err := k8sClient.Create(ctx, cm)
			Expect(err).To(HaveOccurred(), "ConfigMap creation with missing namespace should be denied")
		})
	})
})
