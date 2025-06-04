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
	"path/filepath"
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
		projectDir, err := utils.GetProjectDir()
		Expect(err).NotTo(HaveOccurred(), "Failed to get project directory")

		cmd = exec.Command("kubectl", "apply", "-f", filepath.Join(projectDir, "test/fixtures/test-namespace.yaml"))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create test namespace")
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
			projectDir, err := utils.GetProjectDir()
			Expect(err).NotTo(HaveOccurred(), "Failed to get project directory")

			cmd := exec.Command("kubectl", "apply", "-f", filepath.Join(projectDir, "test/fixtures/test-cluster-quota.yaml"))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterResourceQuota")

			By("Starting the controller")
			cmd = exec.Command("make", "run")
			_, err = utils.StartCmd(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Allow time for the controller to reconcile
			time.Sleep(2 * time.Second)

			By("Verifying that ResourceQuota was created in the test namespace")
			Eventually(func() bool {
				cmd = exec.Command("kubectl", "get", "resourcequota", "managed-quota", "-n", testNamespace)
				output, err := utils.Run(cmd)
				return err == nil && strings.Contains(output, "managed-quota")
			}, "5s", "1s").Should(BeTrue())

			// Check that the specific resource limits were applied
			By("Checking resource limits")
			Eventually(func() string {
				cmd = exec.Command("kubectl", "get", "resourcequota", "managed-quota", "-n", testNamespace, "-o", "jsonpath={.spec.hard.pods}")
				output, err := utils.Run(cmd)
				if err != nil {
					return ""
				}
				return output
			}, "5s", "1s").Should(Equal("5"))

			// Stop the controller
			By("Stopping the controller")
			utils.StopCommand()
		})
	})
})
