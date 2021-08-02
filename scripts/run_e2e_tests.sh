#!/bin/bash

# Add dispose function, called on script end
function finish {
    echo "Cleaning e2e test resources"
    oc delete project $E2E_TEST_NS
    oc delete project $ARGOCD_NS
}
trap finish EXIT

# show commands
set -x

E2E_TEST_NS="gitops-test"
E2E_TEST_DIR=./test/e2e
NON_DEFAULT_E2E_TEST_DIR=./test/nondefaulte2e
ARGOCD_NS="openshift-gitops"

# Create a new namespace for e2e tests
oc new-project $E2E_TEST_NS

export ARGOCD_CLUSTER_CONFIG_NAMESPACES=openshift-gitops

echo "Running e2e tests"
go test $E2E_TEST_DIR -coverprofile cover.out -ginkgo.v
Teststatus=$?
if [ $Teststatus_e2e -ne "0" ]; then
    exit $Teststatus_e2e
fi

echo "Running e2e tests (DISABLE_DEFAULT_ARGOCD_INSTANCE=true)"
go test $NON_DEFAULT_E2E_TEST_DIR -coverprofile cover.out -ginkgo.v
Teststatus_e2e_nondefault=$?
if [ $Teststatus_e2e_nondefault -ne "0" ]; then
    exit $Teststatus_e2e_nondefault
fi
