#!/bin/bash

set -x

E2E_TEST_NS="gitops-test"
E2E_TEST_DIR=./test/e2e
ARGOCD_NS="openshift-gitops"
DEPRACATED_ARGOCD_NS="openshift-pipelines-app-delivery"
CONSOLE_LINK="argocd"

# Create a new namespace for e2e tests
oc new-project $E2E_TEST_NS

echo "Running e2e tests"
SKIP_OPERATOR_DEPLOYMENT=true operator-sdk test local $E2E_TEST_DIR --operator-namespace $E2E_TEST_NS  --verbose 

echo "Cleaning e2e test resources"
oc delete project $E2E_TEST_NS