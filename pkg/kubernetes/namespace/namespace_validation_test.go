package namespace

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	crfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
)

// teamA is a shared (read-only) label set used across the conflict tests.
var teamA = map[string]string{"team": "a"}

func crqSelecting(name string, matchLabels map[string]string) *quotav1alpha1.ClusterResourceQuota {
	return &quotav1alpha1.ClusterResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: quotav1alpha1.ClusterResourceQuotaSpec{
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: matchLabels},
		},
	}
}

func namespaceWithLabels(name string, lbls map[string]string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: lbls}}
}

func validationScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	Expect(corev1.AddToScheme(s)).To(Succeed())
	Expect(quotav1alpha1.AddToScheme(s)).To(Succeed())
	return s
}

var _ = Describe("Namespace CRQ conflict validation", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	// newCRQClient builds a quota.CRQClient backed by a fake runtime client holding crqs.
	newCRQClient := func(crqs ...*quotav1alpha1.ClusterResourceQuota) *quota.CRQClient {
		builder := crfake.NewClientBuilder().WithScheme(validationScheme())
		for _, crq := range crqs {
			builder = builder.WithObjects(crq)
		}
		return quota.NewCRQClient(builder.Build(), zap.NewNop())
	}

	Describe("ValidateNamespaceAgainstCRQs (validator method)", func() {
		It("returns nil when no CRQ selects the namespace", func() {
			validator := NewNamespaceValidator(k8sfake.NewSimpleClientset(), newCRQClient(crqSelecting("crq-a", teamA)))
			err := validator.ValidateNamespaceAgainstCRQs(ctx, namespaceWithLabels("ns1", map[string]string{"team": "b"}))
			Expect(err).NotTo(HaveOccurred())
		})

		It("returns nil when exactly one CRQ selects the namespace", func() {
			validator := NewNamespaceValidator(k8sfake.NewSimpleClientset(), newCRQClient(crqSelecting("crq-a", teamA)))
			err := validator.ValidateNamespaceAgainstCRQs(ctx, namespaceWithLabels("ns1", teamA))
			Expect(err).NotTo(HaveOccurred())
		})

		It("returns an error when multiple CRQs select the namespace", func() {
			validator := NewNamespaceValidator(
				k8sfake.NewSimpleClientset(),
				newCRQClient(
					crqSelecting("crq-a", map[string]string{"team": "a"}),
					crqSelecting("crq-b", map[string]string{"team": "a"}),
				),
			)
			err := validator.ValidateNamespaceAgainstCRQs(ctx, namespaceWithLabels("ns1", map[string]string{"team": "a"}))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("multiple ClusterResourceQuotas select namespace"))
		})
	})

	Describe("ValidateNamespaceAgainstCRQs (free function)", func() {
		It("returns nil when the CRQ client is nil", func() {
			ns := namespaceWithLabels("ns1", teamA)
			err := ValidateNamespaceAgainstCRQs(ctx, k8sfake.NewSimpleClientset(), nil, ns)
			Expect(err).NotTo(HaveOccurred())
		})

		It("returns an error when multiple CRQs select the namespace", func() {
			crqClient := newCRQClient(crqSelecting("crq-a", teamA), crqSelecting("crq-b", teamA))
			ns := namespaceWithLabels("ns1", teamA)
			err := ValidateNamespaceAgainstCRQs(ctx, k8sfake.NewSimpleClientset(), crqClient, ns)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("multiple ClusterResourceQuotas select namespace"))
		})
	})

	Describe("namespaceMatchesCRQ", func() {
		It("returns true when the namespace matches the CRQ selector", func() {
			validator := NewNamespaceValidator(k8sfake.NewSimpleClientset(), newCRQClient())
			matches, err := validator.namespaceMatchesCRQ(
				namespaceWithLabels("ns1", map[string]string{"team": "a"}),
				crqSelecting("crq-a", map[string]string{"team": "a"}),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(matches).To(BeTrue())
		})

		It("returns false when the namespace does not match", func() {
			validator := NewNamespaceValidator(k8sfake.NewSimpleClientset(), newCRQClient())
			matches, err := validator.namespaceMatchesCRQ(
				namespaceWithLabels("ns1", map[string]string{"team": "b"}),
				crqSelecting("crq-a", map[string]string{"team": "a"}),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(matches).To(BeFalse())
		})

		It("returns false when the CRQ client is nil", func() {
			validator := NewNamespaceValidator(k8sfake.NewSimpleClientset(), nil)
			matches, err := validator.namespaceMatchesCRQ(
				namespaceWithLabels("ns1", map[string]string{"team": "a"}),
				crqSelecting("crq-a", map[string]string{"team": "a"}),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(matches).To(BeFalse())
		})
	})

	Describe("findConflictingCRQsForNamespaces", func() {
		It("reports namespaces already owned by another CRQ, excluding the current one", func() {
			ns1 := namespaceWithLabels("ns1", map[string]string{"team": "a"})
			validator := NewNamespaceValidator(
				k8sfake.NewSimpleClientset(ns1),
				newCRQClient(
					crqSelecting("crq-a", map[string]string{"team": "a"}),
					crqSelecting("crq-b", map[string]string{"team": "a"}),
				),
			)
			conflicts, err := validator.findConflictingCRQsForNamespaces(ctx, []string{"ns1"}, "crq-a")
			Expect(err).NotTo(HaveOccurred())
			Expect(conflicts).To(HaveKeyWithValue("ns1", ConsistOf("crq-b")))
		})

		It("returns no conflicts when the namespace does not exist", func() {
			validator := NewNamespaceValidator(
				k8sfake.NewSimpleClientset(),
				newCRQClient(crqSelecting("crq-b", map[string]string{"team": "a"})),
			)
			conflicts, err := validator.findConflictingCRQsForNamespaces(ctx, []string{"missing"}, "crq-a")
			Expect(err).NotTo(HaveOccurred())
			Expect(conflicts).To(BeEmpty())
		})
	})

	Describe("ValidateCRQNamespaceConflicts conflict path", func() {
		It("returns an ownership-conflict error when an intended namespace is owned by another CRQ", func() {
			ns1 := namespaceWithLabels("ns1", map[string]string{"team": "a"})
			validator := NewNamespaceValidator(
				k8sfake.NewSimpleClientset(ns1),
				newCRQClient(crqSelecting("crq-b", map[string]string{"team": "a"})),
			)
			err := validator.ValidateCRQNamespaceConflicts(ctx, crqSelecting("crq-a", map[string]string{"team": "a"}))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("namespace ownership conflict"))
		})
	})

	Describe("GetSelectedNamespaces with matching namespaces", func() {
		It("returns the sorted set of namespaces matching the CRQ selector", func() {
			client := k8sfake.NewSimpleClientset(
				namespaceWithLabels("ns-b", map[string]string{"team": "a"}),
				namespaceWithLabels("ns-a", map[string]string{"team": "a"}),
				namespaceWithLabels("ns-c", map[string]string{"team": "other"}),
			)
			selected, err := GetSelectedNamespaces(ctx, client, crqSelecting("crq-a", map[string]string{"team": "a"}))
			Expect(err).NotTo(HaveOccurred())
			Expect(selected).To(Equal([]string{"ns-a", "ns-b"}))
		})
	})
})
