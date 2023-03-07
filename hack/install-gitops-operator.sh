#!/usr/bin/env bash


set -x 

NAMESPACE_PREFIX=${1:-gitops-operator-}
GIT_REVISION=${2:-b165a7e7829bdaa6585e0bea6159183f32d58bec}
IMG=${3:-quay.io/anjoseph/openshift-gitops-1-gitops-rhel8-operator:v99.9.0-51}

# Image overrides
ARGOCD_DEX_IMAGE=registry.redhat.io/openshift-gitops-1/dex-rhel8:v1.7.2-5
ARGOCD_IMAGE=registry.redhat.io/openshift-gitops-1/argocd-rhel8:v1.7.2-5
ARGOCD_KEYCLOAK_IMAGE=registry.redhat.io/rh-sso-7/sso75-openshift-rhel8i:v1.7.2-5
ARGOCD_REDIS_IMAGE=registry.redhat.io/rhel8/redis-6:1-110
ARGOCD_REDIS_HA_PROXY_IMAGE=registry.redhat.io/openshift4/ose-haproxy-router:v4.12.0-202302280915.p0.g3065f65.assembly.stream
BACKEND_IMAGE=registry.redhat.io/openshift-gitops-1/gitops-rhel8:v1.7.2-5
GITOPS_CONSOLE_PLUGIN_IMAGE=registry.redhat.io/openshift-gitops-1/console-plugin-rhel8:v1.7.2-5
KAM_IMAGE=registry.redhat.io/openshift-gitops-1/kam-delivery-rhel8:v1.7.2-5

# deletes the temp directory
function cleanup {      
  rm -rf "${TEMP_DIR}"
  echo "Deleted temp working directory $WORK_DIR"
}

function install_kustomize {
  if [[ -z "${KUSTOMIZE}" ]]; then
    wget https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2Fv4.5.7/kustomize_v4.5.7_$(uname | tr '[:upper:]' '[:lower:]')_$(uname -m).tar.gz -O ${TEMP_DIR}/kustomize.tar.gz
    tar zxvf ${TEMP_DIR}/kustomize.tar.gz -C ${TEMP_DIR}
    KUSTOMIZE=${TEMP_DIR}/kustomize
    chmod +x ${TEMP_DIR}/kustomize
  fi
}

function install_kubectl {
  if [[ -z "${KUBECTL}" ]]; then
    wget https://dl.k8s.io/release/v1.26.0/bin/$(uname | tr '[:upper:]' '[:lower:]')/$(uname -m)/kubectl -O ${TEMP_DIR}/kubectl
    KUBECTL=${TEMP_DIR}/kubectl
    chmod +x ${TEMP_DIR}/kubectl
  fi
}

function create_kustomization_init_file {
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
  - path: env-overrides.yaml" > ${TEMP_DIR}/kustomization.yaml
}

function create_image_overrides_patch_file {
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

# Code execution starts here
TEMP_DIR=$(mktemp -d -t gitops-operator-install-XXXXXXX)
echo "Using temp directory $TEMP_DIR"
trap cleanup EXIT

KUSTOMIZE=$(which kustomize)
install_kustomize
KUBECTL=$(which kubectl)
install_kubectl
create_image_overrides_patch_file
create_kustomization_init_file

${KUBECTL} apply -k ${TEMP_DIR}


