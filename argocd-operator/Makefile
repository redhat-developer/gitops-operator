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
ARGO_CD_TARGET_VERSION ?= 3.6.1

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
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

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

##@ Deployment

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

