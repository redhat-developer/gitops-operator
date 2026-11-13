# Deploy the GitOps agentic integration to OpenShift

Run from this repository. This installs the development manifests directly, as in
our previous deployment; do not run it alongside an OLM-managed GitOps operator.
Both images must be rebuilt when changing the operator and trigger packages.
The deployed Service and Deployment are named `ai-controller`; the binary and
image repository remain `agentic-trigger`.

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

```sh
export DEPLOY_TAG=ai-wiring-20261015-v2
export IMG="quay.io/mangaal/gitops-operator:${DEPLOY_TAG}"
export AGENTIC_TRIGGER_IMG="quay.io/mangaal/agentic-trigger:${DEPLOY_TAG}"
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
make docker-build-no-test IMG="$IMG" CONTAINER_RUNTIME=docker PLATFORM=linux/amd64
make docker-build-agentic-trigger AGENTIC_TRIGGER_IMG="$AGENTIC_TRIGGER_IMG" \
  CONTAINER_RUNTIME=docker PLATFORM=linux/amd64
make docker-push IMG="$IMG" CONTAINER_RUNTIME=docker
make docker-push-agentic-trigger AGENTIC_TRIGGER_IMG="$AGENTIC_TRIGGER_IMG" CONTAINER_RUNTIME=docker
```

`docker-build-no-test` skips unit tests. Run the relevant tests before building;
see [the integration guide](docs/agentic_gitops_integration_mvp.md).

## Deploy and enable

```sh
make deploy IMG="$IMG"
oc -n openshift-gitops-operator set env deployment/openshift-gitops-operator-controller-manager \
  GITOPS_AI_ENABLED=true \
  GITOPS_AI_CONTROLLER_IMAGE="$AGENTIC_TRIGGER_IMG" \
  GITOPS_AI_AGENTIC_RUN_NAMESPACE=openshift-lightspeed \
  GITOPS_AI_CONTROLLER_TLS_ENABLED=true
oc -n openshift-gitops-operator rollout status \
  deployment/openshift-gitops-operator-controller-manager --timeout=300s
oc -n openshift-gitops get argocd openshift-gitops
oc -n openshift-gitops patch argocd openshift-gitops --type=merge \
  -p '{"spec":{"ai":{"troubleshooting":true}}}'
oc -n openshift-gitops rollout status deployment/ai-controller --timeout=300s
```

The trigger is created asynchronously after opting in; if it is not present yet,
check operator logs and retry the rollout command. The new AI switch replaces
`ENABLE_AGENTIC_GITOPS_INTEGRATION` and the GitopsService enablement annotation.
AI troubleshooting also enables the notifications controller. OpenShift supplies
the serving certificate and CA when TLS is enabled.

For the GitOps skills, set an image containing all three paths before testing:

```sh
oc -n openshift-gitops-operator set env deployment/openshift-gitops-operator-controller-manager \
  GITOPS_AI_SKILLS_IMAGE=ghcr.io/redhat-developer/agentic-skills:latest
```

The image must contain `/skills/gitops/argo-cluster-debug`,
`/skills/gitops/argo-knowledge`, and `/skills/gitops/argo-operations`.

## Verify

```sh
oc -n openshift-gitops-operator get deployment openshift-gitops-operator-controller-manager \
  -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'
oc -n openshift-gitops get deployment ai-controller \
  -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'
oc -n openshift-gitops get notificationsconfiguration default-notifications-configuration
oc -n openshift-gitops get cm argocd-notifications-cm \
  -o jsonpath='{.data.trigger\.on-gitops-ai-diagnose}{"\n"}'
oc -n openshift-gitops-operator logs deployment/openshift-gitops-operator-controller-manager \
  -c manager --tail=100
oc -n openshift-gitops logs deployment/ai-controller --tail=100
oc -n openshift-lightspeed get agenticruns.agentic.openshift.io
```

The notification trigger should include health clauses and a `oncePer` key. It
must **not** filter by `creationTimestamp`: the trigger service's queue owns the
application-age and sustained-health grace period (default ten minutes).
Successful pod rollouts verify installation; a completed analysis additionally
requires a working Agent, LLMProvider, credentials, and a failing test Application.
