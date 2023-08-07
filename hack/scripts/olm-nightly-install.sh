#!/usr/bin/env bash

# To run the script , you have to export the IIB_ID like so
# export IIB_ID=581525

# There are 2 options '-i' and '-m' (for first install on the cluster or for migrating to nightly) that you have to provide
#./olm-install-script -i
#        or
#./olm-install-script -m

set -e

if [ ! ${IIB_ID} ] ;
then
    echo -e "\nPlease set the environment variable IIB_ID\n"   
    exit
fi


REGISTRY_NAME=${OPERATOR_REGISTRY:-"brew.registry.redhat.io/rh-osbs"}
REPO_NAME=${REPO_NAME:-"iib"}

INDEX="${REGISTRY_NAME}/${REPO_NAME}:$IIB_ID"

echo "INDEX IMAGE:- $INDEX"

#patch to disable default catalog source
oc patch operatorhub.config.openshift.io/cluster -p='{"spec":{"disableAllDefaultSources":true}}' --type=merge

# apply image content source policy

cat << EOF | oc apply -f - 
apiVersion: operator.openshift.io/v1alpha1
kind: ImageContentSourcePolicy   
metadata:  
    name: brew-registry  
spec: 
    repositoryDigestMirrors: 
    - mirrors: 
      - brew.registry.redhat.io 
      source: registry.redhat.io 
    - mirrors: 
      - brew.registry.redhat.io 
      source: registry.stage.redhat.io 
    - mirrors: 
      - brew.registry.redhat.io 
      source: registry-proxy.engineering.redhat.com
EOF

# apply catalog source
cat << EOF | oc apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: redhat-operators
  namespace: openshift-marketplace
spec:
  displayName: ''
  image: ${INDEX}
  publisher: ''
  sourceType: grpc
EOF

# Wait for the Catalog source to be ready
i=0
until [ $(oc get catalogsource -n openshift-marketplace -o jsonpath="{.items[0].status.connectionState.lastObservedState}") = "READY" ]
do
  echo "Waiting for the catalog source to be in READY state"
  i=`expr $i + 1`
  sleep 10
  if [[ $i -eq 20 ]];
  then
    echo "Catalog source not READY"
    oc patch operatorhub.config.openshift.io/cluster -p='{"spec":{"disableAllDefaultSources":false}}' --type=merge
    exit
  fi
done

# install nightly operator on fresh cluster

function apply_subscription() { 

create namespace
oc create ns openshift-gitops-operator

create OperatorGroup
cat << EOF | oc apply -f -
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: gitops-operator-group
  namespace: openshift-gitops-operator
spec: {}
EOF

#create Subscription
cat << EOF | oc apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: openshift-gitops-operator
  namespace: openshift-gitops-operator
spec:
  channel: nightly
  installPlanApproval: Automatic
  name: openshift-gitops-operator
  source: redhat-operators
  sourceNamespace: openshift-marketplace
EOF

}

key=$1

  case $key in
    -i)
       echo "Installing Nightly Operator"
       apply_subscription   
       ;;
    -m)

      echo "Migrating to Nightly Operator"

      #patch the channel to nightly for automatic upgrade 
      oc patch subscription openshift-gitops-operator -n openshift-gitops-operator --type='merge' -p='{"spec":{"channel": "nightly"}}'
      ;;
    *)
      echo "[ERROR] Invalid argument $key"
      exit 1
      ;;
  esac

# Wait for the operator to upgrade
NEW_VER="99.9.0"
NEW_BUILD="openshift-gitops-operator.v$NEW_VER"
until [[ $(oc get csv -n openshift-operators -o name) == *"$NEW_BUILD"* ]];
do
  echo "Operator upgrading..."
  sleep 10
  if [[ $(oc get csv -n openshift-operators -o name) == *"$NEW_BUILD"* ]]; then
     break
  fi
done
echo -e "\nOperator upgraded, Waiting for the pods to come up"

# Establish some time for the pods to refresh
sleep 30

# Wait for the deployments to be successfully rolled out
deployments=($(echo $(oc get deployments -n openshift-gitops --no-headers -o custom-columns=':metadata.name')))
for deployment in "${deployments[@]}"; do
  oc rollout status deployment/"${deployment}" -n openshift-gitops --timeout=60s
done

echo -e "\nProvide cluster-admin access to argocd-application-controller service account"
oc adm policy add-cluster-role-to-user cluster-admin system:serviceaccount:openshift-gitops:openshift-gitops-argocd-application-controller

# Re-enable the default Catalog Sources after the upgrade testing
oc patch operatorhub.config.openshift.io/cluster -p='{"spec":{"disableAllDefaultSources":false}}' --type=merge