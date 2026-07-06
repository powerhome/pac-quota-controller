package storage

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// pvc builds a PVC with the given storage request and spec storage class.
func pvc(name, storageReq, class string) corev1.PersistentVolumeClaim {
	p := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(storageReq)},
			},
		},
	}
	if class != "" {
		p.Spec.StorageClassName = &class
	}
	return p
}

var _ = Describe("Storage list aggregation", func() {
	Describe("CalculateStorageUsageFromPVCs", func() {
		It("sums storage requests across PVCs", func() {
			pvcs := []corev1.PersistentVolumeClaim{
				pvc("a", "10Gi", ""),
				pvc("b", "5Gi", ""),
			}
			total := CalculateStorageUsageFromPVCs(pvcs, corev1.ResourceRequestsStorage)
			Expect(total.Equal(resource.MustParse("15Gi"))).To(BeTrue())
		})

		It("returns zero for a non-storage resource name", func() {
			pvcs := []corev1.PersistentVolumeClaim{pvc("a", "10Gi", "")}
			total := CalculateStorageUsageFromPVCs(pvcs, corev1.ResourceRequestsMemory)
			Expect(total.IsZero()).To(BeTrue())
		})

		It("returns zero for an empty list", func() {
			total := CalculateStorageUsageFromPVCs(nil, corev1.ResourceRequestsStorage)
			Expect(total.IsZero()).To(BeTrue())
		})
	})

	Describe("CalculatePVCCountUsageFromPVCs", func() {
		It("counts PVCs", func() {
			pvcs := []corev1.PersistentVolumeClaim{pvc("a", "1Gi", ""), pvc("b", "1Gi", "")}
			count := CalculatePVCCountUsageFromPVCs(pvcs)
			Expect(count.Value()).To(Equal(int64(2)))
		})

		It("returns zero for an empty list", func() {
			count := CalculatePVCCountUsageFromPVCs(nil)
			Expect(count.Value()).To(Equal(int64(0)))
		})
	})

	Describe("CalculateStorageClassUsageFromPVCs", func() {
		It("sums only PVCs matching the storage class", func() {
			pvcs := []corev1.PersistentVolumeClaim{
				pvc("fast-a", "10Gi", "fast"),
				pvc("fast-b", "20Gi", "fast"),
				pvc("slow-a", "100Gi", "slow"),
			}
			total := CalculateStorageClassUsageFromPVCs(pvcs, "fast")
			Expect(total.Equal(resource.MustParse("30Gi"))).To(BeTrue())
		})

		It("returns zero when no PVC matches", func() {
			pvcs := []corev1.PersistentVolumeClaim{pvc("slow-a", "100Gi", "slow")}
			total := CalculateStorageClassUsageFromPVCs(pvcs, "fast")
			Expect(total.IsZero()).To(BeTrue())
		})
	})

	Describe("CalculateStorageClassCountFromPVCs", func() {
		It("counts only PVCs matching the storage class", func() {
			pvcs := []corev1.PersistentVolumeClaim{
				pvc("fast-a", "10Gi", "fast"),
				pvc("fast-b", "20Gi", "fast"),
				pvc("slow-a", "100Gi", "slow"),
			}
			Expect(CalculateStorageClassCountFromPVCs(pvcs, "fast")).To(Equal(int64(2)))
		})

		It("returns zero when no PVC matches", func() {
			pvcs := []corev1.PersistentVolumeClaim{pvc("slow-a", "100Gi", "slow")}
			Expect(CalculateStorageClassCountFromPVCs(pvcs, "fast")).To(Equal(int64(0)))
		})
	})
})

var _ = Describe("PVCStorageClass", func() {
	It("returns the spec storage class name when set", func() {
		p := pvc("a", "1Gi", "fast")
		Expect(PVCStorageClass(&p)).To(Equal("fast"))
	})

	It("falls back to the legacy annotation when spec is unset", func() {
		p := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{"volume.beta.kubernetes.io/storage-class": "legacy"},
			},
		}
		Expect(PVCStorageClass(&p)).To(Equal("legacy"))
	})

	It("returns empty when neither spec nor annotation is set", func() {
		p := corev1.PersistentVolumeClaim{}
		Expect(PVCStorageClass(&p)).To(Equal(""))
	})

	It("returns empty for a nil PVC", func() {
		Expect(PVCStorageClass(nil)).To(Equal(""))
	})
})

var _ = Describe("PVCMatchesStorageClass legacy annotation", func() {
	It("matches via the legacy annotation when spec is unset", func() {
		p := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{"volume.beta.kubernetes.io/storage-class": "legacy"},
			},
		}
		Expect(PVCMatchesStorageClass(&p, "legacy")).To(BeTrue())
		Expect(PVCMatchesStorageClass(&p, "other")).To(BeFalse())
	})

	It("does not match when spec and annotation are both unset", func() {
		p := corev1.PersistentVolumeClaim{}
		Expect(PVCMatchesStorageClass(&p, "")).To(BeFalse())
	})

	It("returns false for a nil PVC", func() {
		Expect(PVCMatchesStorageClass(nil, "fast")).To(BeFalse())
	})
})
