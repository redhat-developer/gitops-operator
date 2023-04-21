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

source $(dirname $0)/e2e-common.sh

# Script entry point.
TARGET=${TARGET:-openshift}
KUBECONFIG=${KUBECONFIG:-$HOME/.kube/config}
# By default we disable uninstall, so you can comment that out if you run locally so it helps in cleanup
E2E_SKIP_UNINSTALL=${E2E_SKIP_UNINSTALL:-true}

# By default on CI operator we don't bulid & push operator bundle, as it is handled by CI.
E2E_SKIP_BUNDLE_BUILD=${E2E_SKIP_BUNDLE_BUILD:-true}

# By default on CI operator we install operator using catalog source.
E2E_SKIP_OPERATOR_INSTALLATION=${E2E_SKIP_OPERATOR_INSTALLATION:-false}

# By default on CI we don't ignore parallel tests but in case of any flaky it would easy to configure
IGNORE_PARALLEL_TESTS=${IGNORE_PARALLEL_TESTS:-false}

E2E_SKIP_BUILD_TOOL_INSTALLATION=${E2E_SKIP_BUILD_TOOL_INSTALLATION:-false} # This flag helps to skip build tool installation on your local system
IMAGE=${IMAGE:-"quay.io/redhat-developer/gitops-operator"}
VERSION=${VERSION:-"1.8.0"}
CATALOG_SOURCE=${CATALOG_SOURCE:-"openshift-gitops-operator"}
CHANNEL=${CHANNEL:-"latest"}

export PATH="$PATH:$(pwd)"

failed=0
timestamp=$(date "+%Y.%m.%d-%H.%M.%S")

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

if [ "$E2E_SKIP_UNINSTALL" = false ]; then
   trap uninstall_operator EXIT
fi

echo ">> Running tests on ${TARGET}"

if [ "$E2E_SKIP_BUNDLE_BUILD" = false ]; then
   header "Building and pushing catalog image"
   build_and_push_catalog_image
fi


if [ "$E2E_SKIP_OPERATOR_INSTALLATION" = false ]; then
   header "Setting up environment"
   configure_operator

   header "Install gitops operator"
   install_operator_resources
fi

header "Running kuttl e2e tests"
make e2e-tests-sequential || failed=1

if [[ "$IGNORE_PARALLEL_TESTS" = "false" ]]; then
   
   # Now we untaint master nodes to schedule workloads to master (Hack for HA tests)
   master_nodes=$(oc get node -l node-role.kubernetes.io/master --no-headers 2>/dev/null)
   while read node ; do
      node_to_taint=(`echo -n ${node} | cut -f1 -d' '`)
      oc adm taint nodes $node_to_taint node-role.kubernetes.io/master-
   done <<< $(echo "${master_nodes}")
   
   make e2e-tests-parallel || failed=1 
fi 

(( failed )) && dump_cluster_state
(( failed )) && fail_test "E2E tests failed"

success