#!/usr/bin/env bash

NAMESPACE_PREFIX=${NAMESPACE_PREFIX:-"gitops-operator-"}
GIT_REVISION=${GIT_REVISION:-"b165a7e7829bdaa6585e0bea6159183f32d58bec"}
IMG=${IMG:-"brew.registry.redhat.io/rh-osbs/openshift-gitops-1-gitops-rhel8-operator:v99.9.0-57"}
BUNDLE_IMG=${BUNDLE_IMG:-"brew.registry.redhat.io/rh-osbs/openshift-gitops-1-gitops-operator-bundle:v99.9.0-57"}

# Image overrides
# gitops-operator version tagged images
GITOPS_OPERATOR_VER=v99.9.0-57
ARGOCD_DEX_IMAGE=${ARGOCD_DEX_IMAGE:-"brew.registry.redhat.io/rh-osbs/openshift-gitops-1-dex-rhel8:${GITOPS_OPERATOR_VER}"}
ARGOCD_IMAGE=${ARGOCD_IMAGE:-"brew.registry.redhat.io/rh-osbs/openshift-gitops-1-argocd-rhel8:${GITOPS_OPERATOR_VER}"}
BACKEND_IMAGE=${BACKEND_IMAGE:-"brew.registry.redhat.io/rh-osbs/openshift-gitops-1-gitops-rhel8:${GITOPS_OPERATOR_VER}"}
GITOPS_CONSOLE_PLUGIN_IMAGE=${GITOPS_CONSOLE_PLUGIN_IMAGE:-"brew.registry.redhat.io/rh-osbs/openshift-gitops-1-console-plugin-rhel8:${GITOPS_OPERATOR_VER}"}
KAM_IMAGE=${KAM_IMAGE:-"brew.registry.redhat.io/rh-osbs/openshift-gitops-1-kam-delivery-rhel8:${GITOPS_OPERATOR_VER}"}

# other images
ARGOCD_KEYCLOAK_IMAGE=${ARGOCD_KEYCLOAK_IMAGE:-"registry.redhat.io/rh-sso-7/sso7-rhel8-operator:7.6-8"}
ARGOCD_REDIS_IMAGE=${ARGOCD_REDIS_IMAGE:-"registry.redhat.io/rhel8/redis-6:1-110"}
ARGOCD_REDIS_HA_PROXY_IMAGE=${ARGOCD_REDIS_HA_PROXY_IMAGE:-"registry.redhat.io/openshift4/ose-haproxy-router:v4.12.0-202302280915.p0.g3065f65.assembly.stream"}

SCRIPT_DIR="$(
  cd "$(dirname "$0")" >/dev/null
  pwd
)"

# deletes the temp directory
function cleanup() {
  rm -rf "${TEMP_DIR}"
  echo "Deleted temp working directory $WORK_DIR"
}

add_brew_registry_credentials ()
{
  # Get current information
  oc get secrets pull-secret \
    -n openshift-config \
    -o template='{{index .data ".dockerconfigjson"}}' \
    | base64 -d > ${TEMP_DIR}/oldauth.json
  
  # Get Brew registry credentials
  local brew_secret=$(jq '.auths."brew.registry.redhat.io".auth' \
                      ${HOME}/.docker/config.json \
                      | tr -d '"')

  # Append the key:value to the JSON file
  jq --arg secret ${brew_secret} \
    '.auths |= . + {"brew.registry.redhat.io":{"auth":$secret}}' \
    ${TEMP_DIR}/oldauth.json > ${TEMP_DIR}/newauth.json

  # Update the pull-secret information in OCP
  oc set data secret pull-secret \
    -n openshift-config \
    --from-file=.dockerconfigjson=${newauth}

  # Cleanup
  rm -f ${oldauth} ${newauth}
}

# installs the stable version kustomize binary if not found in PATH
function install_kustomize() {
  if [[ -z "${KUSTOMIZE}" ]]; then
    wget https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2Fv4.5.7/kustomize_v4.5.7_$(uname | tr '[:upper:]' '[:lower:]')_$(uname -m |sed s/aarch64/arm64/ | sed s/x86_64/amd64/).tar.gz -O ${TEMP_DIR}/kustomize.tar.gz
    tar zxvf ${TEMP_DIR}/kustomize.tar.gz -C ${TEMP_DIR}
    KUSTOMIZE=${TEMP_DIR}/kustomize
    chmod +x ${TEMP_DIR}/kustomize
  fi
}

# installs the stable version of kubectl binary if not found in PATH
function install_kubectl() {
  if [[ -z "${KUBECTL}" ]]; then
    wget https://dl.k8s.io/release/v1.26.0/bin/$(uname | tr '[:upper:]' '[:lower:]')/$(uname -m | sed s/aarch64/arm64/ | sed s/x86_64/amd64/)/kubectl -O ${TEMP_DIR}/kubectl
    KUBECTL=${TEMP_DIR}/kubectl
    chmod +x ${TEMP_DIR}/kubectl
  fi
}

# installs the stable version of yq binary if not found in PATH
function install_yq() {
  if [[ -z "${YQ}" ]]; then
    wget https://github.com/mikefarah/yq/releases/download/v4.31.2/yq_$(uname | tr '[:upper:]' '[:lower:]')_$(uname -m | sed s/aarch64/arm64/ | sed s/x86_64/amd64/) -O ${TEMP_DIR}/yq
    YQ=${TEMP_DIR}/yq
    chmod +x ${TEMP_DIR}/yq
  fi
}

# creates a kustomization.yaml file in the temp directory pointing to the manifests available in the upstream repo.
function create_kustomization_init_file() {
  echo "apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: ${NAMESPACE_PREFIX}system
namePrefix: ${NAMESPACE_PREFIX}
resources:
  - https://github.com/redhat-developer/gitops-operator/config/crd
  - https://github.com/redhat-developer/gitops-operator/config/rbac
  - https://github.com/redhat-developer/gitops-operator/config/manager
patches:
  - path: https://raw.githubusercontent.com/redhat-developer/gitops-operator/master/config/default/manager_auth_proxy_patch.yaml 
  - path: env-overrides.yaml" > ${TEMP_DIR}/kustomization.yaml
}

# creates a patch file, containing the environment variable overrides for overriding the default images
# for various gitops-operator components.
function create_image_overrides_patch_file() {
  echo "apiVersion: apps/v1
kind: Deployment
metadata:
  name: controller-manager
  namespace: system
spec:
  template:
    spec:
      containers:
      - name: manager
        image: ${IMG}
        env:
        - name: ARGOCD_DEX_IMAGE
          value: ${ARGOCD_DEX_IMAGE}
        - name: ARGOCD_KEYCLOAK_IMAGE
          value: ${ARGOCD_KEYCLOAK_IMAGE}
        - name: BACKEND_IMAGE
          value: ${BACKEND_IMAGE}
        - name: ARGOCD_IMAGE
          value: ${ARGOCD_IMAGE}
        - name: ARGOCD_REPOSERVER_IMAGE
          value: ${ARGOCD_IMAGE}
        - name: ARGOCD_REDIS_IMAGE
          value: ${ARGOCD_REDIS_IMAGE}
        - name: ARGOCD_REDIS_HA_IMAGE
          value: ${ARGOCD_REDIS_IMAGE}
        - name: ARGOCD_REDIS_HA_PROXY_IMAGE
          value: ${ARGOCD_REDIS_HA_PROXY_IMAGE}
        - name: GITOPS_CONSOLE_PLUGIN_IMAGE
          value: ${GITOPS_CONSOLE_PLUGIN_IMAGE}
        - name: KAM_IMAGE
          value: ${KAM_IMAGE}" > ${TEMP_DIR}/env-overrides.yaml
}

function create_argocd_admin_clusterrole() {
    echo "apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: argocd-application-controller-role" > ${TEMP_DIR}/rbac-patch.yaml

  wget https://raw.githubusercontent.com/redhat-developer/gitops-operator/master/bundle/manifests/gitops-operator.clusterserviceversion.yaml -O ${TEMP_DIR}/gitops-operator.clusterserviceversion.yaml
  cat "${TEMP_DIR}"/gitops-operator.clusterserviceversion.yaml |  yq -e '.spec.install.spec.clusterPermissions[0].rules' >> "${TEMP_DIR}"/rbac-patch.yaml
}
function create_deployment_patch_from_bundle_image() {
  container_id=$(${DOCKER} create --entrypoint sh "${BUNDLE_IMG}")
  ${DOCKER} cp "$container_id:manifests/gitops-operator.clusterserviceversion.yaml" "${TEMP_DIR}"
  ${DOCKER} rm "$container_id"

  echo "apiVersion: apps/v1
kind: Deployment
metadata:
  name: controller-manager
  namespace: system" > "${TEMP_DIR}"/env-overrides.yaml
  cat "${TEMP_DIR}"/gitops-operator.clusterserviceversion.yaml | yq -e '.spec.install.spec.deployments[0]' | tail -n +2 >> "${TEMP_DIR}"/env-overrides.yaml
  yq -e -i '.spec.selector.matchLabels.control-plane = "argocd-operator"' "${TEMP_DIR}"/env-overrides.yaml
  yq -e -i '.spec.template.metadata.labels.control-plane = "argocd-operator"' "${TEMP_DIR}"/env-overrides.yaml
  cat "${TEMP_DIR}"/env-overrides.yaml
}

# Code execution starts here
# create a temporary directory and do all the operations inside the directory.
TEMP_DIR=$(mktemp -d -t gitops-operator-install-XXXXXXX)
echo "Using temp directory $TEMP_DIR"
# cleanup the temporary directory irrespective of whether the script ran successfully or failed with an error.
trap cleanup EXIT

# install kustomize in the the temp directory if its not available in the PATH
KUSTOMIZE=$(which kustomize)
install_kustomize

# install kubectl in the the temp directory if its not available in the PATH
KUBECTL=$(which kubectl)
install_kubectl

# install yq in the the temp directory if its not available in the PATH
YQ=$(which yq)
install_yq

# copy the rbac patch file to the kustomize directory
cp ${SCRIPT_DIR}/rbac-patch.yaml ${TEMP_DIR}

# create the required yaml files for the kustomize based install.
create_image_overrides_patch_file

create_kustomization_init_file

# use kubectl binary to apply the manifests from the directory containing the kustomization.yaml file.
#${KUBECTL} apply -k ${TEMP_DIR}

# apply the RBAC patch
#${KUBECTL} apply -f ${TEMP_DIR}/rbac-patch.yaml

# Get the options
while getopts ":iu" option; do
   case $option in
      i) # use kubectl binary to apply the manifests from the directory containing the kustomization.yaml file.
         echo "installing ..."
         ${KUBECTL} apply -k ${TEMP_DIR}
         ${KUBECTL} apply -f ${TEMP_DIR}/rbac-patch.yaml
         exit;;
      u) # uninstall
         echo "uninstalling ..."
         ${KUBECTL} delete -k ${TEMP_DIR}
         ${KUBECTL} delete -f ${TEMP_DIR}/rbac-patch.yaml
         exit;;   
     \?) # Invalid option
         echo "Error: Invalid option"
         exit;;
   esac
done

