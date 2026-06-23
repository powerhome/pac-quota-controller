package utils

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
)

// Marks namespaces created by the e2e helpers so Scrub can find them.
const (
	E2ELabelKey   = "pac-quota-controller.powerapp.cloud/e2e"
	E2ELabelValue = "true"

	// Substring in test StorageClass names (e.g. "fast-ssd-e2e-<suffix>").
	e2eStorageClassMarker = "-e2e-"
)

const (
	defaultKindCluster   = "pac-quota-controller-test-e2e"
	defaultNodeImage     = "kindest/node:v1.34.0@sha256:7416a61b42b1662ca6ca89f02028ac133a309a2a30ba309614e8ec94d976dc5a"
	defaultImage         = "ghcr.io/powerhome/pac-quota-controller:latest"
	defaultHelmRelease   = "pac-quota-controller"
	defaultHelmNamespace = "pac-quota-controller-system"
	defaultCertManagerNS = "cert-manager"
	chartPath            = "./charts/pac-quota-controller"
	controllerDeployment = "pac-quota-controller-manager"
)

// E2EConfig holds the e2e environment settings, read from the environment.
type E2EConfig struct {
	KindCluster   string
	NodeImage     string
	Image         string
	HelmRelease   string
	HelmNamespace string
	CertManagerNS string

	// Don't create or delete the cluster; fail if missing.
	UseExistingCluster bool
	// Reuse the image already loaded into the cluster.
	SkipBuild bool
	// Leave the cluster running after the suite.
	SkipTeardown bool
	// Assume the cluster/chart already exist; only build the client.
	SkipSetup bool
}

// LoadE2EConfig reads the suite configuration from the environment.
func LoadE2EConfig() E2EConfig {
	return E2EConfig{
		KindCluster: envOr("KIND_CLUSTER", defaultKindCluster),
		NodeImage:   envOr("KIND_NODE_IMAGE", defaultNodeImage),
		// IMG is the Makefile/CI convention; honor it as a fallback.
		Image:              envOr("E2E_IMG", envOr("IMG", defaultImage)),
		HelmRelease:        envOr("HELM_RELEASE_NAME", defaultHelmRelease),
		HelmNamespace:      envOr("HELM_NAMESPACE", defaultHelmNamespace),
		CertManagerNS:      envOr("CERT_MANAGER_NAMESPACE", defaultCertManagerNS),
		UseExistingCluster: envBool("E2E_USE_EXISTING_CLUSTER"),
		SkipBuild:          envBool("E2E_SKIP_BUILD"),
		SkipTeardown:       envBool("E2E_SKIP_TEARDOWN"),
		SkipSetup:          envBool("E2E_SKIP_SETUP"),
	}
}

// Provision brings up the full environment: cluster, image, cert-manager, chart, readiness.
func (c E2EConfig) Provision(ctx context.Context) error {
	if c.SkipSetup {
		return nil
	}
	root, err := repoRoot()
	if err != nil {
		return err
	}
	if err := c.EnsureCluster(ctx); err != nil {
		return err
	}
	if err := c.BuildAndLoadImage(ctx, root); err != nil {
		return err
	}
	if err := c.InstallCertManager(ctx); err != nil {
		return err
	}
	if err := c.DeployChart(ctx, root); err != nil {
		return err
	}
	if err := c.WaitControllerReady(ctx); err != nil {
		return err
	}
	return c.ExportKubeconfig(ctx)
}

// EnsureCluster creates the kind cluster only if it does not already exist.
func (c E2EConfig) EnsureCluster(ctx context.Context) error {
	out, err := runCmdOutput(ctx, "", "kind", "get", "clusters")
	if err != nil {
		return err
	}
	for line := range strings.SplitSeq(out, "\n") {
		if strings.TrimSpace(line) == c.KindCluster {
			return nil // already exists, reuse it
		}
	}
	if c.UseExistingCluster {
		return fmt.Errorf("E2E_USE_EXISTING_CLUSTER is set but kind cluster %q does not exist", c.KindCluster)
	}
	return runCmd(ctx, "", "kind", "create", "cluster", "--name", c.KindCluster, "--image", c.NodeImage)
}

// BuildAndLoadImage builds the manager image and loads it into the kind cluster.
func (c E2EConfig) BuildAndLoadImage(ctx context.Context, root string) error {
	if c.SkipBuild {
		return nil
	}
	if err := runCmd(ctx, root, "docker", "build", "-t", c.Image, "."); err != nil {
		return err
	}
	return runCmd(ctx, "", "kind", "load", "docker-image", c.Image, "--name", c.KindCluster)
}

// InstallCertManager installs cert-manager via Helm and waits for its webhook.
func (c E2EConfig) InstallCertManager(ctx context.Context) error {
	// Ignore error: fails if the repo is already added.
	_ = runCmd(ctx, "", "helm", "repo", "add", "jetstack", "https://charts.jetstack.io")
	if err := runCmd(ctx, "", "helm", "repo", "update", "jetstack"); err != nil {
		return err
	}
	if err := runCmd(ctx, "", "helm", "upgrade", "--install", "cert-manager", "jetstack/cert-manager",
		"--namespace", c.CertManagerNS, "--create-namespace",
		"--set", "crds.enabled=true"); err != nil {
		return err
	}
	return runCmd(ctx, "", "kubectl", "-n", c.CertManagerNS, "wait",
		"--for=condition=Available", "deployment/cert-manager-webhook", "--timeout=2m")
}

// DeployChart installs/upgrades the controller Helm chart with the loaded image.
func (c E2EConfig) DeployChart(ctx context.Context, root string) error {
	repo, tag := splitImage(c.Image)
	return runCmd(ctx, root, "helm", "upgrade", "--install", c.HelmRelease, chartPath,
		"--namespace", c.HelmNamespace, "--create-namespace",
		"--set", "controllerManager.container.image.repository="+repo,
		"--set", "controllerManager.container.image.tag="+tag,
		"--set", "controllerManager.container.image.pullPolicy=Never",
		"--wait", "--timeout", "10m0s")
}

// WaitControllerReady blocks until the controller has rolled out and its pod is ready.
func (c E2EConfig) WaitControllerReady(ctx context.Context) error {
	if err := runCmd(ctx, "", "kubectl", "-n", c.HelmNamespace, "rollout", "status",
		"deployment/"+controllerDeployment, "--timeout=2m"); err != nil {
		return err
	}
	return runCmd(ctx, "", "kubectl", "-n", c.HelmNamespace, "wait",
		"--for=condition=ready", "pod", "--timeout=60s", "-l", "control-plane=controller-manager")
}

// ExportKubeconfig points the current kubeconfig context at the kind cluster.
func (c E2EConfig) ExportKubeconfig(ctx context.Context) error {
	if c.SkipSetup {
		return nil
	}
	return runCmd(ctx, "", "kind", "export", "kubeconfig", "--name", c.KindCluster)
}

// Teardown deletes the kind cluster unless told to keep or reuse it.
func (c E2EConfig) Teardown(ctx context.Context) error {
	if c.SkipSetup || c.SkipTeardown || c.UseExistingCluster {
		return nil
	}
	return runCmd(ctx, "", "kind", "delete", "cluster", "--name", c.KindCluster)
}

// Scrub deletes leftovers from a previous run: all CRQs (cluster-scoped, the main
// cross-run hazard) plus e2e-labeled namespaces and test StorageClasses.
func Scrub(ctx context.Context, k8sClient client.Client) error {
	crqList := &quotav1alpha1.ClusterResourceQuotaList{}
	if err := k8sClient.List(ctx, crqList); err != nil {
		return fmt.Errorf("scrub: list CRQs: %w", err)
	}
	for i := range crqList.Items {
		if err := k8sClient.Delete(ctx, &crqList.Items[i]); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("scrub: delete CRQ %s: %w", crqList.Items[i].Name, err)
		}
	}

	nsList := &corev1.NamespaceList{}
	if err := k8sClient.List(ctx, nsList, client.MatchingLabels{E2ELabelKey: E2ELabelValue}); err != nil {
		return fmt.Errorf("scrub: list namespaces: %w", err)
	}
	for i := range nsList.Items {
		if err := k8sClient.Delete(ctx, &nsList.Items[i]); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("scrub: delete namespace %s: %w", nsList.Items[i].Name, err)
		}
	}

	scList := &storagev1.StorageClassList{}
	if err := k8sClient.List(ctx, scList); err != nil {
		return fmt.Errorf("scrub: list storageclasses: %w", err)
	}
	for i := range scList.Items {
		if !strings.Contains(scList.Items[i].Name, e2eStorageClassMarker) {
			continue
		}
		if err := k8sClient.Delete(ctx, &scList.Items[i]); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("scrub: delete storageclass %s: %w", scList.Items[i].Name, err)
		}
	}
	return nil
}

// --- helpers ---

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envBool(key string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return v == "1" || v == "true" || v == "yes"
}

// splitImage splits "repo:tag" on the last colon.
func splitImage(image string) (repo, tag string) {
	if i := strings.LastIndex(image, ":"); i >= 0 && !strings.Contains(image[i:], "/") {
		return image[:i], image[i+1:]
	}
	return image, "latest"
}

// repoRoot walks up from this file to the dir containing go.mod.
func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("repoRoot: cannot determine caller path")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("repoRoot: go.mod not found above %s", filepath.Dir(file))
		}
		dir = parent
	}
}

func runCmd(ctx context.Context, dir, name string, args ...string) error {
	_, _ = fmt.Fprintf(os.Stdout, "[e2e] $ %s %s\n", name, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("command failed: %s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func runCmdOutput(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("command failed: %s %s: %w", name, strings.Join(args, " "), err)
	}
	return out.String(), nil
}
