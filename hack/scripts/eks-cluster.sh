#!/usr/bin/env bash
#
# Manage an AWS EKS cluster (3 nodes) suitable for Argo CD HA.
#
# Usage:
#   ./eks-cluster.sh create  [CLUSTER_NAME] [REGION]
#   ./eks-cluster.sh delete  [CLUSTER_NAME] [REGION]
#   ./eks-cluster.sh status  [CLUSTER_NAME] [REGION]
#
# Defaults:
#   CLUSTER_NAME = argocd-ha
#   REGION       = us-east-1

set -euo pipefail

usage() {
  echo "Usage: $0 {create|delete|status} [CLUSTER_NAME] [REGION]"
  exit 1
}

[[ $# -ge 1 ]] || usage

ACTION="$1"; shift
CLUSTER_NAME="${1:-argocd-ha}"
REGION="${2:-us-east-1}"

K8S_VERSION="1.30"
NODE_TYPE="m5.xlarge"
NODE_COUNT=3
NODE_VOLUME_SIZE=100

required_cmds=(aws eksctl)
[[ "${ACTION}" == "create" || "${ACTION}" == "status" ]] && required_cmds+=(kubectl)
for cmd in "${required_cmds[@]}"; do
  if ! command -v "${cmd}" &>/dev/null; then
    echo "ERROR: ${cmd} not found in PATH" >&2
    exit 1
  fi
done

cmd_create() {
  echo "==> Creating EKS cluster: ${CLUSTER_NAME} in ${REGION}"
  echo "    Kubernetes ${K8S_VERSION}, ${NODE_COUNT}x ${NODE_TYPE}, ${NODE_VOLUME_SIZE}GiB volumes"

  eksctl create cluster \
    --name "${CLUSTER_NAME}" \
    --region "${REGION}" \
    --version "${K8S_VERSION}" \
    --nodegroup-name "argocd-workers" \
    --node-type "${NODE_TYPE}" \
    --nodes "${NODE_COUNT}" \
    --nodes-min "${NODE_COUNT}" \
    --nodes-max "${NODE_COUNT}" \
    --node-volume-size "${NODE_VOLUME_SIZE}" \
    --managed \
    --with-oidc \
    --ssh-access=false \
    --asg-access

  echo "==> Cluster ready"
  kubectl get nodes -o wide
}

cmd_delete() {
  echo "==> This will DELETE cluster '${CLUSTER_NAME}' in ${REGION} and all associated resources"
  if [[ "${FORCE:-false}" != "true" ]]; then
    read -rp "    Continue? [y/N] " confirm
    [[ "${confirm}" =~ ^[Yy]$ ]] || { echo "Aborted."; exit 0; }
  fi

  echo "==> Deleting EKS cluster: ${CLUSTER_NAME}"
  eksctl delete cluster \
    --name "${CLUSTER_NAME}" \
    --region "${REGION}" \
    --wait

  echo "==> Cleanup complete"
}

cmd_status() {
  echo "==> Cluster: ${CLUSTER_NAME} (${REGION})"
  if eksctl get cluster --name "${CLUSTER_NAME}" --region "${REGION}" 2>/dev/null; then
    echo ""
    echo "==> Nodes:"
    kubectl get nodes -o wide 2>/dev/null || echo "    (kubectl not configured for this cluster)"
  else
    echo "    Cluster not found"
  fi
}

case "${ACTION}" in
  create) cmd_create ;;
  delete) cmd_delete ;;
  status) cmd_status ;;
  *)      usage ;;
esac
