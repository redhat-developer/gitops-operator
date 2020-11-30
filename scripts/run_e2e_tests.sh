#!/bin/bash

set -x

E2E_TEST_NS=gitops-test
E2E_TEST_DIR=./test/e2e


echo "Checking if operator-sdk is installed"
if ! command -v operator-sdk &> /dev/null
then
    echo "Unable to find operator-sdk"
    exit
fi

operator_sdk_version=$(operator-sdk version | grep -Po '[0-9][^.]+' | head -1)
if [ $operator_sdk_version -gt "17" ]; then
    echo "Install operator-sdk with version less than 0.18.0"
    exit
fi

# Install ArgoCD operator
sh ./scripts/install_argocd.sh

# Create a new namespace for e2e tests
oc new-project $E2E_TEST_NS

echo "Running e2e tests"
operator-sdk test local $E2E_TEST_DIR --operator-namespace $E2E_TEST_NS --watch-namespace "" --up-local

echo "Cleaning e2e test resources"
oc delete project $E2E_TEST_NS
oc delete project argocd
