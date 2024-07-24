#!/bin/bash

# The goal of this script is to run the Argo Rollouts operator tests from the argo-rollouts-manager repo against gitops-operator:
# - Runs the (cluster-scoped) E2E tests of the Argo Rollouts operator
# - Runs the upstream E2E tests from the argo-rollouts repo

set -e

ROLLOUTS_TMP_DIR=$(mktemp -d)

cd $ROLLOUTS_TMP_DIR

git clone https://github.com/argoproj-labs/argo-rollouts-manager

cd "$ROLLOUTS_TMP_DIR/argo-rollouts-manager"

# This commit value will be automatically updated by calling 'hack/upgrade-rollouts-manager/go-run.sh':
# - It should always point to the same argo-rollouts-manager commit that is referenced in go.mod of gitops-operator (which will usually be the most recent argo-rollouts-manager commit)
TARGET_ROLLOUT_MANAGER_COMMIT=192dd2c3b5dd026e2c59c5765e98ca2f70ca01f9

git checkout $TARGET_ROLLOUT_MANAGER_COMMIT

# 1) Run E2E tests from argo-rollouts-manager repo

make test-e2e

# Clean up old namespaces created by test
# NOTE: remove this once this is handled by 'make test-e2e' in argo-rollouts-manager repo
kubectl delete rolloutmanagers --all -n test-rom-ns-1 || true

cd "$ROLLOUTS_TMP_DIR/argo-rollouts-manager"


# 2) Run E2E tests from argoproj/argo-rollouts repo

SKIP_RUN_STEP=true hack/run-upstream-argo-rollouts-e2e-tests.sh

