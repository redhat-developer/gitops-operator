#!/bin/bash

if [ -f .env ]; then
  echo "Loading variables from .env file..."
  set -a  #export all variables
  source .env
  set +a  # stop automatically exporting
fi

#making sure we are in the correct dir
cd "$(dirname "$0")" || exit 1

# username (might be something different for rosa - can be overwritten with export CLUSTER_USER)
export CLUSTER_USER=${CLUSTER_USER:-"kubeadmin"}
export IDP=${IDP:-"kube:admin"}

#check auth state first
echo "Syncing CLI context..."
if [ -n "$OC_API_URL" ] && [ -n "$CLUSTER_PASSWORD" ]; then
    # If variables exist, FORCE the CLI to match them so there is no cross-cluster confusion
    echo "Logging into $OC_API_URL..."
    oc login "$OC_API_URL" -u "$CLUSTER_USER" -p "$CLUSTER_PASSWORD" --insecure-skip-tls-verify=true > /dev/null 2>&1
    
    if [ $? -ne 0 ]; then
        echo "Error: Failed to log into the cluster. Please check the credentials in your .env file."
        exit 1
    fi
elif ! oc whoami > /dev/null 2>&1; then
    # If variables don't exist AND we aren't logged in, fail out
    echo "Error: Not logged in. Missing OC_API_URL or CLUSTER_PASSWORD."
    exit 1
else
    # If variables don't exist but we ARE logged in locally, just use the current session
    echo "No .env credentials found. Using existing oc CLI session..."
fi

#find the URLs for console and argocd 
echo "Discovering component URLs..."
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