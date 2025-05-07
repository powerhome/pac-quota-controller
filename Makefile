.PHONY: build test clean lint run docker-build docker-run kind-create kind-delete kind-load kind-deploy cleanup install-cert-manager verify-webhook

# Variables
BINARY_NAME=pac-quota-controller
DOCKER_IMAGE=powerhouse/$(BINARY_NAME)
VERSION=$(shell git describe --tags --always --dirty)
GO=go
DOCKER=docker
KIND=kind
KUBECTL=kubectl
KIND_CLUSTER_NAME=pac-webhook
NAMESPACE=pac-system
CERT_MANAGER_VERSION=v1.14.4

# Build flags
LDFLAGS=-ldflags "-X main.version=$(VERSION)"

# Default target
all: build

# Build the application
build:
	$(GO) build $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/$(BINARY_NAME)

# Run tests
test:
	$(GO) test -v -race -coverprofile=coverage.txt -covermode=atomic ./...

# Clean build artifacts
clean:
	@echo "Cleaning up build artifacts..."
	@rm -rf bin/ || true
	@rm -f coverage.txt || true
	@echo "Cleaning up Docker resources..."
	@$(DOCKER) ps -a --filter "name=$(BINARY_NAME)" -q | xargs -r $(DOCKER) rm -f || true
	@$(DOCKER) images "$(DOCKER_IMAGE):*" -q | xargs -r $(DOCKER) rmi -f || true
	@echo "Cleaning up kind cluster..."
	@if $(KIND) get clusters | grep -q $(KIND_CLUSTER_NAME); then \
		echo "Deleting kind cluster $(KIND_CLUSTER_NAME)..."; \
		$(KIND) delete cluster --name $(KIND_CLUSTER_NAME); \
	else \
		echo "Kind cluster $(KIND_CLUSTER_NAME) does not exist."; \
	fi
	@echo "Cleanup complete!"

# Run linter
lint:
	golangci-lint run

# Run the application
run:
	$(GO) run $(LDFLAGS) ./cmd/$(BINARY_NAME)

# Build Docker image
docker-build:
	$(DOCKER) build -t $(DOCKER_IMAGE):$(VERSION) .

# Run Docker container
docker-run:
	$(DOCKER) run -p 8080:8080 $(DOCKER_IMAGE):$(VERSION)

# Create kind cluster
kind-create:
	$(KIND) create cluster --name $(KIND_CLUSTER_NAME)

# Delete kind cluster
kind-delete:
	@if $(KIND) get clusters | grep -q $(KIND_CLUSTER_NAME); then \
		echo "Deleting kind cluster $(KIND_CLUSTER_NAME)..."; \
		$(KIND) delete cluster --name $(KIND_CLUSTER_NAME); \
	else \
		echo "Kind cluster $(KIND_CLUSTER_NAME) does not exist."; \
	fi

# Load Docker image into kind cluster
kind-load:
	@if $(KIND) get clusters | grep -q $(KIND_CLUSTER_NAME); then \
		echo "Loading Docker image into kind cluster..."; \
		$(KIND) load docker-image $(DOCKER_IMAGE):$(VERSION) --name $(KIND_CLUSTER_NAME); \
	else \
		echo "Kind cluster $(KIND_CLUSTER_NAME) does not exist. Please create it first with 'make kind-create'."; \
		exit 1; \
	fi

# Install cert-manager
install-cert-manager:
	@echo "Checking if cert-manager is already installed..."
	@if $(KUBECTL) get namespace cert-manager > /dev/null 2>&1; then \
		echo "cert-manager namespace already exists, checking deployments..."; \
		if $(KUBECTL) get deployment cert-manager-webhook -n cert-manager > /dev/null 2>&1 && \
		   $(KUBECTL) get deployment cert-manager-cainjector -n cert-manager > /dev/null 2>&1 && \
		   $(KUBECTL) get deployment cert-manager -n cert-manager > /dev/null 2>&1; then \
			echo "cert-manager components already deployed, skipping installation"; \
		else \
			echo "cert-manager namespace exists but deployments are incomplete, reinstalling..."; \
			$(KUBECTL) apply -f https://github.com/cert-manager/cert-manager/releases/download/$(CERT_MANAGER_VERSION)/cert-manager.yaml; \
			echo "Waiting for cert-manager to be ready..."; \
			$(KUBECTL) wait --for=condition=Available --timeout=300s deployment/cert-manager-webhook -n cert-manager || true; \
			$(KUBECTL) wait --for=condition=Available --timeout=300s deployment/cert-manager-cainjector -n cert-manager || true; \
			$(KUBECTL) wait --for=condition=Available --timeout=300s deployment/cert-manager -n cert-manager || true; \
		fi; \
	else \
		echo "Installing cert-manager..."; \
		$(KUBECTL) apply -f https://github.com/cert-manager/cert-manager/releases/download/$(CERT_MANAGER_VERSION)/cert-manager.yaml; \
		echo "Waiting for cert-manager to be ready..."; \
		$(KUBECTL) wait --for=condition=Available --timeout=300s deployment/cert-manager-webhook -n cert-manager || true; \
		$(KUBECTL) wait --for=condition=Available --timeout=300s deployment/cert-manager-cainjector -n cert-manager || true; \
		$(KUBECTL) wait --for=condition=Available --timeout=300s deployment/cert-manager -n cert-manager || true; \
	fi
	@echo "cert-manager check completed"

# Deploy to Kubernetes with Helm
kind-deploy: kind-load install-cert-manager
	@echo "Deploying webhook to kind cluster..."
	@kubectl create namespace $(NAMESPACE) || true
	@helm upgrade --install $(BINARY_NAME) charts/$(BINARY_NAME) \
		--namespace $(NAMESPACE) \
		--set image.repository=powerhouse/$(BINARY_NAME) \
		--set image.tag=$(shell git rev-parse --short HEAD)-dirty \
		--set image.pullPolicy=IfNotPresent
	@echo "Deployment complete. Webhook is using cert-manager for TLS certificates."

# Verify webhook deployment and communication
verify-webhook:
	@echo "Verifying webhook service..."
	@$(KUBECTL) -n $(NAMESPACE) get svc $(BINARY_NAME)
	@$(KUBECTL) -n $(NAMESPACE) get pods -l app.kubernetes.io/name=$(BINARY_NAME)
	@$(KUBECTL) -n $(NAMESPACE) get endpoints $(BINARY_NAME)
	@echo "Checking certificate resources..."
	@$(KUBECTL) -n $(NAMESPACE) get certificate $(BINARY_NAME)-tls
	@$(KUBECTL) -n $(NAMESPACE) get issuer $(BINARY_NAME)-selfsigned
	@$(KUBECTL) -n $(NAMESPACE) get secret $(BINARY_NAME)-tls

# Install dependencies
deps:
	$(GO) mod tidy

# Generate documentation
docs:
	$(GO) doc -all ./...

# Help
help:
	@echo "Available targets:"
	@echo "  all           - Build the application (default)"
	@echo "  build         - Build the application"
	@echo "  test          - Run tests"
	@echo "  clean         - Clean build artifacts"
	@echo "  lint          - Run linter"
	@echo "  run           - Run the application"
	@echo "  docker-build  - Build Docker image"
	@echo "  docker-run    - Run Docker container"
	@echo "  kind-create   - Create kind cluster"
	@echo "  kind-delete   - Delete kind cluster"
	@echo "  kind-load     - Load Docker image into kind cluster"
	@echo "  install-cert-manager - Install cert-manager in the cluster"
	@echo "  kind-deploy   - Deploy to Kubernetes"
	@echo "  verify-webhook - Verify webhook deployment and communication"
	@echo "  deps          - Install all dependencies"
	@echo "  docs          - Generate documentation"
	@echo "  help          - Show this help message"
