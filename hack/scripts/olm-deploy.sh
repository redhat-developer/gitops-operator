#!/usr/bin/env bash
# https://github.com/olivergondza/bash-strict-mode
set -eEuo pipefail
trap 's=$?; echo >&2 "$0: Error on line "$LINENO": $BASH_COMMAND"; exit $s' ERR

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null && pwd)"

SUBSCRIPTION_NAME="gitops-operator"
OPERATOR_NAMESPACE="openshift-gitops-operator"
VERSION="$(git describe --tags --dirty | sed 's/^v//')-$(date '+%Y%m%d-%H%M%S')"

function build_bundles() {
    export VERSION
    make build docker-build docker-push bundle bundle-build bundle-push catalog-build catalog-push
}

function install_catalog_source() {
    oc apply -f - <<EOF
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
    name: devel-gitops-service-source
    namespace: openshift-marketplace
spec:
    displayName: 'GITOPS DEVEL'
    publisher: 'GITOPS DEVEL'
    sourceType: grpc
    image: '${IMAGE}-catalog:v${VERSION}'
EOF

    local yaml=''
    for i in $(seq 1 20); do
        echo >&2 "Waiting for catalog source to be ready... ($i)"
        yaml=$(oc get -o yaml CatalogSource devel-gitops-service-source -n openshift-marketplace --ignore-not-found)
        if [ "$(yq '.status.connectionState.lastObservedState' <<< "$yaml")" == "READY" ]; then
            echo >&2  "Catalog source is ready"
            return 0
        fi

        sleep 5
    done

    echo >&2 "Timeout waiting for catalog source to be ready. Current state:"
    echo >&2 "$yaml"
    return 1
}

function delete_operator() {
    oc delete subscription "$SUBSCRIPTION_NAME" -n "$OPERATOR_NAMESPACE" --ignore-not-found
    readarray -t ogs < <(oc get operatorgroup -n "$OPERATOR_NAMESPACE" -o name --ignore-not-found)
    if [[ ${#ogs[@]} -gt 0 ]]; then
        oc delete -n "$OPERATOR_NAMESPACE" "${ogs[@]}"
    fi
    oc delete csv -n "$OPERATOR_NAMESPACE" -l "operators.coreos.com/${SUBSCRIPTION_NAME}.${OPERATOR_NAMESPACE}" --ignore-not-found

    readarray -t pods < <(oc get pod -n "$OPERATOR_NAMESPACE" -o name --ignore-not-found)
    if [[ ${#pods[@]} -gt 0 ]]; then
        # Pods might stop existing before query and delete, so ignoring not found ones
        oc delete -n "$OPERATOR_NAMESPACE" "${pods[@]}" --ignore-not-found
    fi
    oc delete ns "$OPERATOR_NAMESPACE" --ignore-not-found
}

function install_operator() {
    oc create ns "$OPERATOR_NAMESPACE"
    oc apply -f - <<EOF
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: openshift-gitops-operator-devel
  namespace: $OPERATOR_NAMESPACE
spec:
  upgradeStrategy: Default
---
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: $SUBSCRIPTION_NAME
  namespace: $OPERATOR_NAMESPACE
spec:
  channel: latest
  name: $SUBSCRIPTION_NAME
  installPlanApproval: Automatic
  source: devel-gitops-service-source
  sourceNamespace: openshift-marketplace
EOF

    local out=''
    for i in $(seq 1 20); do
        echo >&2 "Waiting for operator to start... ($i)"
        out=$(oc get pods -n "$OPERATOR_NAMESPACE" --ignore-not-found --no-headers)
        echo "$out"
        if [[ "$out" =~ "Running" ]]; then
            echo >&2 "Operator is ready"
            return 0
        fi
        sleep 5
    done

    echo >&2 "Timeout waiting for operator to start. Current state:"
    echo >&2 "$out"
    oc events -n "$OPERATOR_NAMESPACE" >&2
    return 1
}

function main() {
    if [ ! -v IMAGE ]; then
        echo >&2 "Variable IMAGE not specified"
        exit 1
    fi

    echo >&2 "Deploying version $VERSION"

    build_bundles
    install_catalog_source
    delete_operator
    install_operator
}

main "$@"
