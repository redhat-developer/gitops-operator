#!/usr/bin/env bash

# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This script runs the presubmit tests; it is started by prow for each PR.
# For convenience, it can also be executed manually.
# Running the script without parameters, or with the --all-tests
# flag, causes all tests to be executed, in the right order.
# Use the flags --build-tests, --unit-tests and --integration-tests
# to run a specific set of tests.

# Helper functions for E2E tests.

function unexpectedError() {
  echo "Unexpected error occured!!"
  failed=1

  ((failed)) && fail_test
  success
}

function make_banner() {
  local msg="$1$1$1$1 $2 $1$1$1$1"
  local border="${msg//[-0-9A-Za-z _.,\/()]/$1}"
  echo -e "${border}\n${msg}\n${border}"
}

# Simple header for logging purposes.
function header() {
  local upper="$(echo $1 | tr a-z A-Z)"
  make_banner "=" "${upper}"
}

function wait_until_pods_running() {
  echo -n "Waiting until all pods in namespace $1 are up"
  for i in {1..150}; do # timeout after 5 minutes
    local pods="$(oc get pods --no-headers -n $1 2>/dev/null)"
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

function wait_until_object_exist() {
  local oc_ARGS="get $1 $2"
  local DESCRIPTION="$1 $2"

  if [[ -n $3 ]]; then
    oc_ARGS="get -n $3 $1 $2"
    DESCRIPTION="$1 $3/$2"
  fi
  echo -n "Waiting until ${DESCRIPTION} exist"
  for i in {1..150}; do # timeout after 5 minutes
    if oc ${oc_ARGS} >/dev/null 2>&1; then
      echo -e "\n${DESCRIPTION} exist"
      return 0
    fi
    echo -n "."
    sleep 2
  done
  echo -e "\n\nERROR: timeout waiting for ${DESCRIPTION} to exist"
  oc ${oc_ARGS}
  return 1
}

function wait_until_object_doesnt_exist() {
  local KUBECTL_ARGS="get $1 $2"
  local DESCRIPTION="$1 $2"

  if [[ -n $3 ]]; then
    KUBECTL_ARGS="get -n $3 $1 $2"
    DESCRIPTION="$1 $3/$2"
  fi
  echo -n "Waiting until ${DESCRIPTION} doesn't exist"
  for i in {1..150}; do # timeout after 5 minutes
    if ! kubectl ${KUBECTL_ARGS} >/dev/null 2>&1; then
      echo -e "\n${DESCRIPTION} dosen't exist"
      return 0
    fi
    echo -n "."
    sleep 2
  done
  echo -e "\n\nERROR: timeout waiting for ${DESCRIPTION} to doesn't exist"
  kubectl ${KUBECTL_ARGS}
  return 1
}

function dump_cluster_state() {
  echo "***************************************"
  echo "***         E2E TEST FAILED         ***"
  echo "***    Start of information dump    ***"
  echo "***************************************"
  echo ">>> All resources:"
  kubectl get all --all-namespaces
  echo ">>> Services:"
  kubectl get services --all-namespaces
  echo ">>> Events:"
  kubectl get events --all-namespaces
  dump_extra_cluster_state
  echo "***************************************"
  echo "***         E2E TEST FAILED         ***"
  echo "***     End of information dump     ***"
  echo "***************************************"
}

function dump_extra_cluster_state() {
  echo ">>> Gitops controller log:"
  kubectl -n openshift-gitops-operator logs $(get_app_pod argocd-operator openshift-gitops-operator) --all-containers=true
}

# Returns the name of the first pod of the given app.
# Parameters: $1 - app name.
#             $2 - namespace (optional).
function get_app_pod() {
  local pods=($(get_app_pods $1 $2))
  echo "${pods[0]}"
}

# Returns the name of all pods of the given app.
# Parameters: $1 - app name.
#             $2 - namespace (optional).
function get_app_pods() {
  local namespace=""
  [[ -n $2 ]] && namespace="-n $2"
  kubectl get pods ${namespace} --selector=control-plane=$1 --output=jsonpath="{.items[*].metadata.name}"
}

function fail_test() {
  set_test_return_code 1
  [[ -n $1 ]] && echo "ERROR: $1"
  exit 1
}

function set_test_return_code() {
  echo -n "$1"
}

function success() {
  set_test_return_code 0
  n="E2E"
  [[ -n $1 ]] && n=$(echo $1 | tr '[:lower:]' '[:upper:]')
  echo "**************************************"
  echo "***        $n TESTS PASSED         ***"
  echo "**************************************"
  exit 0
}

function build_and_push_catalog_image() {

  if [ "$E2E_SKIP_BUILD_TOOL_INSTALLATION" = false ]; then
    echo ">> Install operator-sdk & opm"
    make operator-sdk opm
  else
    echo ">> skipping operator-sdk & olm installation"
  fi

  echo ">> Building and pushing operator images"
  make docker-build docker-push

  echo ">> Making bundle"
  make bundle CHANNELS=$CHANNEL DEFAULT_CHANNEL=$CHANNEL

  echo ">> Building and pushing Bundle images"
  make bundle-build bundle-push

  echo "Build and push index image"
  make catalog-build catalog-push

}

function configure_operator() {
  header "Configuring OpenShift Gitops operator"

  echo -e "Disabling default catalog sources"
  kubectl patch operatorhub.config.openshift.io/cluster -p='{"spec":{"disableAllDefaultSources":true}}' --type=merge
  sleep 5

  # echo -e "Copying artifacts [catalog source, image content source policy, mapping.txt]..."
  cat <<EOF >$TMP_DIR/catalog-source.yaml
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: $CATALOG_SOURCE
  namespace: openshift-marketplace
spec:
  sourceType: grpc
  image: $IMAGE-catalog:v$VERSION
  displayName: openshift-gitops
  updateStrategy:
    registryPoll:
      interval: 30m
EOF

  echo -e "Creating custom catalog source"
  kubectl apply -f $TMP_DIR/catalog-source.yaml

  echo "Waiting for pods in namespace openshift-marketplace to be ready"
  # filtering out old catalog source pod that will be removed shortly
  pods=$(kubectl get pods -n openshift-marketplace --sort-by={metadata.creationTimestamp} -o name |
    grep gitops-operator | tail -1)

  for pod in ${pods}; do
    echo "Waiting for pod $pod in openshift-marketplace to be in ready state"
    kubectl wait --for=condition=Ready -n openshift-marketplace $pod --timeout=5m
  done
}

function uninstall_operator_resources() {

  deployments=$(oc get deployments -n openshift-gitops --no-headers -o name 2>/dev/null)

  # Delete instance (name: cluster) of gitopsservices.pipelines.openshift.io
  oc delete --ignore-not-found=true gitopsservices.pipelines.openshift.io cluster 2>/dev/null || fail_test "Unable to delete gitopsservice cluster instance"

  wait_until_object_doesnt_exist "gitopsservices.pipelines.openshift.io" "cluster" "openshift-gitops" || fail_test "gitops service haven't deleted successfully"

  # wait for pods deployments to be deleted in gitops namespace
  for deployment in $deployments; do
    oc wait --for=delete $deployment -n openshift-gitops --timeout=5m || fail_test "Failed to delete deployment: $deployment in openshift-gitops namespace"
  done

  oc delete $(oc get csv -n openshift-gitops-operator -o name | grep gitops) -n openshift-gitops-operator || fail_test "Unable to delete CSV"

  oc delete -n openshift-gitops-operator installplan $(oc get subscription gitops-operator -n openshift-gitops-operator -o jsonpath='{.status.installplan.name}') || fail_test "Unable to delete installplan"

  oc delete subscription gitops-operator -n openshift-gitops-operator --cascade=background || fail_test "Unable to delete subscription"

  echo -e ">> Delete arogo resources accross all namespaces"
  for res in applications applicationsets appprojects argocds; do
    oc delete --ignore-not-found=true ${res}.argoproj.io --all
  done

  echo -e ">> Cleanup existing crds"
  for res in applications applicationsets appprojects argocds; do
    oc delete --ignore-not-found=true crds ${res}.argoproj.io
  done

  echo -e ">> Delete \"openshift-gitops\" project"
  oc delete --ignore-not-found=true project openshift-gitops
}

function install_operator_resources() {
  echo -e ">>Ensure Gitops subscription exists"
  oc get subscription gitops-operator -n openshift-gitops-operator 2>/dev/null ||
    cat <<EOF | oc apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: gitops-operator
  namespace: openshift-gitops-operator
spec:
  channel: $CHANNEL
  installPlanApproval: Automatic
  name: gitops-operator
  source: $CATALOG_SOURCE
  sourceNamespace: openshift-marketplace
EOF

  wait_until_pods_running "openshift-gitops-operator" || fail_test "openshift gitops Operator controller did not come up"

  echo ">> Wait for GitopsService creation"
  wait_until_object_exist "gitopsservices.pipelines.openshift.io" "cluster" "openshift-gitops" || fail_test "gitops service haven't created yet"

  wait_until_pods_running "openshift-gitops" || fail_test "argocd controller did not come up"

  #Make sure that everything is cleaned up in the current namespace.
  for res in applications applicationsets appprojects appprojects; do
    oc delete --ignore-not-found=true ${res}.argoproj.io --all
  done
}

function get_operator_namespace() {
  # TODO: parameterize namespace, operator can run in a namespace different from the namespace where tektonpipelines is installed
  local operator_namespace="argocd-operator"
  [[ "${TARGET}" == "openshift" ]] && operator_namespace="openshift-gitops"
  echo ${operator_namespace}
}
