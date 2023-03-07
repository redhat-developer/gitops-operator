GITOPS_OPERATOR_VER=99.0.9
ARGOCD_DEX_IMAGE=${ARGOCD_DEX_IMAGE:-registry.redhat.io/openshift-gitops-1/dex-rhel8:${GITOPS_OPERATOR_VER}}
echo "image: $ARGOCD_DEX_IMAGE"
