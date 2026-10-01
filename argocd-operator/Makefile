# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 0.20.0

# ARGO_CD_TARGET_VERSION is the target version that argocd-operator will install.
# Update this when you upgrade the Argo CD dependencies of the project.
# After updating, call 'make update-dependencies'.
# Notes:
# - String should NOT begin with 'v' prefix, e.g. 'v3.1.1'
ARGO_CD_TARGET_VERSION ?= 3.5.2

# Try to detect Docker or Podman
CONTAINER_RUNTIME := $(shell command -v docker 2> /dev/null || command -v podman 2> /dev/null)

# If neither Docker nor Podman is found, print an error message and exit
ifeq ($(CONTAINER_RUNTIME),)
$(warning "No container runtime (Docker or Podman) found in PATH. Please install one of them.")
endif

GOSEC_VERSION ?= v2.22.7
GOLANGCILINT_VERSION ?= v2.12.2


# IMAGE_TAG_BASE defines the docker.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# /argocd-operator-bundle:$VERSION and /argocd-operator-catalog:$VERSION.
IMAGE_TAG_BASE ?= quay.io/argoprojlabs/argocd-operator

# Image URL to use all building/pushing image targets
IMG ?= $(IMAGE_TAG_BASE):v$(VERSION)

LD_FLAGS = "-X github.com/argoproj-labs/argocd-operator/version.Version=$(VERSION)"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

fmt: ## Run go fmt against code.
	go fmt ./...

vet: ## Run go vet against code.
	go vet ./...

get-image-updater-crd: ## Download Image Updater CRD.
	@echo "downloading image updater crd"
	@curl -sSLo config/crd/bases/argocd-image-updater.argoproj.io_imageupdaters.yaml https://raw.githubusercontent.com/argoproj-labs/argocd-image-updater/master/config/crd/bases/argocd-image-updater.argoproj.io_imageupdaters.yaml


# Exclude E2E tests from the list of unit test packages
UNIT_TEST_PACKAGES := $(shell go list ./... | grep -E -v '/tests/ginkgo')

test: fmt vet envtest ## Run tests.
	REDIS_CONFIG_PATH="$(shell pwd)/build/redis" go test $(UNIT_TEST_PACKAGES) -coverprofile cover.out

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

##@ Deployment

list-crds: ## List all CRDs in the cluster
	@echo "=== Installed CRDs ==="
	@kubectl get crds --sort-by=.metadata.name
	@echo ""
	@echo "=== ArgoCD Operator CRDs ==="
	@kubectl get crds | grep argoproj.io || echo "None found"
	@echo ""
	@echo "=== Prometheus Operator CRDs ==="
	@kubectl get crds | grep monitoring.coreos.com || echo "None found"
	@echo ""
	@echo "=== OpenShift Route CRDs ==="
	@kubectl get crds | grep route.openshift.io || echo "None found"

PROMETHEUS_OPERATOR_VERSION ?= v0.73.2
install-prometheus-crds: ## Install Prometheus Operator CRDs if not already installed
	@echo "Checking for Prometheus Operator CRDs..."
	@if kubectl get crd prometheuses.monitoring.coreos.com >/dev/null 2>&1 && \
	   kubectl get crd servicemonitors.monitoring.coreos.com >/dev/null 2>&1 && \
	   kubectl get crd prometheusrules.monitoring.coreos.com >/dev/null 2>&1; then \
		echo "All Prometheus Operator CRDs already installed, skipping installation"; \
	else \
		echo "Installing Prometheus Operator CRDs $(PROMETHEUS_OPERATOR_VERSION)..."; \
		kubectl apply --server-side -f https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/$(PROMETHEUS_OPERATOR_VERSION)/example/prometheus-operator-crd/monitoring.coreos.com_prometheuses.yaml; \
		kubectl apply --server-side -f https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/$(PROMETHEUS_OPERATOR_VERSION)/example/prometheus-operator-crd/monitoring.coreos.com_servicemonitors.yaml; \
		kubectl apply --server-side -f https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/$(PROMETHEUS_OPERATOR_VERSION)/example/prometheus-operator-crd/monitoring.coreos.com_prometheusrules.yaml; \
		echo "Prometheus Operator CRDs installed successfully"; \
	fi

##@ E2E


start-e2e: install-prometheus-crds ## Start operator for E2E tests (installs required CRDs if needed)
	ARGOCD_CLUSTER_CONFIG_NAMESPACES="argocd-e2e-cluster-config, argocd-test-impersonation-1-046, argocd-agent-principal-1-051, argocd-agent-agent-1-052, appset-argocd, appset-old-ns, appset-new-ns, appset-argocd-clusterrole, ns-hosting-principal, ns-hosting-managed-agent, ns-hosting-autonomous-agent, gitops-promoter-1-134" make run

CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.21.0)

KUSTOMIZE = $(shell pwd)/bin/kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5@v5.8.1)

ENVTEST = $(shell pwd)/bin/setup-envtest
envtest: ## Download envtest-setup locally if necessary.
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest@release-0.24)
	$(ENVTEST) use 1.26

# go-install-tool will 'go install' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-install-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin go install $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef

# UTIL_IMG defines the image:tag used for the utility image (for backup).
# You can use it as an arg. (E.g make util-build UTIL_IMG=<some-registry>/<project-name-bundle>:<tag>)
UTIL_IMG ?= $(IMAGE_TAG_BASE)-util:v$(VERSION)

.PHONY: util-build
util-build: ## Build the util container image (for backup)
	$(CONTAINER_RUNTIME) build --no-cache -t $(UTIL_IMG) build/util

.PHONY: util-push
util-push: ## Push the util container image
	$(MAKE) docker-push IMG=$(UTIL_IMG)

.PHONY: opm
OPM = ./bin/opm
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.23.0/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

.PHONY: e2e-tests-sequential-ginkgo
e2e-tests-sequential-ginkgo: ginkgo
	@echo "Running operator sequential Ginkgo E2E tests..."
	$(GINKGO_CLI) -v --trace --timeout 110m -r ./tests/ginkgo/sequential

.PHONY: e2e-tests-parallel-ginkgo
e2e-tests-parallel-ginkgo: ginkgo
	@echo "Running operator parallel Ginkgo E2E tests..."
	$(GINKGO_CLI) -p -v -procs=3 --trace --timeout 120m -r ./tests/ginkgo/parallel


GINKGO_CLI = $(shell pwd)/bin/ginkgo
GINKGO_MOD_VERSION = $(shell go list -m -f '{{.Version}}' github.com/onsi/ginkgo/v2)
.PHONY: ginkgo
ginkgo: ## Download ginkgo locally if necessary.
	$(call go-install-tool,$(GINKGO_CLI),github.com/onsi/ginkgo/v2/ginkgo@$(GINKGO_MOD_VERSION))

# Updates upstream dependencies throughout the repository
.PHONY: update-dependencies
update-dependencies:
	hack/update-dependencies-script/run.sh

# Updates only Argo CD dependencies
.PHONY: update-dependencies-argocd
update-dependencies-argocd:
	hack/update-dependencies-script/argocd/run.sh

# Updates only Argo CD dependencies
.PHONY: update-dependencies-gitops-promoter
update-dependencies-gitops-promoter:
	hack/update-dependencies-script/gitops-promoter/run.sh

