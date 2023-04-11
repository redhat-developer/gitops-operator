#!/usr/bin/env bash

NAMESPACE_PREFIX=${NAMESPACE_PREFIX:-"gitops-operator-"}
GIT_REVISION=${GIT_REVISION:-"master"}

# gitops-operator version tagged images
OPERATOR_REGISTRY=${OPERATOR_REGISTRY:-"registry.redhat.io"}
GITOPS_OPERATOR_VER=${GITOPS_OPERATOR_VER:-"v1.8.2-5"}
OPERATOR_REGISTRY_ORG=${OPERATOR_ORG:-"openshift-gitops-1"}  
OPERATOR_IMG=${OPERATOR_IMG:-"${OPERATOR_REGISTRY}/${OPERATOR_REGISTRY_ORG}/gitops-rhel8-operator:${GITOPS_OPERATOR_VER}"}

# If enabled, operator and component image URLs would be derived from within CSV present in the bundle image.
USE_BUNDLE_IMG=${USE_BUNDLE_IMG:-"false"}
BUNDLE_IMG=${BUNDLE_IMG:-"${OPERATOR_REGISTRY}/${OPERATOR_REGISTRY_ORG}/gitops-operator-bundle:${GITOPS_OPERATOR_VER}"}
DOCKER=${DOCKER:-"podman"}

# Image overrides
# gitops-operator version tagged images
ARGOCD_DEX_IMAGE=${ARGOCD_DEX_IMAGE:-"${OPERATOR_REGISTRY}/${OPERATOR_REGISTRY_ORG}/dex-rhel8:${GITOPS_OPERATOR_VER}"}
ARGOCD_IMAGE=${ARGOCD_IMAGE:-"${OPERATOR_REGISTRY}/${OPERATOR_REGISTRY_ORG}/argocd-rhel8:${GITOPS_OPERATOR_VER}"}
ARGOCD_APPLICATIONSET_IMAGE=${ARGOCD_APPLICATIONSET_IMAGE:-"${OPERATOR_REGISTRY}/${OPERATOR_REGISTRY_ORG}/applicationset-rhel8:${GITOPS_OPERATOR_VER}"}
BACKEND_IMAGE=${BACKEND_IMAGE:-"${OPERATOR_REGISTRY}/${OPERATOR_REGISTRY_ORG}/gitops-rhel8:${GITOPS_OPERATOR_VER}"}
GITOPS_CONSOLE_PLUGIN_IMAGE=${GITOPS_CONSOLE_PLUGIN_IMAGE:-"${OPERATOR_REGISTRY}/${OPERATOR_REGISTRY_ORG}/console-plugin-rhel8:${GITOPS_OPERATOR_VER}"}
KAM_IMAGE=${KAM_IMAGE:-"${OPERATOR_REGISTRY}/${OPERATOR_REGISTRY_ORG}/kam-delivery-rhel8:${GITOPS_OPERATOR_VER}"}

# other images
ARGOCD_KEYCLOAK_IMAGE=${ARGOCD_KEYCLOAK_IMAGE:-"registry.redhat.io/rh-sso-7/sso7-rhel8-operator:7.6-8"}
ARGOCD_REDIS_IMAGE=${ARGOCD_REDIS_IMAGE:-"registry.redhat.io/rhel8/redis-6:1-110"}
ARGOCD_REDIS_HA_PROXY_IMAGE=${ARGOCD_REDIS_HA_PROXY_IMAGE:-"registry.redhat.io/openshift4/ose-haproxy-router:v4.12.0-202302280915.p0.g3065f65.assembly.stream"}

# Tool Versions
KUSTOMIZE_VERSION=${KUSTOMIZE_VERSION:-"v4.5.7"}
KUBECTL_VERSION=${KUBECTL_VERSION:-"v1.26.0"}
YQ_VERSION=${YQ_VERSION:-"v4.31.2"}

# Check if a pod is ready, if it fails to get ready, rollback to PREV_IMAGE
function check_pod_status_ready() {
  for binary in "$@"; do
    echo "Binary $binary";
    pod_name=$(${KUBECTL} get pods --no-headers -o custom-columns=":metadata.name" -n ${NAMESPACE_PREFIX}system | grep "$binary");
    if [ ! -z "$pod_name" ]; then
      echo "Pod name : $pod_name";
      ${KUBECTL} wait pod --for=condition=Ready $pod_name -n ${NAMESPACE_PREFIX}system --timeout=150s;
      if [ $? -ne 0 ]; then
        echo "Pod '$pod_name' failed to become Ready in desired time. Logs from the pod:"
        kubectl logs $pod_name -n ${NAMESPACE_PREFIX}system;
        echo "\nInstall/Upgrade failed. Performing rollback to $PREV_IMAGE";      
        rollback
      fi
    fi
  done
}

function rollback() {
  if [ ! -z "${PREV_OPERATOR_IMG}" ]; then
    export OPERATOR_IMG=${PREV_OPERATOR_IMG}    
    prepare_kustomize_files
    ${KUSTOMIZE} build ${TEMP_DIR} | ${KUBECTL} apply -f -
    echo "Upgrade Unsuccessful!!";
  else
    echo "Installing image for the first time. Nothing to rollback. Quitting..";
  fi
  exit 1;
}

# deletes the temp directory
function cleanup() {
  rm -rf "${TEMP_DIR}"
  echo "Deleted temp working directory ${TEMP_DIR}"
}

# installs the stable version kustomize binary if not found in PATH
function install_kustomize() {
  if [[ -z "${KUSTOMIZE}" ]]; then
    echo "[INFO] kustomize binary not found in \$PATH, installing kustomize-${KUSTOMIZE_VERSION} in ${TEMP_DIR}"
    wget https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2F${KUSTOMIZE_VERSION}/kustomize_${KUSTOMIZE_VERSION}_$(uname | tr '[:upper:]' '[:lower:]')_$(uname -m |sed s/aarch64/arm64/ | sed s/x86_64/amd64/).tar.gz -O ${TEMP_DIR}/kustomize.tar.gz
    tar zxvf ${TEMP_DIR}/kustomize.tar.gz -C ${TEMP_DIR}
    KUSTOMIZE=${TEMP_DIR}/kustomize
    chmod +x ${TEMP_DIR}/kustomize
  fi
}

# installs the stable version of kubectl binary if not found in PATH
function install_kubectl() {
  if [[ -z "${KUBECTL}" ]]; then
    echo "[INFO] kubectl binary not found in \$PATH, installing kubectl-${KUBECTL_VERSION} in ${TEMP_DIR}"
    wget https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/$(uname | tr '[:upper:]' '[:lower:]')/$(uname -m | sed s/aarch64/arm64/ | sed s/x86_64/amd64/)/kubectl -O ${TEMP_DIR}/kubectl
    KUBECTL=${TEMP_DIR}/kubectl
    chmod +x ${TEMP_DIR}/kubectl
  fi
}

# installs the stable version of yq binary if not found in PATH
function install_yq() {
  if [[ -z "${YQ}" ]]; then
    echo "[INFO] yq binary not found in \$PATH, installing yq-${YQ_VERSION} in ${TEMP_DIR}"
    wget https://github.com/mikefarah/yq/releases/download/${YQ_VERSION}/yq_$(uname | tr '[:upper:]' '[:lower:]')_$(uname -m | sed s/aarch64/arm64/ | sed s/x86_64/amd64/) -O ${TEMP_DIR}/yq
    YQ=${TEMP_DIR}/yq
    chmod +x ${TEMP_DIR}/yq
  fi
}

# creates a kustomization.yaml file in the temp directory pointing to the manifests available in the upstream repo.
function create_kustomization_init_file() {
  echo "[INFO] Creating kustomization.yaml file using manifests from revision ${GIT_REVISION}"
  echo "apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: ${NAMESPACE_PREFIX}system
namePrefix: ${NAMESPACE_PREFIX}
resources:
  - https://github.com/redhat-developer/gitops-operator/config/crd?ref=$GIT_REVISION
  - https://github.com/redhat-developer/gitops-operator/config/rbac?ref=$GIT_REVISION
  - https://github.com/redhat-developer/gitops-operator/config/manager?ref=$GIT_REVISION
patches:
  - path: https://raw.githubusercontent.com/redhat-developer/gitops-operator/master/config/default/manager_auth_proxy_patch.yaml 
  - path: env-overrides.yaml
  - path: security-context.yaml" > ${TEMP_DIR}/kustomization.yaml
}

# creates a patch file, containing the environment variable overrides for overriding the default images
# for various gitops-operator components.
function create_image_overrides_patch_file() {
  echo "[INFO] Creating env-overrides.yaml file using component images specified in environment variables"
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
        image: ${OPERATOR_IMG}
        command:
        - manager
        env:
        - name: ARGOCD_DEX_IMAGE
          value: ${ARGOCD_DEX_IMAGE}
        - name: ARGOCD_KEYCLOAK_IMAGE
          value: ${ARGOCD_KEYCLOAK_IMAGE}
        - name: ARGOCD_APPLICATIONSET_IMAGE 
          value: ${ARGOCD_APPLICATIONSET_IMAGE}
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

function create_security_context_patch_file(){
echo "[INFO] Creating security-context.yaml file using component images specified in environment variables"
  echo "apiVersion: apps/v1
kind: Deployment
metadata:
  name: controller-manager
  namespace: system
spec:
  template:
    metadata:
      annotations:
        openshift.io/scc: restricted-v2
    spec:
      containers:
      - name: kube-rbac-proxy
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop:
            - ALL
          readOnlyRootFilesystem: true
          runAsNonRoot: true
          seccompProfile:
            type: RuntimeDefault
      - name: manager
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop:
            - ALL
          readOnlyRootFilesystem: true
          runAsNonRoot: true
          seccompProfile:
            type: RuntimeDefault" > ${TEMP_DIR}/security-context.yaml
}

function create_deployment_patch_from_bundle_image() {
  echo "[INFO] Creating env-overrides.yaml file using component images specified in the gitops operator bundle image"
  container_id=$(${DOCKER} create --entrypoint sh "${BUNDLE_IMG}")
  ${DOCKER} cp "$container_id:manifests/gitops-operator.clusterserviceversion.yaml" "${TEMP_DIR}"
  ${DOCKER} rm "$container_id"

  echo "apiVersion: apps/v1
kind: Deployment
metadata:
  name: controller-manager
  namespace: system" > "${TEMP_DIR}"/env-overrides.yaml
  cat "${TEMP_DIR}"/gitops-operator.clusterserviceversion.yaml | ${YQ} -e '.spec.install.spec.deployments[0]' | tail -n +2 >> "${TEMP_DIR}"/env-overrides.yaml
  ${YQ} -e -i '.spec.selector.matchLabels.control-plane = "argocd-operator"' "${TEMP_DIR}"/env-overrides.yaml
  ${YQ} -e -i '.spec.template.metadata.labels.control-plane = "argocd-operator"' "${TEMP_DIR}"/env-overrides.yaml
  cat "${TEMP_DIR}"/env-overrides.yaml
}

function init_work_directory() {
  # create a temporary directory and do all the operations inside the directory.
  TEMP_DIR=$(mktemp -d "${TMPDIR:-"/tmp"}/gitops-operator-install-XXXXXXX")
  echo "Using temp directory $TEMP_DIR"
  # cleanup the temporary directory irrespective of whether the script ran successfully or failed with an error.
  trap cleanup EXIT
}

# Checks if the pre-requisite binaries are already present in the PATH,
# if not installs appropriate versions of them.
# This function just checks if the binary is found in the PATH and 
# does not validate if the version of the binary matches the minimum required version.
function check_and_install_prerequisites {
  # install kustomize in the the temp directory if its not available in the PATH
  KUSTOMIZE=$(which kustomize)
  install_kustomize

  # install kubectl in the the temp directory if its not available in the PATH
  KUBECTL=$(which kubectl)
  install_kubectl

  # install yq in the the temp directory if its not available in the PATH
  YQ=$(which yq)
  install_yq
}

# Checks if the openshift-gitops-operator is already installed in the system.
# if so, stores the previous version which would be used for rollback in case of
# a failure during installation.
function get_prev_operator_image() {
  PREV_OPERATOR_IMG=$(${KUBECTL} get deploy/gitops-operator-controller-manager -n ${NAMESPACE_PREFIX}system -o jsonpath='{..image}');
  echo "PREV OPERATOR IMAGE : ${PREV_OPERATOR_IMG}"
}

# Prepares the kustomization.yaml file in the TEMP_DIR which would be used 
# for the installation.
function prepare_kustomize_files() {
  # create the required yaml files for the kustomize based install.
  create_kustomization_init_file
  DOCKER=$(which ${DOCKER})
  if [[ ${USE_BUNDLE_IMG} == "true" && ! -z "${DOCKER}" ]]; then
    echo "Generating env-overrides.yaml file from the CSV defined in the bundle image"
    create_deployment_patch_from_bundle_image
  else
    echo "Bundle image is disabled or Docker binary ${DOCKER} not found in PATH"
    echo "Generating env-overrides.yaml file from the values provided in the environment variable"
    create_image_overrides_patch_file
  fi
  create_security_context_patch_file
}

# Code execution starts here
# Get the options
while getopts ":iu" option; do
  case $option in
    i) # use kubectl binary to apply the manifests from the directory containing the kustomization.yaml file.
      echo "installing ..."
      init_work_directory
      check_and_install_prerequisites
      get_prev_operator_image
      prepare_kustomize_files
      ${KUSTOMIZE} build ${TEMP_DIR} | ${KUBECTL} apply -f -
      # Check pod status and rollback if necessary.
      check_pod_status_ready gitops-operator-controller-manager 
      exit;;
    u) # uninstall
      echo "uninstalling ..."
      init_work_directory
      check_and_install_prerequisites
      prepare_kustomize_files
      ${KUBECTL} delete -k ${TEMP_DIR}
      # TODO: Remove the workaround of adding RBAC policies once the below issue is resolved.
      # Workaround for fixing the issue https://github.com/redhat-developer/gitops-operator/issues/148
      ${KUBECTL} delete -f https://raw.githubusercontent.com/anandf/gitops-operator/add_install_script/hack/non-bundle-install/rbac-patch.yaml
      exit;;
    \?) # Invalid option
      echo "Error: Invalid option"
      exit;;
  esac
done
