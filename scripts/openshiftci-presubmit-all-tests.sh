#!/usr/bin/env bash

# fail if some commands fails
set -e

# Do not show token in CI log
set +x

# show commands
set -x
export CI="prow"
go mod vendor
# make prepare-test-cluster

export PATH="$PATH:$(pwd)"

# Copy kubeconfig to temporary kubeconfig file and grant
# read and Write permission to temporary kubeconfig file
TMP_DIR=$(mktemp -d)
cp $KUBECONFIG $TMP_DIR/kubeconfig
chmod 640 $TMP_DIR/kubeconfig
export KUBECONFIG=$TMP_DIR/kubeconfig

# without vendoring upgrade-rollouts-manager, make manifests runs into an error
cd hack/upgrade-rollouts-manager
go mod vendor
cd ../..

# Run e2e test
make test-e2e

