#!/bin/bash

#making sure we are in the correct dir
cd "$(dirname "$0")" || exit 1

# username (might be something different for rosa - can be overwritten with export CLUSTER_USER)
export CLUSTER_USER=${CLUSTER_USER:-"kubeadmin"}
export IDP=${IDP:-"kube:admin"}

#check auth state first
echo "Checking cluster authentication..."
if ! oc whoami > /dev/null 2>&1; then
    if [ -n "$OC_API_URL" ] && [ -n "$CLUSTER_PASSWORD" ]; then
        echo "Attempting automated login..."
        oc login "$OC_API_URL" -u "$CLUSTER_USER" -p "$CLUSTER_PASSWORD" --insecure-skip-tls-verify=true
    else
        echo "Error: Not logged in. Missing OC_API_URL or CLUSTER_PASSWORD."
        exit 1
    fi
fi

#find the URLs for console and argocd 
echo "🔍 Discovering component URLs..."
export ARGOCD_URL=$(oc get route openshift-gitops-server -n openshift-gitops -o jsonpath='{"https://"}{.spec.host}')
export CONSOLE_URL=$(oc whoami --show-console)

if [ -z "$ARGOCD_URL" ] || [ -z "$CONSOLE_URL" ]; then
    echo "Error: Could not find Argo CD or Console routes."
    exit 1
fi

echo "OpenShift Console: $CONSOLE_URL"
echo " Argo CD UI:        $ARGOCD_URL"

#clean up any old Playwright state
echo "Getting rid of any old browser sessions..."
rm -f .auth/storageState.json || true 

#run Playwright 
echo " Starting Playwright tests..."
npx playwright test "$@"