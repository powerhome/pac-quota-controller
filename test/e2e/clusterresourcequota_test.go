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
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/powerhome/pac-quota-controller/test/utils"
)

var _ = Describe("ClusterResourceQuota", Ordered, func() {
	const testNamespace = "test-quota-namespace"
	const quotaName = "test-cluster-quota"

	BeforeAll(func() {
		By("Installing CRDs")
		cmd := exec.Command("make", "install")
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("Creating test namespace with required labels")
		cmd = exec.Command("kubectl", "create", "ns", testNamespace)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create test namespace")

		cmd = exec.Command("kubectl", "label", "ns", testNamespace, "quota=limited")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label test namespace")
	})

	AfterAll(func() {
		By("Cleaning up test namespace and resources")

		// Check for finalizers on the ClusterResourceQuota before deletion
		cmd := exec.Command("kubectl", "get", "clusterresourcequota", quotaName, "-o", "jsonpath={.metadata.finalizers}")
		finalizers, _ := utils.Run(cmd)
		if finalizers != "" {
			By(fmt.Sprintf("Found finalizers on ClusterResourceQuota: %s", finalizers))
			// You might need to forcefully remove finalizers if they're preventing deletion
			patchCmd := exec.Command("kubectl", "patch", "clusterresourcequota", quotaName, "--type=json",
				"-p", "[{\"op\": \"remove\", \"path\": \"/metadata/finalizers\"}]")
			_, _ = utils.Run(patchCmd)
		}

		// Delete the ClusterResourceQuota
		cmd = exec.Command("kubectl", "delete", "clusterresourcequota", quotaName, "--ignore-not-found")
		_, _ = utils.Run(cmd)

		// Clean up the test namespace
		cmd = exec.Command("kubectl", "delete", "ns", testNamespace, "--ignore-not-found", "--wait=false")
		_, _ = utils.Run(cmd)

		// Add a timeout to ensure proper cleanup
		time.Sleep(2 * time.Second)

		By("Uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)
	})

	Context("Basic functionality", func() {
		It("should create a ClusterResourceQuota", func() {
			By("Creating the ClusterResourceQuota custom resource")
			crqManifest := `
apiVersion: quota.powerapp.cloud/v1alpha1
kind: ClusterResourceQuota
metadata:
  name: test-cluster-quota
spec:
  hard:
    pods: "10"
    requests.cpu: "1"
    requests.memory: 1Gi
    limits.cpu: "2"
    limits.memory: 2Gi
  namespaceSelector:
    matchLabels:
      quota: limited
  scopes:
  - NotTerminating
  scopeSelector:
    matchExpressions:
    - operator: In
      scopeName: PriorityClass
      values:
      - high
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(crqManifest)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply ClusterResourceQuota")

			By("Verifying the ClusterResourceQuota was created successfully")
			Eventually(func() error {
				cmd := exec.Command("kubectl", "get", "clusterresourcequota", quotaName, "-o", "jsonpath={.metadata.name}")
				_, err := utils.Run(cmd)
				return err
			}, time.Minute, time.Second).Should(Succeed(), "Failed to get ClusterResourceQuota")

			By("Checking the resource has the expected scopes and scopeSelector")
			Eventually(func() (string, error) {
				cmd := exec.Command("kubectl", "get", "clusterresourcequota", quotaName, "-o", "jsonpath={.spec.scopes[0]}")
				return utils.Run(cmd)
			}, time.Minute, time.Second).Should(Equal("NotTerminating"), "Failed to verify scopes")

			Eventually(func() (string, error) {
				cmd := exec.Command("kubectl", "get", "clusterresourcequota", quotaName, "-o", "jsonpath={.spec.scopeSelector.matchExpressions[0].scopeName}")
				return utils.Run(cmd)
			}, time.Minute, time.Second).Should(Equal("PriorityClass"), "Failed to verify scopeSelector")
		})
	})
})
