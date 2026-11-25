# Deploy the GitOps agentic integration to OpenShift

Run from this repository. This installs the development manifests directly, as in
our previous deployment; do not run it alongside an OLM-managed GitOps operator.
Both images must be rebuilt when changing the operator and trigger packages.
The deployed Service and Deployment are named `gitops-ai-controller` and live in
`openshift-gitops-operator`. The container runs `/gitops-ai-controller`; the examples
below use a custom image repository named `agentic-trigger`. On an OLM-managed
cluster, use the CSV patch below in place of `make deploy`.

## Check the target and prerequisites

```sh
export PATH="$HOME/.orbstack/bin:$PATH"
oc whoami
oc whoami --show-server
oc get nodes -o custom-columns=NAME:.metadata.name,ARCH:.status.nodeInfo.architecture
```

The commands below target `linux/amd64`. Use a new tag for each build so rollout
pulls the new images without deleting pods manually. Quay push access and cluster
pull access to both repositories are required.

Replace `<your-quay-namespace>` with a Quay user or organization namespace where
you can push the operator and AI-controller images.

```sh
export DEPLOY_TAG=upstream-skills-20261112
export QUAY_NAMESPACE="<your-quay-namespace>"
export IMG="quay.io/${QUAY_NAMESPACE}/gitops-operator:${DEPLOY_TAG}"
export GITOPS_AI_CONTROLLER_IMG="quay.io/${QUAY_NAMESPACE}/agentic-trigger:${DEPLOY_TAG}"
export GITOPS_AI_SKILLS_IMG="ghcr.io/redhat-developer/agentic-skills:latest"
```

On a fresh cluster, install the Lightspeed Agentic Operator and configure an
analysis Agent and its LLMProvider credentials before testing diagnosis. The
GitOps deployment does not install those APIs or supply model credentials.

```sh
oc get crd agenticruns.agentic.openshift.io analysisresults.agentic.openshift.io
oc get namespace openshift-lightspeed
oc get agents.agentic.openshift.io
```

For a local Lightspeed checkout, its quickstart supports a minimal setup without
the alerts adapter or console plugin:

```sh
oc create namespace openshift-lightspeed --dry-run=client -o yaml | oc apply -f -
bash /Users/mangaal/go/src/github.com/lightspeed-agentic-operator/hack/quickstart/deploy-configmap.sh
bash /Users/mangaal/go/src/github.com/lightspeed-agentic-operator/hack/quickstart/deploy-operator.sh
```

Configure the provider and analysis Agent using that checkout's
`hack/quickstart/examples/` templates and your credential Secret.
These scripts install the Agentic Operator with cluster-admin privileges; the
analysis service account gets read permissions, and execution approval stays manual.

## Build and push

```sh
docker build --platform linux/amd64 -t "$IMG" .
make docker-build-gitops-ai-controller GITOPS_AI_CONTROLLER_IMG="$GITOPS_AI_CONTROLLER_IMG" \
  CONTAINER_RUNTIME=docker PLATFORM=linux/amd64
make docker-push IMG="$IMG" CONTAINER_RUNTIME=docker
make docker-push-gitops-ai-controller GITOPS_AI_CONTROLLER_IMG="$GITOPS_AI_CONTROLLER_IMG" \
  CONTAINER_RUNTIME=docker
```

Run the relevant tests before building these development images.
See [the integration guide](docs/ai-assisted-troubleshooting-with-lightspeed.md)
for feature enablement and configuration.

## Deploy and enable

```sh
make deploy IMG="$IMG"
oc -n openshift-gitops-operator set env deployment/openshift-gitops-operator-controller-manager \
  GITOPS_AI_ENABLED=true \
  GITOPS_AI_CONTROLLER_IMAGE="$GITOPS_AI_CONTROLLER_IMG" \
  GITOPS_AI_AGENTIC_RUN_NAMESPACE=openshift-lightspeed \
  GITOPS_AI_CONTROLLER_TLS_ENABLED=true
oc -n openshift-gitops-operator rollout status \
  deployment/openshift-gitops-operator-controller-manager --timeout=300s
oc -n openshift-gitops get argocd openshift-gitops
oc -n openshift-gitops patch argocd openshift-gitops --type=merge \
  -p '{"spec":{"ai":{"troubleshooting":true}}}'
oc -n openshift-gitops-operator rollout status deployment/gitops-ai-controller --timeout=300s
```

On a cluster where the operator came from OLM (a dev CatalogSource and
Subscription) `make deploy` fails on field ownership: OLM owns the manager
image. Patch the CSV instead and let OLM roll the Deployment:

```sh
GITOPS_CSV=$(oc -n openshift-gitops-operator get subscription gitops-operator -o jsonpath='{.status.installedCSV}')
oc -n openshift-gitops-operator patch csv "$GITOPS_CSV" --type=json -p "$(jq -n \
  --arg img "$IMG" --arg ai "$GITOPS_AI_CONTROLLER_IMG" --arg sk "$GITOPS_AI_SKILLS_IMG" '[
    {op:"replace", path:"/spec/install/spec/deployments/0/spec/template/spec/containers/0/image", value:$img},
    {op:"add", path:"/spec/install/spec/deployments/0/spec/template/spec/containers/0/env/-", value:{name:"GITOPS_AI_ENABLED",value:"true"}},
    {op:"add", path:"/spec/install/spec/deployments/0/spec/template/spec/containers/0/env/-", value:{name:"GITOPS_AI_CONTROLLER_IMAGE",value:$ai}},
    {op:"add", path:"/spec/install/spec/deployments/0/spec/template/spec/containers/0/env/-", value:{name:"GITOPS_AI_SKILLS_IMAGE",value:$sk}}]')"
```

The trigger is created asynchronously after opting in; if it is not present yet,
check operator logs and retry the rollout command. The new AI switch replaces
`ENABLE_AGENTIC_GITOPS_INTEGRATION` and the GitopsService enablement annotation.
AI troubleshooting also enables the notifications controller. OpenShift supplies
the serving certificate and CA when TLS is enabled.

For the GitOps skills, set the image before testing:

```sh
oc -n openshift-gitops-operator set env deployment/openshift-gitops-operator-controller-manager \
  GITOPS_AI_SKILLS_IMAGE="$GITOPS_AI_SKILLS_IMG"
```

No skill-path export is needed for the defaults. The controller uses
`/skills/gitops/argo-cluster-debug` and `/skills/gitops/argo-knowledge` when
`GITOPS_AI_SKILLS_PATHS` is unset or blank. Both directories must exist in the
selected image.

Set `GITOPS_AI_SKILLS_PATHS` only to select a different comma-separated list of
paths that exist in your image; see the [configuration reference](docs/ai-assisted-troubleshooting-with-lightspeed.md#configuration-reference).
If a previous deployment configured an override, remove that environment entry
from its Subscription or CSV/Deployment configuration to restore the defaults.
Unsetting the variable in your terminal alone does not change a deployed operator.

## Verify

```sh
oc -n openshift-gitops-operator get deployment openshift-gitops-operator-controller-manager \
  -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'
oc -n openshift-gitops-operator get deployment gitops-ai-controller \
  -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'
oc -n openshift-gitops get notificationsconfiguration default-notifications-configuration
oc -n openshift-gitops get cm argocd-notifications-cm \
  -o jsonpath='{.data.trigger\.on-gitops-ai-diagnose}{"\n"}'
oc -n openshift-gitops-operator logs deployment/openshift-gitops-operator-controller-manager \
  -c manager --tail=100
oc -n openshift-gitops-operator logs deployment/gitops-ai-controller --tail=100
oc -n openshift-lightspeed get agenticruns.agentic.openshift.io
```

The notification trigger should include health clauses and a `oncePer` key. It
must **not** filter by `creationTimestamp`: the trigger service's queue owns the
application-age and sustained-health grace period (default ten minutes). Set
`GITOPS_AI_DIAGNOSIS_UNHEALTHY_DURATION` on the operator to shorten that wait
while testing; the operator stamps the value onto the `gitops-ai-controller`
Deployment, which restarts to pick it up.
Successful pod rollouts verify installation; a completed analysis additionally
requires a working Agent, LLMProvider, credentials, and a failing test Application.
