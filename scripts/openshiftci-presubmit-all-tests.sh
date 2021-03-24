#!/bin/sh

# fail if some commands fails
set -e

# Do not show token in CI log
set +x

# show commands
set -x
export CI="prow"
go mod vendor
make prepare-test-cluster

export PATH="$PATH:$(pwd)"

INSTALL_OPERATOR_SDK="./scripts/install-operator-sdk.sh"
sh $INSTALL_OPERATOR_SDK

# Copy kubeconfig to temporary kubeconfig file and grant
# read and Write permission to temporary kubeconfig file
TMP_DIR=$(mktemp -d)
cp $KUBECONFIG $TMP_DIR/kubeconfig
chmod 640 $TMP_DIR/kubeconfig
export KUBECONFIG=$TMP_DIR/kubeconfig

# Run e2e test
make test-e2e
