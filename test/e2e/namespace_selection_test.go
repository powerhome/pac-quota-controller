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

var _ = Describe("NamespaceSelection", Ordered, func() {
	const (
		testNamespace1 = "test-ns-selection-1"
		testNamespace2 = "test-ns-selection-2"
		testNamespace3 = "test-ns-selection-3"
		quotaName      = "ns-selector-test-quota"
	)

	BeforeAll(func() {
		By("Installing CRDs")
		cmd := exec.Command("make", "install")
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("Creating test namespaces with different labels")
		// Create namespace 1 with label team=frontend
		cmd = exec.Command("kubectl", "create", "namespace", testNamespace1)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create test namespace 1")

		cmd = exec.Command("kubectl", "label", "namespace", testNamespace1, "team=frontend")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label test namespace 1")

		// Create namespace 2 with label team=backend
		cmd = exec.Command("kubectl", "create", "namespace", testNamespace2)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create test namespace 2")

		cmd = exec.Command("kubectl", "label", "namespace", testNamespace2, "team=backend")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label test namespace 2")

		// Create namespace 3 with different label
		cmd = exec.Command("kubectl", "create", "namespace", testNamespace3)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create test namespace 3")

		cmd = exec.Command("kubectl", "label", "namespace", testNamespace3, "team=other")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label test namespace 3")

		By("Starting the controller")
		cmd = exec.Command("make", "run")
		_, err = utils.StartCmd(cmd)
		Expect(err).NotTo(HaveOccurred())

		// Wait for controller to start
		time.Sleep(5 * time.Second)
	})

	AfterAll(func() {
		By("Stopping the controller")
		utils.StopCommand()

		By("Cleaning up test namespaces")
		cmd := exec.Command("kubectl", "delete", "namespace", testNamespace1, testNamespace2, testNamespace3, "--wait=false")
		_, _ = utils.Run(cmd)

		By("Cleaning up ClusterResourceQuota")
		cmd = exec.Command("kubectl", "delete", "clusterresourcequota", quotaName, "--wait=false")
		_, _ = utils.Run(cmd)

		By("Uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)
	})

	Context("Label selector tests", func() {
		It("Should create resource quotas in namespaces matching label selector", func() {
			// Create ClusterResourceQuota with label selector for team=frontend and team=backend
			quotaYAML := fmt.Sprintf(`
apiVersion: quota.powerapp.cloud/v1alpha1
kind: ClusterResourceQuota
metadata:
  name: %s
spec:
  resourceQuotaName: label-quota
  selector:
    labels:
      matchLabels:
        team: frontend
  quota:
    hard:
      pods: "10"
      limits.cpu: "2"
      requests.cpu: "1"
`, quotaName)

			// Apply the ClusterResourceQuota
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(quotaYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterResourceQuota")

			// Wait for controller to process
			time.Sleep(2 * time.Second)

			// Check if ResourceQuota was created in namespace1 (should match label)
			By("Checking if ResourceQuota was created in namespace with matching label")
			Eventually(func() bool {
				cmd = exec.Command("kubectl", "get", "resourcequota", "label-quota", "-n", testNamespace1)
				out, err := utils.Run(cmd)
				return err == nil && strings.Contains(out, "label-quota")
			}, "5s", "1s").Should(BeTrue(), "ResourceQuota should be created in namespace with matching label")

			// Check that ResourceQuota was NOT created in namespace2 and namespace3 (shouldn't match label)
			By("Checking that ResourceQuota wasn't created in namespaces without matching label")
			Consistently(func() bool {
				cmd = exec.Command("kubectl", "get", "resourcequota", "label-quota", "-n", testNamespace2)
				_, err := utils.Run(cmd)
				return err != nil
			}, "5s", "1s").Should(BeTrue(), "ResourceQuota shouldn't be created in namespace without matching label")

			Consistently(func() bool {
				cmd = exec.Command("kubectl", "get", "resourcequota", "label-quota", "-n", testNamespace3)
				_, err := utils.Run(cmd)
				return err != nil
			}, "5s", "1s").Should(BeTrue(), "ResourceQuota shouldn't be created in namespace without matching label")
		})

		It("Should update ResourceQuotas when label selector changes", func() {
			// Update ClusterResourceQuota to select team=backend
			quotaYAML := fmt.Sprintf(`
apiVersion: quota.powerapp.cloud/v1alpha1
kind: ClusterResourceQuota
metadata:
  name: %s
spec:
  resourceQuotaName: label-quota
  selector:
    labels:
      matchLabels:
        team: backend
  quota:
    hard:
      pods: "10"
      limits.cpu: "2"
      requests.cpu: "1"
`, quotaName)

			// Apply the updated ClusterResourceQuota
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(quotaYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to update ClusterResourceQuota")

			// Wait for controller to process
			time.Sleep(2 * time.Second)

			// Check that ResourceQuota was moved from namespace1 to namespace2
			By("Checking if ResourceQuota was removed from original namespace")
			Eventually(func() bool {
				cmd = exec.Command("kubectl", "get", "resourcequota", "label-quota", "-n", testNamespace1)
				_, err := utils.Run(cmd)
				return err != nil
			}, "5s", "1s").Should(BeTrue(), "ResourceQuota should be removed from original namespace")

			By("Checking if ResourceQuota was created in new matching namespace")
			Eventually(func() bool {
				cmd = exec.Command("kubectl", "get", "resourcequota", "label-quota", "-n", testNamespace2)
				out, err := utils.Run(cmd)
				return err == nil && strings.Contains(out, "label-quota")
			}, "5s", "1s").Should(BeTrue(), "ResourceQuota should be created in new matching namespace")
		})
	})
})
