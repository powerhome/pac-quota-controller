package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/powerhome/pac-quota-controller/api/v1alpha1"
	testutils "github.com/powerhome/pac-quota-controller/test/utils"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8sconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	k8sClient client.Client
	clientSet *kubernetes.Clientset
	ctx       context.Context
	e2eConfig testutils.E2EConfig
)

// TestE2E runs the e2e suite. The suite owns the cluster lifecycle (tune via testutils.E2EConfig).
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting pac-quota-controller integration test suite\n")
	RunSpecs(t, "e2e suite")
}

// First func provisions the environment once; second runs on every process.
var _ = SynchronizedBeforeSuite(func() []byte {
	e2eConfig = testutils.LoadE2EConfig()
	setupCtx := context.Background()

	By("provisioning the e2e environment")
	Expect(e2eConfig.Provision(setupCtx)).To(Succeed(), "Failed to provision e2e environment")

	By("scrubbing leftovers from any previous run")
	Expect(testutils.Scrub(setupCtx, newK8sClient())).To(Succeed(), "Failed to scrub cluster")

	By("waiting for the controller to start reconciling")
	Expect(testutils.WaitForControllerReconciling(setupCtx, newK8sClient(), 180*time.Second, time.Second)).
		To(Succeed(), "controller did not begin reconciling in time")
	return nil
}, func(_ []byte) {
	e2eConfig = testutils.LoadE2EConfig()
	ctx = context.Background()

	log.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	cfg, err := k8sconfig.GetConfig()
	Expect(err).NotTo(HaveOccurred(), "Failed to get kubeconfig")

	k8sClient = newK8sClient()
	clientSet, err = kubernetes.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred(), "Failed to create Kubernetes clientset")
})

// Second func tears the environment down once, after all processes finish.
var _ = SynchronizedAfterSuite(func() {}, func() {
	By("tearing down the e2e environment")
	Expect(e2eConfig.Teardown(context.Background())).To(Succeed(), "Failed to tear down e2e environment")
})

// newK8sClient builds a controller-runtime client with the CRQ types registered.
func newK8sClient() client.Client {
	cfg, err := k8sconfig.GetConfig()
	Expect(err).NotTo(HaveOccurred(), "Failed to get kubeconfig")

	Expect(v1alpha1.AddToScheme(scheme.Scheme)).To(Succeed(), "Failed to add ClusterResourceQuota types to scheme")

	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred(), "Failed to create k8s client")
	return c
}
