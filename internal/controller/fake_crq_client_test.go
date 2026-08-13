package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
)

// fakeCRQClient is a hand-written stand-in for quota.CRQClientInterface. Every test
// using it only needs a fixed return value, never call-count or argument assertions,
// so a mocking framework added nothing but ceremony.
type fakeCRQClient struct {
	listAllCRQsResult []quotav1alpha1.ClusterResourceQuota
	listAllCRQsErr    error

	getCRQByNamespaceResult *quotav1alpha1.ClusterResourceQuota
	getCRQByNamespaceErr    error
}

func (f *fakeCRQClient) ListAllCRQs(ctx context.Context) ([]quotav1alpha1.ClusterResourceQuota, error) {
	return f.listAllCRQsResult, f.listAllCRQsErr
}

func (f *fakeCRQClient) GetCRQByNamespace(
	ctx context.Context, ns *corev1.Namespace,
) (*quotav1alpha1.ClusterResourceQuota, error) {
	return f.getCRQByNamespaceResult, f.getCRQByNamespaceErr
}

// Neither method below is exercised by any test today (confirmed: the controller
// package never calls them — only pkg/kubernetes/namespace and quota.go itself do,
// and both are tested against the real CRQClient elsewhere). Panic loudly rather than
// silently return a zero value, so a future test that starts relying on one of these
// fails immediately instead of passing on a wrong default.

func (f *fakeCRQClient) NamespaceMatchesCRQ(
	ns *corev1.Namespace, crq *quotav1alpha1.ClusterResourceQuota,
) (bool, error) {
	panic("fakeCRQClient.NamespaceMatchesCRQ: not stubbed for this test")
}

func (f *fakeCRQClient) GetNamespacesFromStatus(crq *quotav1alpha1.ClusterResourceQuota) []string {
	panic("fakeCRQClient.GetNamespacesFromStatus: not stubbed for this test")
}
