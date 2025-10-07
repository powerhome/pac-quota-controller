/*
Copyright 2025 PowerHome.

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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/powerhome/pac-quota-controller/test/utils"
)

// namespace where the project is deployed in
const namespace = "pac-quota-controller-system"

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	// Before running the tests, set up the environment by creating the namespace,
	// enforce the restricted security policy to the namespace, installing CRDs,
	// and deploying the controller.
	BeforeAll(func() {
		By("labeling the namespace to enforce the restricted security policy and allow metrics")
		ns := &v1.Namespace{}
		err := k8sClient.Get(ctx, client.ObjectKey{Name: namespace}, ns)
		if err == nil {
			if ns.Labels == nil {
				ns.Labels = make(map[string]string)
			}
			ns.Labels["pod-security.kubernetes.io/enforce"] = "restricted"
			ns.Labels["metrics"] = "enabled" // Add label for metrics NetworkPolicy
			err = k8sClient.Update(ctx, ns)
		} else {
			ns = &v1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
					Labels: map[string]string{
						"pod-security.kubernetes.io/enforce": "restricted",
						"metrics":                            "enabled",
					},
				},
			}
			err = k8sClient.Create(ctx, ns)
		}
		Expect(err).NotTo(HaveOccurred(), "Failed to label namespace")
	})

	// After all tests have been executed, clean up by undeploying the controller, uninstalling CRDs,
	// and deleting the namespace.
	AfterAll(func() {
		By("cleaning up the curl pod for metrics")
		_ = k8sClient.Delete(ctx, &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "curl-metrics", Namespace: namespace}})
	})

	// After each test, check for failures and collect logs, events,
	// and pod descriptions for debugging.
	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs")
			logs := utils.GetPodLogs(ctx, clientSet, namespace, controllerPodName)
			_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", logs)

			By("Fetching curl-metrics logs")
			metricsOutput := utils.GetPodLogs(ctx, clientSet, namespace, "curl-metrics")
			_, _ = fmt.Fprintf(GinkgoWriter, "Metrics logs:\n %s", metricsOutput)

			By("Fetching controller manager pod description")
			podDesc := utils.DescribePod(ctx, k8sClient, namespace, controllerPodName)
			fmt.Println("Pod description:\n", podDesc)
		}
	})

	SetDefaultEventuallyTimeout(3 * time.Minute)         // Increased timeout for leader election
	SetDefaultEventuallyPollingInterval(2 * time.Second) // Increased polling interval

	Context("Manager", func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func(g Gomega) {
				podList := &v1.PodList{}
				err := k8sClient.List(
					ctx,
					podList,
					client.MatchingLabels{
						"control-plane": "controller-manager",
					},
					client.InNamespace(namespace),
				)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
				var runningPods []string
				for _, pod := range podList.Items {
					if pod.DeletionTimestamp == nil {
						runningPods = append(runningPods, pod.Name)
					}
				}
				g.Expect(runningPods).To(HaveLen(1), "expected 1 controller pod running")
				controllerPodName = runningPods[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				pod := &v1.Pod{}
				err = k8sClient.Get(ctx, client.ObjectKey{Name: controllerPodName, Namespace: namespace}, pod)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(pod.Status.Phase).To(Equal(v1.PodRunning), "Incorrect controller-manager pod status")

				// Check that all containers are ready
				for _, container := range pod.Status.ContainerStatuses {
					g.Expect(container.Ready).To(BeTrue(), "Container %s is not ready", container.Name)
				}
			}
			Eventually(verifyControllerUp).Should(Succeed())

			By("waiting for controller to acquire leadership")
			Eventually(func(g Gomega) {
				logs := utils.GetPodLogs(ctx, clientSet, namespace, controllerPodName)
				g.Expect(logs).To(ContainSubstring("successfully acquired lease"), "Controller should have acquired leadership")
			}, 2*time.Minute, 5*time.Second).Should(Succeed())
		})
	})
})
