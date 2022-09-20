#!/bin/sh

# fail if some commands fails
set -e

# Do not show token in CI log
set +x
#export QUAY_CREDENTIAL=`cat $QUAY_CREDENTIAL`


# show commands
set -x
export CI="prow"
go mod vendor
sh ./prepare-test-cluster

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

# install CRDs
# make install

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

# Checks to ensure the proper environment
oc get catalogsources -A
oc projects | grep openshift-gitops
oc get pods -n openshift-gitops || true
oc get subscription -A || true
kubectl-kuttl version || true
pod=gitops-operator-controller-manager && oc get pods `oc get pods --all-namespaces | grep $pod | head -1 | awk '{print $2}'` -n openshift-operators -o yaml || true

# Check argocd instance creation

oc create ns test-argocd

cat << EOF | oc apply -f -
apiVersion: argoproj.io/v1alpha1
kind: ArgoCD
metadata:
  name: argocd
  namespace: test-argocd
EOF

sleep 120

oc get pods -n test-argocd

echo ">> Running tests on ${TARGET}"

# header "Building and pushing catalog image"
# build_and_push_catalog_image

# header "Setting up environment"
# [[ -z ${E2E_SKIP_OPERATOR_INSTALLATION} ]] && configure_operator

# header "Install gitops operator"
# [[ -z ${E2E_SKIP_OPERATOR_INSTALLATION} ]] && install_operator_resources

# header "Running kuttl e2e tests"
make kuttl-e2e
