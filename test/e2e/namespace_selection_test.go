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
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/powerhome/pac-quota-controller/test/utils"
)

func crqStatusNamespaces(crqName string) []string {
	cmd := exec.Command("kubectl", "get", "clusterresourcequota", crqName, "-o", "jsonpath={.status.namespaces[*].namespace}")
	out, _ := utils.Run(cmd)
	if strings.TrimSpace(out) == "" {
		return nil
	}
	return strings.Fields(out)
}

var _ = Describe("ClusterResourceQuota Namespace Selection", Ordered, func() {
	const (
		crqName       = "test-namespaceselection-quota"
		matchingNS    = "test-namespaceselection-ns"
		nonMatchingNS = "test-namespaceselection-ns-wrong-label"
	)

	BeforeAll(func() {
		// No global setup needed, handled in e2e_suite_test.go

		By("Creating test namespaces")
		// Apply test manifest
		cmd := exec.Command("kubectl", "apply", "-f", "test/fixtures/namespace_selection/")
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create test manifests")

		// Wait for controller to process
		time.Sleep(2 * time.Second)
	})

	AfterAll(func() {
		By("Cleaning up test resources")
		cmd := exec.Command("kubectl", "delete", "namespace", matchingNS, nonMatchingNS, "--wait=false")
		_, _ = utils.Run(cmd)
		cmd = exec.Command("kubectl", "delete", "clusterresourcequota", crqName, "--wait=false")
		_, _ = utils.Run(cmd)
	})

	It("Should create a ClusterResourceQuota and check if it exists", func() {
		cmd := exec.Command("kubectl", "get", "clusterresourcequota", crqName)
		out, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring(crqName))
	})

	It("Should add a matching namespace to the CRQ status", func() {
		By("Waiting for namespace to appear in CRQ status")
		Eventually(func() []string {
			return crqStatusNamespaces(crqName)
		}, "10s", "1s").Should(ContainElement(matchingNS))
	})

	It("Should not add a non-matching namespace to the CRQ status", func() {
		Consistently(func() []string {
			return crqStatusNamespaces(crqName)
		}, "5s", "1s").ShouldNot(ContainElement(nonMatchingNS))
	})

	It("Should update CRQ status when a namespace label is changed to match", func() {
		cmd := exec.Command("kubectl", "label", "namespace", nonMatchingNS, "quota=limited", "--overwrite")
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() []string {
			return crqStatusNamespaces(crqName)
		}, "10s", "1s").Should(ContainElement(nonMatchingNS))
	})

	It("Should update CRQ status when a namespace label is changed to exclude it", func() {
		cmd := exec.Command("kubectl", "label", "namespace", matchingNS, "quota-", "--overwrite")
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() []string {
			return crqStatusNamespaces(crqName)
		}, "10s", "1s").ShouldNot(ContainElement(matchingNS))
	})

	It("Should update CRQ status when a namespace is deleted", func() {
		cmd := exec.Command("kubectl", "delete", "namespace", nonMatchingNS, "--wait=false")
		_, _ = utils.Run(cmd)

		Eventually(func() []string {
			return crqStatusNamespaces(crqName)
		}, "10s", "1s").ShouldNot(ContainElement(nonMatchingNS))
	})
})
