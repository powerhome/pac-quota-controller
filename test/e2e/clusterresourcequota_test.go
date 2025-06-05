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
	"os/exec"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/powerhome/pac-quota-controller/test/utils"
)

var _ = Describe("ClusterResourceQuota", Ordered, func() {
	const testNamespace = "test-clusterresourcequota-ns"
	const quotaName = "test-clusterresourcequota-quota"

	AfterAll(func() {
		cmd := exec.Command("kubectl", "delete", "clusterresourcequota", quotaName, "--ignore-not-found")
		_, _ = utils.Run(cmd)

		// Clean up the test namespace
		cmd = exec.Command("kubectl", "delete", "ns", testNamespace, "--ignore-not-found", "--wait=false")
		_, _ = utils.Run(cmd)

		// Add a timeout to ensure proper cleanup
		time.Sleep(2 * time.Second)
	})

	Context("Basic functionality", func() {
		It("should create a ClusterResourceQuota and update its status with matching namespaces", func() {
			By("Creating the test manifests")
			projectDir, err := utils.GetProjectDir()
			Expect(err).NotTo(HaveOccurred(), "Failed to get project directory")

			cmd := exec.Command(
				"kubectl", "apply", "-f",
				filepath.Join(
					projectDir,
					"test/fixtures/clusterresourcequota/",
				),
			)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create test manifests")

			By("Verifying that the ClusterResourceQuota exists")
			cmd = exec.Command("kubectl", "get", "clusterresourcequota", quotaName)
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring(quotaName))

			By("Verifying that the status field is updated with matching namespaces")
			Eventually(func() string {
				cmd = exec.Command(
					"kubectl", "get", "clusterresourcequota", quotaName,
					"-o", "jsonpath={.status.namespaces[*].namespace}",
				)
				out, _ := utils.Run(cmd)
				return out
			}, "10s", "1s").Should(ContainSubstring(testNamespace))
		})
	})
})
