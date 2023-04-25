#!/usr/bin/env bash

set -ex

export CI="prow"
go mod vendor

source $(dirname $0)/e2e-common.sh

# Script entry point.
KUBECONFIG=${KUBECONFIG:-$HOME/.kube/config}

# Copy kubeconfig to temporary kubeconfig file and grant read and Write permission to temporary kubeconfig file
TMP_DIR=$(mktemp -d)
cp $KUBECONFIG $TMP_DIR/kubeconfig
chmod 640 $TMP_DIR/kubeconfig
export KUBECONFIG=$TMP_DIR/kubeconfig
cp $KUBECONFIG /go/src/github.com/redhat-developer/gitops-operator/kubeconfig

# Ensuring proper installation
pod=gitops-operator-controller-manager && oc get pods `oc get pods --all-namespaces | grep $pod | head -1 | awk '{print $2}'` -n openshift-operators -o yaml
subscription=gitops-operator- && oc get subscription `oc get subscription --all-namespaces | grep $subscription | head -1 | awk '{print $2}'` -n openshift-operators
oc wait --for=condition=Ready -n openshift-gitops pod --timeout=15m  -l 'app.kubernetes.io/name in (cluster,kam,openshift-gitops-application-controller,openshift-gitops-applicationset-controller,openshift-gitops-dex-server,openshift-gitops-redis,openshift-gitops-repo-server,openshift-gitops-server)' 

# Check argocd instance creation
oc create ns test-argocd
cat << EOF | oc apply -f -
apiVersion: argoproj.io/v1alpha1
kind: ArgoCD
metadata:
  name: argocd
  namespace: test-argocd
EOF

sleep 60s

oc get pods -n test-argocd

oc wait --for=condition=Ready -n test-argocd pod --timeout=15m  -l 'app.kubernetes.io/name in (argocd-application-controller,argocd-redis,argocd-repo-server,argocd-server)' 

echo ">> Running tests on ${CI}"
