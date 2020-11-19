#!/bin/bash
set -x

ARGOCD_NS="argocd"

# Installing ArgoCD operator in argocd namespace

oc new-project $ARGOCD_NS

oc create -f - <<EOF
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: argocd-group
  namespace: argocd
spec:
  targetNamespaces:
  - argocd
EOF

oc create -f - <<EOF
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata: 
  labels: 
    operators.coreos.com/argocd-operator.argocd: ""
  name: argocd-subscription
  namespace: argocd
spec: 
  channel: alpha
  name: argocd-operator
  source: community-operators
  sourceNamespace: openshift-marketplace
EOF

waitUntilCommandSucceeds(){
  count=0
  while [ "$count" -lt 5 ];
  do
    eval $1
    if [ "$?" -eq "0" ]; then
      break
    else
      count=$(( count + 1 ))
      sleep 10
    fi
  done

  if [ "$count" -ge 5 ]; then
    echo "Command failed"
    exit
  fi
}

# Wait until ArgoCD CRD is available
command="oc get crd/argocds.argoproj.io"
waitUntilCommandSucceeds "$command"

echo "Creating ArgoCD Instance"
oc create -f - <<EOF
apiVersion: argoproj.io/v1alpha1
kind: ArgoCD
metadata:
  creationTimestamp: null
  name: argocd
  namespace: argocd
spec:
  server:
    route:
      enabled: true
EOF

# Wait unitl ArgoCD server route is available
command="oc get route argocd-server -n argocd"
waitUntilCommandSucceeds "$command"

echo "Successfully installed ArgoCD operator"
