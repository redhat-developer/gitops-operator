#!/bin/sh

# fail if some commands fails
set -e

# Do not show token in CI log
set +x
export QUAY_CREDENTIAL=`cat $QUAY_CREDENTIAL`


# show commands
set -x
export CI="prow"
go mod vendor
# make prepare-test-cluster

# source $(dirname $0)/e2e-common.sh

# Script entry point.
TARGET=${TARGET:-openshift}
KUBECONFIG=${KUBECONFIG:-$HOME/.kube/config}
# By default we disable uninstall, so you can comment that out if you run locally so it helps in cleanup
E2E_SKIP_UNINSTALL=true
# E2E_SKIP_BUILD_TOOL_INSTALLATION=true # This flag helps to skip build tool installation on your local system
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

uninstall_operator() {
    header "Uninstalling operator resources"
    uninstall_operator_resources

    if [ -d "$TMP_DIR/catalog-source.yaml" ]; then
       echo "Cleaning catalog source"
       kubectl delete --ignore-not-found=true -f $TMP_DIR/catalog-source.yaml
    fi
    
    echo -e "Enabling default catalog sources"
    kubectl patch operatorhub.config.openshift.io/cluster -p='{"spec":{"disableAllDefaultSources":false}}' --type=merge
}
[[ -z ${E2E_SKIP_UNINSTALL} ]] && trap uninstall_operator EXIT

docker login quay.io -u redhat-developer -p QUAY_CREDENTIAL

oc get catalogsources -A
oc projects | grep openshift-gitops
oc get subscription openshift-gitops-operator -n openshift-operators
oc get pods -n openshift-gitops

docker logout quay.io

echo ">> Running tests on ${TARGET}"

# header "Building and pushing catalog image"
# build_and_push_catalog_image

# header "Setting up environment"
# [[ -z ${E2E_SKIP_OPERATOR_INSTALLATION} ]] && configure_operator

# header "Install gitops operator"
# [[ -z ${E2E_SKIP_OPERATOR_INSTALLATION} ]] && install_operator_resources

header "Running kuttl e2e tests"
make kuttl-e2e || fail_test "Kuttl tests failed"

success