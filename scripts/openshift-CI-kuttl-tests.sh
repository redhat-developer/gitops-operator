#!/usr/bin/env bash

set -ex

cleanup_on_failure() {
  local exit_code=$?
  if [ $exit_code -ne 0 ]; then
    echo ">> Script failed with exit code ${exit_code}, collecting debug logs..."
    echo ""
    echo "Pods in openshift-gitops-operator"
    oc get pods -n openshift-gitops-operator -o yaml
    echo ""
    echo "Operator pod log"
    oc logs deployment/openshift-gitops-operator-controller-manager -n openshift-gitops-operator
    echo ""
    echo "Events in openshift-gitops-operator"
    oc get events -n openshift-gitops-operator
    echo ""
    echo "ArgoCDs in test-argocd:"
    oc get argocds -n test-argocd -o yaml
    echo ""
    echo "Pods in test-argocd:"
    oc get pods -n test-argocd -o yaml
    echo ""
    echo "Events in test-argocd:"
    oc get events -n test-argocd
    echo ""
  fi
  exit $exit_code
}
trap cleanup_on_failure EXIT

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
pod=openshift-gitops-operator-controller-manager && oc get pods `oc get pods --all-namespaces | grep $pod | head -1 | awk '{print $2}'` -n openshift-gitops-operator -o yaml
subscription=gitops-operator- && oc get subscription `oc get subscription --all-namespaces | grep $subscription | head -1 | awk '{print $2}'` -n openshift-gitops-operator
oc wait --for=condition=Ready -n openshift-gitops pod --timeout=15m  -l 'app.kubernetes.io/name in (cluster,openshift-gitops-application-controller,openshift-gitops-applicationset-controller,openshift-gitops-dex-server,openshift-gitops-redis,openshift-gitops-repo-server,openshift-gitops-server)' 

# Check argocd instance creation
oc create ns test-argocd
cat << EOF | oc apply -f -
apiVersion: argoproj.io/v1alpha1
kind: ArgoCD
metadata:
  name: argocd
  namespace: test-argocd
EOF


EXPECTED_LABELS=("argocd-application-controller" "argocd-redis" "argocd-repo-server" "argocd-server")
TIMEOUT=900
INTERVAL=10
ELAPSED=0

echo ">> Waiting for all ${#EXPECTED_LABELS[@]} ArgoCD pods to exist in test-argocd..."
while true; do
  ALL_EXIST=true
  for label in "${EXPECTED_LABELS[@]}"; do
    if ! oc get pod -n test-argocd -l "app.kubernetes.io/name=${label}" --no-headers 2>/dev/null | grep -q .; then
      ALL_EXIST=false
      break
    fi
  done

  if $ALL_EXIST; then
    echo ">> All ${#EXPECTED_LABELS[@]} ArgoCD pods exist after ${ELAPSED}s."
    break
  fi

  if [ $ELAPSED -ge $TIMEOUT ]; then
    echo ">> Timed out after ${TIMEOUT}s waiting for ArgoCD pods to exist."
    oc get pods -n test-argocd
    exit 1
  fi

  sleep $INTERVAL
  ELAPSED=$((ELAPSED + INTERVAL))
done

oc get pods -n test-argocd

oc wait --for=condition=Ready -n test-argocd pod --timeout=15m \
  -l 'app.kubernetes.io/name in (argocd-application-controller,argocd-redis,argocd-repo-server,argocd-server)'

echo ">> Running tests on ${CI}"
