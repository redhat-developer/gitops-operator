#!/bin/bash

# The goal of this script is to run the Argo Rollouts operator tests from the argo-rollouts-manager repo against gitops-operator:
# - Runs the (cluster-scoped) E2E tests of the Argo Rollouts operator
# - Runs the upstream E2E tests from the argo-rollouts repo

set -ex

ROLLOUTS_TMP_DIR=$(mktemp -d)

cd $ROLLOUTS_TMP_DIR

git clone https://github.com/argoproj-labs/argo-rollouts-manager

cd "$ROLLOUTS_TMP_DIR/argo-rollouts-manager"

# This commit value will be automatically updated by calling 'hack/upgrade-rollouts-manager/go-run.sh':
# - It should always point to the same argo-rollouts-manager commit that is referenced in go.mod of gitops-operator (which will usually be the most recent argo-rollouts-manager commit)
TARGET_ROLLOUT_MANAGER_COMMIT=9f79ece2e923cbf03fe041bb6d1d83aae16a08da

# This commit value will be automatically updated by calling 'hack/upgrade-rollouts-manager/go-run.sh':
# - It should always point to the same argo-rollouts-manager commit that is referenced in the version of argo-rollouts-manager that is in go.mod
TARGET_OPENSHIFT_ROUTE_ROLLOUT_PLUGIN_COMMIT=8b4125a7f9ecffb0247df91a4c890f88c0c523b7

git checkout $TARGET_ROLLOUT_MANAGER_COMMIT

# 1) Run E2E tests from argo-rollouts-manager repo

make test-e2e

# Clean up old namespaces created by test
# NOTE: remove this once this is handled by 'make test-e2e' in argo-rollouts-manager repo
kubectl delete rolloutmanagers --all -n test-rom-ns-1 || true

cd "$ROLLOUTS_TMP_DIR/argo-rollouts-manager"


# 2) Run E2E tests from argoproj/argo-rollouts repo

SKIP_RUN_STEP=true hack/run-upstream-argo-rollouts-e2e-tests.sh

# 3) Run rollouts-plugin-trafficrouter-openshift E2E tests

kubectl delete ns argo-rollouts || true

kubectl wait --timeout=5m --for=delete namespace/argo-rollouts

kubectl create ns argo-rollouts
kubectl config set-context --current --namespace=argo-rollouts

cat << EOF > "$ROLLOUTS_TMP_DIR/rollout-manager.yaml"
apiVersion: argoproj.io/v1alpha1
kind: RolloutManager
metadata:
  name: argo-rollout
  namespace: argo-rollouts
spec: {}
EOF

kubectl apply -f "$ROLLOUTS_TMP_DIR/rollout-manager.yaml"

cd "$ROLLOUTS_TMP_DIR"
git clone https://github.com/argoproj-labs/rollouts-plugin-trafficrouter-openshift

cd "$ROLLOUTS_TMP_DIR/rollouts-plugin-trafficrouter-openshift"

git checkout $TARGET_OPENSHIFT_ROUTE_ROLLOUT_PLUGIN_COMMIT

make test-e2e




