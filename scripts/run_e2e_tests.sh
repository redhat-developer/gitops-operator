#!/bin/bash

# show commands
set -x

E2E_TEST_NS="gitops-test"
E2E_TEST_DIR=./test/e2e
ARGOCD_NS="openshift-gitops"
OPERATOR_SDK="${OPERATOR_SDK:-operator-sdk}"

echo "Checking if operator-sdk is installed"
if ! command -v operator-sdk &> /dev/null
then
    echo "Unable to find operator-sdk"
    exit
fi

operator_sdk_version=$(${OPERATOR_SDK} version | awk '/operator-sdk version/ { print $3 }' | sed -re 's/\"v([0-9]+).*\",/\1/')
if [ $operator_sdk_version -gt "0" ]; then
    echo "Install operator-sdk with version less than 1.0"
    exit
fi

# Create a new namespace for e2e tests
oc new-project $E2E_TEST_NS

export ARGOCD_CLUSTER_CONFIG_NAMESPACES=openshift-gitops

echo "Running e2e tests"
${OPERATOR_SDK} test local $E2E_TEST_DIR --operator-namespace $E2E_TEST_NS --watch-namespace "" --up-local --verbose

Teststatus=$?

echo "Cleaning e2e test resources"
oc delete project $E2E_TEST_NS
oc delete project $ARGOCD_NS

if [ $Teststatus -ne "0" ]; then
    exit $Teststatus
fi
