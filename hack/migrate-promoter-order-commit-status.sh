#!/usr/bin/env bash
# Migrates PromotionStrategies that lack required spec.orderCommitStatusRef after
# upgrading to gitops-promoter v0.39+ / OpenShift GitOps with promoter v0.49+.
#
# For each PromotionStrategy missing orderCommitStatusRef.name this script:
#   1. Creates a DependentsSuccessfulCommitStatus (same name/namespace) when absent
#   2. Patches the PromotionStrategy with orderCommitStatusRef pointing at that gate
#   3. Removes legacy ordering keys from proposedCommitStatuses when present
#      (promoter-previous-environment, dependents-successful) — the PS controller
#      injects the gate key from orderCommitStatusRef
#
# Live-cluster patches are not enough when strategies are GitOps-managed: update the
# source manifests as well (see docs/usage/gitops-promoter.md) or Argo CD will revert.
#
# Usage:
#   ./hack/migrate-promoter-order-commit-status.sh [--dry-run] [-n NAMESPACE | --all-namespaces]
#
# Requires: kubectl (or oc), jq

set -euo pipefail

KUBECTL="${KUBECTL:-kubectl}"
if ! command -v "${KUBECTL}" >/dev/null 2>&1 && command -v oc >/dev/null 2>&1; then
  KUBECTL=oc
fi

DRY_RUN=0
NAMESPACE=""
ALL_NAMESPACES=0

usage() {
  sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
  exit "${1:-0}"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) DRY_RUN=1; shift ;;
    -n|--namespace) NAMESPACE="${2:-}"; shift 2 ;;
    --all-namespaces|-A) ALL_NAMESPACES=1; shift ;;
    -h|--help) usage 0 ;;
    *) echo "unknown argument: $1" >&2; usage 1 ;;
  esac
done

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required" >&2
  exit 1
fi

list_args=()
if [[ "${ALL_NAMESPACES}" -eq 1 || -z "${NAMESPACE}" ]]; then
  list_args+=(--all-namespaces)
else
  list_args+=(-n "${NAMESPACE}")
fi

echo "Listing PromotionStrategies (${list_args[*]})..."
strategies="$("${KUBECTL}" get promotionstrategies.promoter.argoproj.io "${list_args[@]}" -o json)"

count="$(jq '.items | length' <<<"${strategies}")"
if [[ "${count}" -eq 0 ]]; then
  echo "No PromotionStrategies found."
  exit 0
fi

migrated=0
skipped=0

while IFS= read -r item; do
  name="$(jq -r '.metadata.name' <<<"${item}")"
  ns="$(jq -r '.metadata.namespace' <<<"${item}")"
  ref_name="$(jq -r '.spec.orderCommitStatusRef.name // empty' <<<"${item}")"

  if [[ -n "${ref_name}" ]]; then
    echo "skip ${ns}/${name}: orderCommitStatusRef.name already set (${ref_name})"
    skipped=$((skipped + 1))
    continue
  fi

  gate_name="${name}"
  echo "migrate ${ns}/${name}: DependentsSuccessfulCommitStatus/${gate_name} + orderCommitStatusRef"

  dscs_yaml="$(cat <<EOF
apiVersion: promoter.argoproj.io/v1alpha1
kind: DependentsSuccessfulCommitStatus
metadata:
  name: ${gate_name}
  namespace: ${ns}
spec:
  key: dependents-successful
  promotionStrategyRef:
    name: ${name}
EOF
)"

  # Drop legacy ordering keys; leave other proposedCommitStatuses intact.
  cleaned_pcs="$(jq -c '
    (.spec.proposedCommitStatuses // [])
    | map(select(.key != "promoter-previous-environment" and .key != "dependents-successful"))
  ' <<<"${item}")"

  patch="$(jq -nc --arg gate "${gate_name}" --argjson pcs "${cleaned_pcs}" '
    {
      "spec": {
        "orderCommitStatusRef": {
          "group": "promoter.argoproj.io",
          "kind": "DependentsSuccessfulCommitStatus",
          "name": $gate
        },
        "proposedCommitStatuses": $pcs
      }
    }
  ')"

  if [[ "${DRY_RUN}" -eq 1 ]]; then
    echo "--- dry-run DependentsSuccessfulCommitStatus ---"
    echo "${dscs_yaml}"
    echo "--- dry-run patch PromotionStrategy ${ns}/${name} ---"
    echo "${patch}" | jq .
    migrated=$((migrated + 1))
    continue
  fi

  if ! "${KUBECTL}" get dependentssuccessfulcommitstatuses.promoter.argoproj.io -n "${ns}" "${gate_name}" >/dev/null 2>&1; then
    echo "${dscs_yaml}" | "${KUBECTL}" apply -f -
  else
    echo "  DependentsSuccessfulCommitStatus/${gate_name} already exists"
  fi

  "${KUBECTL}" patch promotionstrategies.promoter.argoproj.io "${name}" -n "${ns}" --type merge -p "${patch}"
  migrated=$((migrated + 1))
done < <(jq -c '.items[]' <<<"${strategies}")

echo
echo "Done. migrated=${migrated} skipped=${skipped} dry_run=${DRY_RUN}"
echo
echo "IMPORTANT: If these PromotionStrategies are managed from Git, update the GitOps"
echo "source manifests the same way (add DependentsSuccessfulCommitStatus + orderCommitStatusRef),"
echo "then sync. See docs/usage/gitops-promoter.md#migrating-ordercommitstatusref."
