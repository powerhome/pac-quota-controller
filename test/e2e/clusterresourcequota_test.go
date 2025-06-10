/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"context"
	"math/rand"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("ClusterResourceQuota", func() {
	var (
		ctx     = context.Background()
		suffix  string
		crqName string
	)
	BeforeEach(func() {
		suffix = strconv.Itoa(rand.Intn(1000000))
		crqName = "test-crq-" + suffix
	})
	It("should create a ClusterResourceQuota with valid spec", func() {
		crq := &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: crqName,
			},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				Hard: quotav1alpha1.ResourceList{
					corev1.ResourcePods:   resource.MustParse("10"),
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"quota": "limited-" + suffix},
				},
			},
		}
		Expect(k8sClient.Create(ctx, crq)).To(Succeed())
	})
})

func resourceMustParse(val string) resource.Quantity {
	return resource.MustParse(val)
}
