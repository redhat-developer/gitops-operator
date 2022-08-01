#!/bin/sh

# fail if some commands fails
set -e

# Do not show token in CI log
set +x

# show commands
set -x
export CI="prow"
go mod vendor
# make prepare-test-cluster

source $(dirname $0)/e2e-common.sh

# Script entry point.
TARGET=${TARGET:-openshift}
IMAGE=${IMAGE:-"quay.io/redhat-developer/gitops-backend-operator"}
VERSION=${VERSION:-"0.0.3"}
CATALOG_SOURCE=${CATALOG_SOURCE:-"openshift-gitops-operator"}
CHANNEL=${CHANNEL:-"alpha"}

export PATH="$PATH:$(pwd)"


# Copy kubeconfig to temporary kubeconfig file and grant
# read and Write permission to temporary kubeconfig file
TMP_DIR=$(mktemp -d)
cp $KUBECONFIG $TMP_DIR/kubeconfig
chmod 640 $TMP_DIR/kubeconfig
export KUBECONFIG=$TMP_DIR/kubeconfig
KUBECONFIG_PARAM=${KUBECONFIG:+"--kubeconfig $KUBECONFIG"}

# make sure you export IMAGE and version so it builds and pushes code to right registry. 

unsubscribe_to_operator() {
    header "Uninstalling operator resources"
    uninstall_operator_resources

    echo "Cleaning custom catalo source"
    kubectl delete -f $TMP_DIR/catalog-source.yaml

    echo -e "Enabling default catalog sources"
    kubectl patch operatorhub.config.openshift.io/cluster -p='{"spec":{"disableAllDefaultSources":false}}' --type=merge
}
trap unsubscribe_to_operator EXIT


echo ">> Running tests on ${TARGET}"

header "Building and pushing catalog image"
build_and_push_catalog_image

header "Setting up environment"
[[ -z ${E2E_SKIP_OPERATOR_INSTALLATION} ]] && configure_operator

header "Install gitops operator"
[[ -z ${E2E_SKIP_OPERATOR_INSTALLATION} ]] && install_operator_resources

header "Running kuttl e2e tests"
make kuttl-e2e || fail_test "Kuttl tests failed"

success