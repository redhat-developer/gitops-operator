#!/bin/bash

set -x

E2E_TEST_NS="gitops-test"
E2E_TEST_DIR=./test/e2e
ARGOCD_NS="openshift-gitops"
DEPRACATED_ARGOCD_NS="openshift-pipelines-app-delivery"
CONSOLE_LINK="argocd"

# Create a new namespace for e2e tests
#oc new-project $E2E_TEST_NS

# Point to the internal API server hostname
APISERVER=https://kubernetes.default.svc

# Path to ServiceAccount token
SERVICEACCOUNT=/var/run/secrets/kubernetes.io/serviceaccount

# Read this Pod's namespace
NAMESPACE=$(cat ${SERVICEACCOUNT}/namespace)

# Read the ServiceAccount bearer token
TOKEN=$(cat ${SERVICEACCOUNT}/token)

# Reference the internal certificate authority (CA)
CACERT=$(cat ${SERVICEACCOUNT}/ca.crt)

APISERVER=https://kubernetes.default.svc

echo "
apiVersion: v1
kind: Config
clusters:
- name: default-cluster
  cluster:
    certificate-authority-data: ${CACERT}
    server: ${APISERVER}
contexts:
- name: default-context
  context:
    cluster: default-cluster
    namespace: ${NAMESPACE}
    user: default-user
current-context: default-context
users:
- name: default-user
  user:
    token: ${TOKEN}
" > sa.kubeconfig



echo "Running e2e tests"
SKIP_OPERATOR_DEPLOYMENT=true operator-sdk test local $E2E_TEST_DIR --operator-namespace default --kubeconfig=sa.kubeconfig --verbose 

echo "Cleaning e2e test resources"
#oc delete project $E2E_TEST_NS