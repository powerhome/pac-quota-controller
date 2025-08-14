# Image URL to use all building/pushing image targets
IMG ?= ghcr.io/powerhome/pac-quota-controller:latest
HELM_RELEASE_NAME ?= pac-quota-controller
HELM_NAMESPACE ?= pac-quota-controller-system

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet setup-envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test $$(go list ./... | grep -v /e2e) -coverprofile cover.out

# TODO(user): To use a different vendor for e2e tests, modify the setup under 'tests/e2e'.
# The default setup assumes Kind is pre-installed and builds/loads the Manager Docker image locally.
# CertManager is installed by default; skip with:
# - CERT_MANAGER_INSTALL_SKIP=true
KIND_CLUSTER ?= pac-quota-controller-test-e2e

.PHONY: cleanup-test-e2e
cleanup-test-e2e: ## Tear down the Kind cluster used for e2e tests
	@$(KIND) delete cluster --name $(KIND_CLUSTER) || true

.PHONY: install-tools
install-tools: ## Install development tools
	go install github.com/onsi/ginkgo/v2/ginkgo@latest
	go install github.com/onsi/gomega/...@latest

.PHONY: test-e2e-setup
test-e2e-setup:
	@echo "[test-e2e-setup] Building manager image..."
	make docker-build IMG=$(IMG)
	@echo "[test-e2e-setup] Ensuring Kind cluster exists..."
	$(eval KIND_CLUSTER ?= pac-quota-controller-test-e2e)
	@if ! kind get clusters | grep -q "^$(KIND_CLUSTER)$$" ; then \
		kind create cluster --name $(KIND_CLUSTER); \
	fi
	@echo "[test-e2e-setup] Loading image to Kind..."
	kind load docker-image $(IMG) --name $(KIND_CLUSTER)
	make install-cert-manager
	@echo "[test-e2e-setup] Deploying Helm chart..."
	make helm-deploy IMG=$(IMG)
	@echo "[test-e2e-setup] Helm chart deployed and controller is available."

.PHONY: test-e2e-cleanup
# Clean up Kind cluster before/after e2e tests for a fully clean environment
test-e2e-cleanup:
	@echo "[test-e2e-cleanup] Deleting Kind cluster..."
	$(KIND) delete cluster --name $(KIND_CLUSTER) || true

.PHONY: test-e2e
# Run e2e tests with setup/cleanup
test-e2e: test-e2e-cleanup test-e2e-setup
	KIND_CLUSTER=$(KIND_CLUSTER) go test ./test/e2e/ -v -ginkgo.v; \
	make test-e2e-cleanup

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	$(GOLANGCI_LINT) run --fix

.PHONY: lint-config
lint-config: golangci-lint ## Verify golangci-lint linter configuration
	$(GOLANGCI_LINT) config verify

##@ Kind Deployment

# Define the Kind cluster name
KIND_DEV_CLUSTER ?= pac-quota-controller-dev

.PHONY: kind-up
kind-up: ## Create a local Kind cluster for development
	@command -v $(KIND) >/dev/null 2>&1 || { \
		echo "Kind is not installed. Please install Kind manually."; \
		exit 1; \
	}
	@$(KIND) get clusters | grep -q $(KIND_DEV_CLUSTER) || { \
		echo "Creating Kind cluster: $(KIND_DEV_CLUSTER)"; \
		$(KIND) create cluster --name $(KIND_DEV_CLUSTER); \
	}
	@$(KUBECTL) config use-context kind-$(KIND_DEV_CLUSTER)
	@echo "Kind cluster $(KIND_DEV_CLUSTER) is ready"

.PHONY: kind-build
kind-build: docker-build ## Build and load the controller image into Kind cluster
	@echo "Loading image ${IMG} into Kind cluster..."
	@$(KIND) load docker-image ${IMG} --name $(KIND_DEV_CLUSTER)
	@echo "Image loaded successfully!"

.PHONY: kind-deploy
kind-deploy: kind-up kind-build install-cert-manager ## Deploy controller to local Kind cluster
	@echo "Deploying controller to local Kind cluster with Helm..."
	make helm-deploy IMG=$(IMG)

.PHONY: kind-logs
kind-logs: ## Get logs from the controller
	@$(KUBECTL) -n pac-quota-controller-system logs -l control-plane=controller-manager -f

.PHONY: kind-restart
kind-restart: ## Restart the controller deployment
	@$(KUBECTL) -n pac-quota-controller-system rollout restart deployment pac-quota-controller-manager
	@echo "Controller restarting..."
	@$(KUBECTL) -n pac-quota-controller-system rollout status deployment pac-quota-controller-manager

.PHONY: kind-down
kind-down: ## Delete the local Kind cluster
	@$(KIND) delete cluster --name $(KIND_DEV_CLUSTER)
	@echo "Kind cluster $(KIND_DEV_CLUSTER) deleted"

##@ Build

# CERT_MANAGER_INSTALL controls whether cert-manager is installed as part of the Helm deployment.
# Default is true. Set to false if cert-manager is already installed or managed externally.
CERT_MANAGER_INSTALL ?= true

.PHONY: install-cert-manager
install-cert-manager: ## Install cert-manager using Helm for e2e tests or local dev
	@echo "Installing cert-manager..."
	helm repo add jetstack https://charts.jetstack.io || true
	helm repo update
	helm upgrade --install cert-manager jetstack/cert-manager \
	  --namespace cert-manager \
	  --create-namespace \
	  --set crds.enabled=true \
	  --wait --timeout 10m0s
	@echo "Waiting for cert-manager webhook to be ready..."
	@$(KUBECTL) -n cert-manager wait --for=condition=Available deployment/cert-manager-webhook --timeout=2m
	@echo "Cert-manager installed and webhook is ready."

.PHONY: uninstall-cert-manager
uninstall-cert-manager: ## Uninstall cert-manager
	@echo "Uninstalling cert-manager..."
	helm uninstall cert-manager --namespace cert-manager || echo "Cert-manager not found or uninstall failed."
	kubectl delete namespace cert-manager --ignore-not-found=true || echo "Namespace cert-manager not found."
	@echo "Cert-manager uninstalled."

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager cmd/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/main.go

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	$(CONTAINER_TOOL) build -t ${IMG} .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	$(CONTAINER_TOOL) push ${IMG}

# PLATFORMS defines the target platforms for the manager image be built to provide support to multiple
# architectures. (i.e. make docker-buildx IMG=myregistry/mypoperator:0.0.1). To use this option you need to:
# - be able to use docker buildx. More info: https://docs.docker.com/build/buildx/
# - have enabled BuildKit. More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# - be able to push the image to your registry (i.e. if you do not set a valid value via IMG=<myregistry/image:<tag>> then the export will fail)
# To adequately provide solutions that are compatible with multiple platforms, you should consider using this option.
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager for cross-platform support
	# copy existing Dockerfile and insert --platform=${BUILDPLATFORM} into Dockerfile.cross, and preserve the original Dockerfile
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross
	$(CONTAINER_TOOL) buildx create --name pac-quota-controller-builder || echo "Builder creation failed. Ensure Buildx is installed and configured correctly."
	$(CONTAINER_TOOL) buildx use pac-quota-controller-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${IMG} -f Dockerfile.cross .
	- $(CONTAINER_TOOL) buildx rm pac-quota-controller-builder
	rm Dockerfile.cross

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL ?= kubectl
KIND ?= kind
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint

## Tool Versions
KUSTOMIZE_VERSION ?= v5.6.0
CONTROLLER_TOOLS_VERSION ?= v0.18.0
#ENVTEST_VERSION is the version of controller-runtime release branch to fetch the envtest setup script (i.e. release-0.20)
ENVTEST_VERSION ?= $(shell go list -m -f "{{ .Version }}" sigs.k8s.io/controller-runtime | awk -F'[v.]' '{printf "release-%d.%d", $$2, $$3}')
#ENVTEST_K8S_VERSION is the version of Kubernetes to use for setting up ENVTEST binaries (i.e. 1.31)
ENVTEST_K8S_VERSION ?= $(shell go list -m -f "{{ .Version }}" k8s.io/api | awk -F'[v.]' '{printf "1.%d", $$3}')
GOLANGCI_LINT_VERSION ?= v2.1.0

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: setup-envtest
setup-envtest: envtest ## Download the binaries required for ENVTEST in the local bin directory.
	@echo "Setting up envtest binaries for Kubernetes version $(ENVTEST_K8S_VERSION)..."
	@$(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path || { \
		echo "Error: Failed to set up envtest binaries for version $(ENVTEST_K8S_VERSION)."; \
		exit 1; \
	}

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

##@ Release

.PHONY: install-goreleaser
install-goreleaser: ## Install GoReleaser locally
	@command -v goreleaser >/dev/null 2>&1 || { \
		echo "GoReleaser is not installed. Installing..."; \
		brew install goreleaser || go install github.com/goreleaser/goreleaser@latest; \
	}
	@echo "GoReleaser installed successfully."

.PHONY: test-release
test-release: install-goreleaser ## Run a test release with goreleaser
	goreleaser release --snapshot --clean --skip=publish

.PHONY: release
release: install-goreleaser ## Run a production release with goreleaser (requires GITHUB_TOKEN)
	[ -n "$$GITHUB_TOKEN" ] || { echo "GITHUB_TOKEN is required for releasing. Please set it and try again."; exit 1; }
	goreleaser release --clean

.PHONY: ghcr-login
ghcr-login: ## Log in to GitHub Container Registry (requires GITHUB_TOKEN)
	@echo "Logging in to GitHub Container Registry..."
	@[ -n "$$GITHUB_TOKEN" ] || { echo "GITHUB_TOKEN is required. Please set it and try again."; exit 1; }
	echo "$$GITHUB_TOKEN" | docker login ghcr.io -u "$$USER" --password-stdin

##@ Helm Chart


.PHONY: helm-docs
helm-docs: ## Generate documentation for Helm chart
	@echo "Generating documentation for Helm chart..."
	@if ! command -v helm-docs > /dev/null 2>&1; then \
		echo "helm-docs is not installed. Installing..."; \
		go install github.com/norwoodj/helm-docs/cmd/helm-docs@latest; \
	fi
	@helm-docs --chart-search-root=charts/

.PHONY: helm-lint
helm-lint: ## Lint Helm chart
	@echo "Linting Helm chart..."
	@if ! command -v helm > /dev/null 2>&1; then \
		echo "helm is not installed. Please install helm first."; \
		exit 1; \
	fi
	helm lint charts/pac-quota-controller

.PHONY: helm-package
helm-package: helm-docs helm-lint ## Package Helm chart
	@echo "Packaging Helm chart..."
	@mkdir -p dist/chart
	helm package charts/pac-quota-controller -d dist/chart

.PHONY: helm-deploy
helm-deploy: helm-lint ## Deploy the Helm chart
	@echo "Deploying Helm chart $(HELM_RELEASE_NAME) to namespace $(HELM_NAMESPACE)..."
	helm upgrade --install $(HELM_RELEASE_NAME) ./charts/pac-quota-controller \
		--namespace $(HELM_NAMESPACE) \
		--create-namespace \
		--set controllerManager.container.image.repository=$(shell echo $(IMG) | cut -d: -f1) \
		--set controllerManager.container.image.tag=$(shell echo $(IMG) | cut -d: -f2) \
		--set controllerManager.container.image.pullPolicy=Never \
		--wait --timeout 10m0s
	@echo "Helm chart deployed."

.PHONY: helm-uninstall
helm-uninstall: ## Uninstall the Helm release
	@echo "Uninstalling Helm release..."
	helm uninstall $(HELM_RELEASE_NAME) --namespace $(HELM_NAMESPACE) || echo "Helm release not found or uninstall failed."
	@echo "Helm release uninstalled."

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) || true ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $(1)-$(3) $(1)
endef
