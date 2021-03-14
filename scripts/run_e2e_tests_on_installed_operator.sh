#!/bin/bash

set -x


XDG_CACHE_HOME=/tmp/output/.cache
E2E_TEST_DIR=./test/e2e
ARGOCD_NS="openshift-gitops"
DEPRACATED_ARGOCD_NS="openshift-pipelines-app-delivery"
CONSOLE_LINK="argocd"

APISERVER=https://kubernetes.default.svc

# Path to ServiceAccount token
SERVICEACCOUNT=/var/run/secrets/kubernetes.io/serviceaccount

# Read this Pod's namespace
NAMESPACE=$(cat ${SERVICEACCOUNT}/namespace)

# Read the ServiceAccount bearer token
TOKEN=$(cat ${SERVICEACCOUNT}/token)


echo "
apiVersion: v1
kind: Config
clusters:
- name: default-cluster
  cluster:
    insecure-skip-tls-verify: true
    server: ${APISERVER}
contexts:
- name: default-context
  context:
    cluster: default-cluster
    namespace: ${NAMESPACE}
    user: admin
current-context: default-context
users:
- name: admin
  user:
    token: ${TOKEN}
" > /tmp/output/sa.kubeconfig



echo "Running e2e tests"
CGO_ENABLED=0 SKIP_OPERATOR_DEPLOYMENT=true operator-sdk test local $E2E_TEST_DIR  --kubeconfig=/tmp/output/sa.kubeconfig --verbose --no-setup