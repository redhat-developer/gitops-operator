#!/usr/bin/env bash

# fail if some commands fails
set -e

# Do not show token in CI log
set +x

# Get path containing the current script, usually (repo path)/scripts
SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )


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

# Run e2e test
make test-e2e

# Run Rollouts E2E tests
cd "$SCRIPT_DIR"

# "$SCRIPT_DIR/run-rollouts-e2e-tests.sh"
