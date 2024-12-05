#!/bin/bash

# The goal of this script is to run the Argo Rollouts operator tests from the argo-rollouts-manager repo against gitops-operator:
# - Runs the cluster-scoped/namespace-scoped E2E tests of the Argo Rollouts operator
# - Runs the upstream E2E tests from the argo-rollouts repo

set -ex

function wait_until_pods_running() {
  echo -n "Waiting until all pods in namespace $1 are up"

  # Wait for there to be only a single Pod line in 'oc get pods' (there should be no more 'terminating' pods, etc)
  timeout="true"
  for i in {1..30}; do
    local num_pods="$(oc get pods --no-headers -n $1 | grep openshift-gitops-operator-controller-manager | wc -l 2>/dev/null)"

    # Check the number of lines
    if [[ "$num_lines" == "1" ]]; then
      echo "Waiting for a single Pod entry in Namespace '$1': $num_pods"
      sleep 5
    else
      timeout="false"
      break
    fi
  done
  if [ "$timeout" == "true" ]; then
    echo -e "\n\nERROR: timeout waiting for expected number of pods"
    return 1
  fi

  for i in {1..150}; do # timeout after 5 minutes
    local pods="$(oc get pods --no-headers -n $1 | grep openshift-gitops-operator-controller-manager 2>/dev/null)"
    # write it to tempfile
    TempFile=$(mktemp)
    oc get pods --no-headers -n $1 2>/dev/null >$TempFile

    # All pods must be running
    local not_running=$(echo "${pods}" | grep -v Running | grep -v Completed | wc -l)
    if [[ -n "${pods}" && ${not_running} -eq 0 ]]; then
      local all_ready=1
      while read pod; do
        local status=($(echo ${pod} | cut -f2 -d' ' | tr '/' ' '))
        # All containers must be ready
        [[ -z ${status[0]} ]] && all_ready=0 && break
        [[ -z ${status[1]} ]] && all_ready=0 && break
        [[ ${status[0]} -lt 1 ]] && all_ready=0 && break
        [[ ${status[1]} -lt 1 ]] && all_ready=0 && break
        [[ ${status[0]} -ne ${status[1]} ]] && all_ready=0 && break
      done <${TempFile}
      if ((all_ready)); then
        echo -e "\nAll pods are up:\n${pods}"
        return 0
      fi
    fi
    echo -n "."
    sleep 2
  done
  echo -e "\n\nERROR: timeout waiting for pods to come up\n${pods}"
  return 1
}

function enable_rollouts_cluster_scoped_namespaces() {
  
  # This functions add this env var to operator:
  # - CLUSTER_SCOPED_ARGO_ROLLOUTS_NAMESPACES="argo-rollouts,test-rom-ns-1,rom-ns-1"

  if ! [ -z $NON_OLM ]; then
    oc set env deployment openshift-gitops-operator-controller-manager -n openshift-gitops-operator CLUSTER_SCOPED_ARGO_ROLLOUTS_NAMESPACES="argo-rollouts,test-rom-ns-1,rom-ns-1"
    
  elif [ -z $CI ]; then 

    oc patch -n openshift-gitops-operator subscription openshift-gitops-operator \
      --type merge --patch '{"spec": {"config": {"env": [{"name": "CLUSTER_SCOPED_ARGO_ROLLOUTS_NAMESPACES", "value": "argo-rollouts,test-rom-ns-1,rom-ns-1"}]}}}'

  else

    oc patch -n openshift-gitops-operator subscription `subscription=gitops-operator- && oc get subscription --all-namespaces | grep $subscription | head -1 | awk '{print $2}'` \
      --type merge --patch '{"spec": {"config": {"env": [{"name": "CLUSTER_SCOPED_ARGO_ROLLOUTS_NAMESPACES", "value": "argo-rollouts,test-rom-ns-1,rom-ns-1"}]}}}'
  fi

  # Loop to wait until CLUSTER_SCOPED_ARGO_ROLLOUTS_NAMESPACES is added to the OpenShift GitOps Operator Deployment
  for i in {1..30}; do
    if oc get deployment openshift-gitops-operator-controller-manager -n openshift-gitops-operator -o jsonpath='{.spec.template.spec.containers[0].env}' | grep -q '{"name":"CLUSTER_SCOPED_ARGO_ROLLOUTS_NAMESPACES","value":"argo-rollouts,test-rom-ns-1,rom-ns-1"}'; then
      echo "CLUSTER_SCOPED_ARGO_ROLLOUTS_NAMESPACES to be set"
      break
    else
      echo "Waiting for CLUSTER_SCOPED_ARGO_ROLLOUTS_NAMESPACES to be set"
      sleep 5      
    fi
  done

  # Verify the variable is set
  if oc get deployment openshift-gitops-operator-controller-manager -n openshift-gitops-operator -o jsonpath='{.spec.template.spec.containers[0].env}' | grep -q '{"name":"CLUSTER_SCOPED_ARGO_ROLLOUTS_NAMESPACES","value":"argo-rollouts,test-rom-ns-1,rom-ns-1"}'; then
    echo "CLUSTER_SCOPED_ARGO_ROLLOUTS_NAMESPACES is set."
  else 
    echo "ERROR: CLUSTER_SCOPED_ARGO_ROLLOUTS_NAMESPACES was never set."
    exit 1    
  fi

  # Deployment is correct, now wait for Pods to start
  wait_until_pods_running "openshift-gitops-operator"

}

function disable_rollouts_cluster_scope_namespaces() {

  # Remove the env var we previously added to operator

  if ! [ -z $NON_OLM ]; then

    oc set env deployment openshift-gitops-operator-controller-manager -n openshift-gitops-operator CLUSTER_SCOPED_ARGO_ROLLOUTS_NAMESPACES=null
      
  elif [ -z $CI ]; then 

    oc patch -n openshift-gitops-operator subscription openshift-gitops-operator \
      --type json --patch '[{"op": "remove", "path": "/spec/config"}]'
  else

    oc patch -n openshift-gitops-operator subscription `subscription=gitops-operator- && oc get subscription --all-namespaces | grep $subscription | head -1 | awk '{print $2}'` \
      --type json --patch '[{"op": "remove", "path": "/spec/config"}]'
  fi


  # Loop to wait until CLUSTER_SCOPED_ARGO_ROLLOUTS_NAMESPACES is removed from the OpenShift GitOps Operator Deplyoment
  for i in {1..30}; do
    if oc get deployment openshift-gitops-operator-controller-manager -n openshift-gitops-operator -o jsonpath='{.spec.template.spec.containers[0].env}' | grep -q '{"name":"CLUSTER_SCOPED_ARGO_ROLLOUTS_NAMESPACES","value":"argo-rollouts,test-rom-ns-1,rom-ns-1"}'; then
      echo "Waiting for CLUSTER_SCOPED_ARGO_ROLLOUTS_NAMESPACES to be removed"
      sleep 5      
    else
      echo "CLUSTER_SCOPED_ARGO_ROLLOUTS_NAMESPACES has been removed."
      break
    fi
  done

  # Verify it has been removed.
  if oc get deployment openshift-gitops-operator-controller-manager -n openshift-gitops-operator -o jsonpath='{.spec.template.spec.containers[0].env}' | grep -q '{"name":"CLUSTER_SCOPED_ARGO_ROLLOUTS_NAMESPACES","value":"argo-rollouts,test-rom-ns-1,rom-ns-1"}'; then
    echo "ERROR: CLUSTER_SCOPED_ARGO_ROLLOUTS_NAMESPACES was not successfully removed."
    exit 1    
  else 
    echo "CLUSTER_SCOPED_ARGO_ROLLOUTS_NAMESPACES was successfuly removed."
  fi

  # Wait for Pods to reflect the removal of the env var
  wait_until_pods_running "openshift-gitops-operator"
}


enable_rollouts_cluster_scoped_namespaces

trap disable_rollouts_cluster_scope_namespaces EXIT



ROLLOUTS_TMP_DIR=$(mktemp -d)

cd $ROLLOUTS_TMP_DIR

git clone https://github.com/argoproj-labs/argo-rollouts-manager

cd "$ROLLOUTS_TMP_DIR/argo-rollouts-manager"

# This commit value will be automatically updated by calling 'hack/upgrade-rollouts-manager/go-run.sh':
# - It should always point to the same argo-rollouts-manager commit that is referenced in go.mod of gitops-operator (which will usually be the most recent argo-rollouts-manager commit)
TARGET_ROLLOUT_MANAGER_COMMIT=9f79ece2e923cbf03fe041bb6d1d83aae16a08da

# This commit value will be automatically updated by calling 'hack/upgrade-rollouts-manager/go-run.sh':
# - It should always point to the same argo-rollouts-manager commit that is referenced in the version of argo-rollouts-manager that is in go.mod
TARGET_OPENSHIFT_ROUTE_ROLLOUT_PLUGIN_COMMIT=8b4125a7f9ecffb0247df91a4c890f88c0c523b7

git checkout $TARGET_ROLLOUT_MANAGER_COMMIT

# 1) Run E2E tests from argo-rollouts-manager repo

make test-e2e

# Clean up old namespaces created by test
# NOTE: remove this once this is handled by 'make test-e2e' in argo-rollouts-manager repo
kubectl delete rolloutmanagers --all -n test-rom-ns-1 || true

cd "$ROLLOUTS_TMP_DIR/argo-rollouts-manager"


# 2) Run E2E tests from argoproj/argo-rollouts repo

SKIP_RUN_STEP=true hack/run-upstream-argo-rollouts-e2e-tests.sh

# 3) Run rollouts-plugin-trafficrouter-openshift E2E tests

kubectl delete ns argo-rollouts || true

kubectl wait --timeout=5m --for=delete namespace/argo-rollouts

kubectl create ns argo-rollouts
kubectl config set-context --current --namespace=argo-rollouts

cat << EOF > "$ROLLOUTS_TMP_DIR/rollout-manager.yaml"
apiVersion: argoproj.io/v1alpha1
kind: RolloutManager
metadata:
  name: argo-rollout
  namespace: argo-rollouts
spec: {}
EOF

kubectl apply -f "$ROLLOUTS_TMP_DIR/rollout-manager.yaml"

cd "$ROLLOUTS_TMP_DIR"
git clone https://github.com/argoproj-labs/rollouts-plugin-trafficrouter-openshift

cd "$ROLLOUTS_TMP_DIR/rollouts-plugin-trafficrouter-openshift"

git checkout $TARGET_OPENSHIFT_ROUTE_ROLLOUT_PLUGIN_COMMIT

make test-e2e

