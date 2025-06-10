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
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	k8sconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
)

var (
	// projectImage is the name of the image which will be build and loaded
	// with the code source changes to be tested.
	projectImage = "ghcr.io/powerhome/pac-quota-controller:latest"

	// helmReleaseName is the name of the Helm release for the controller-manager.
	helmReleaseName = "pac-quota-controller"
	// helmNamespace is the Kubernetes namespace where the controller-manager will be deployed.
	helmNamespace = "pac-quota-controller-system"
	// helmChartPath is the file path to the Helm chart for the controller-manager.
	helmChartPath = "./charts/pac-quota-controller"

	k8sClient client.Client
	ctx       context.Context
)

// TestE2E runs the end-to-end (e2e) test suite for the project. These tests execute in an isolated,
// temporary environment to validate project changes with the purposed to be used in CI jobs.
// The default setup requires Kind, builds/loads the Manager Docker image locally, and installs
// CertManager.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting pac-quota-controller integration test suite\n")
	RunSpecs(t, "e2e suite")
}

var _ = BeforeSuite(func() {
	By("initializing Kubernetes client")
	var err error
	ctx = context.Background()
	cfg, err := k8sconfig.GetConfig()
	Expect(err).NotTo(HaveOccurred(), "Failed to get kubeconfig")

	// Register ClusterResourceQuota CRD types with the global scheme.
	// The global scheme already includes all built-in Kubernetes types.
	err = v1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred(), "Failed to add ClusterResourceQuota types to scheme")

	k8sClient, err = k8sclient.New(cfg, k8sclient.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred(), "Failed to create k8s client")
})

var _ = AfterSuite(func() {
})
