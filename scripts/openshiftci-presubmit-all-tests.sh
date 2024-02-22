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

# INSTALL_OPERATOR_SDK="./scripts/install-operator-sdk.sh"
# sh $INSTALL_OPERATOR_SDK

# Copy kubeconfig to temporary kubeconfig file and grant
# read and Write permission to temporary kubeconfig file
TMP_DIR=$(mktemp -d)
cp $KUBECONFIG $TMP_DIR/kubeconfig
chmod 640 $TMP_DIR/kubeconfig
export KUBECONFIG=$TMP_DIR/kubeconfig

# Run e2e test

# source $(dirname $0)/e2e-common.sh

# # Ensuring proper installation
# pod=openshift-gitops-operator-controller-manager && oc get pods `oc get pods --all-namespaces | grep $pod | head -1 | awk '{print $2}'` -n openshift-gitops-operator -o yaml

# subscription=gitops-operator- && oc get subscription `oc get subscription --all-namespaces | grep $subscription | head -1 | awk '{print $2}'` -n openshift-gitops-operator

# oc wait --for=condition=Ready -n openshift-gitops pod --timeout=15m  -l 'app.kubernetes.io/name in (cluster,kam,openshift-gitops-application-controller,openshift-gitops-applicationset-controller,openshift-gitops-dex-server,openshift-gitops-redis,openshift-gitops-repo-server,openshift-gitops-server)' 





# ROLLOUTS_TMP_DIR=$(mktemp -d)

# cd $ROLLOUTS_TMP_DIR

# kubectl get namespaces

# kubectl get pods -A || true

# kubectl api-resources

# git clone https://github.com/argoproj-labs/argo-rollouts-manager

# cd "$ROLLOUTS_TMP_DIR/argo-rollouts-manager"

# TARGET_ROLLOUT_MANAGER_COMMIT=027faa92ffdbc43a02eca3982f020a8c391fd340

# git checkout $TARGET_ROLLOUT_MANAGER_COMMIT

# make install generate fmt vet
make test-e2e