package e2e

import (
	"fmt"
	"math/rand"
	"strconv"

	. "github.com/onsi/gomega"
	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// generateTestName generates a unique test name with the given prefix.
// This provides consistent naming across all e2e tests.
func generateTestName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, rand.Intn(1000000))
}

// generateTestSuffix generates a unique suffix for test resources.
// This is useful when tests need just a suffix for multiple resources.
func generateTestSuffix() string {
	return strconv.Itoa(rand.Intn(1000000))
}

func getCRQStatusNamespaces(crqName string) []string {
	crq := &quotav1alpha1.ClusterResourceQuota{}
	err := k8sClient.Get(ctx, client.ObjectKey{Name: crqName}, crq)
	if err != nil || crq.Status.Namespaces == nil {
		return nil
	}
	nsList := make([]string, 0, len(crq.Status.Namespaces))
	for _, ns := range crq.Status.Namespaces {
		nsList = append(nsList, ns.Namespace)
	}
	return nsList
}

func ensureNamespaceDeleted(name string) {
	ns := &corev1.Namespace{}
	_ = k8sClient.Get(ctx, client.ObjectKey{Name: name}, ns)
	_ = k8sClient.Delete(ctx, ns)
	// Wait for namespace to be deleted
	Eventually(func() error {
		return k8sClient.Get(ctx, client.ObjectKey{Name: name}, ns)
	}, "10s", "250ms").ShouldNot(Succeed())
}

func ensureCRQDeleted(name string) {
	crq := &quotav1alpha1.ClusterResourceQuota{}
	_ = k8sClient.Get(ctx, client.ObjectKey{Name: name}, crq)
	_ = k8sClient.Delete(ctx, crq)
	// Wait for CRQ to be deleted
	Eventually(func() error {
		return k8sClient.Get(ctx, client.ObjectKey{Name: name}, crq)
	}, "10s", "250ms").ShouldNot(Succeed())
}
