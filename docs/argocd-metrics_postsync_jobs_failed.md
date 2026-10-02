# Deploying kube-state-metrics for Monitoring Argo CD PostSync hook failures

This guide shows how to deploy Custom Resource State metrics for Argo CD `Application` resources. All assets are standard Kubernetes resources that you can deploy directly using `oc apply`, Helm, Kustomize, or any GitOps tool.

---
## Prerequisites
1. **A running Argo CD instance** (managed by any method — the OpenShift GitOps operator, upstream Helm charts, etc.)
2. **kube-state-metrics not already deployed** (or using a dedicated KSM instance just for Applications)
3. **Prometheus and the monitoring stack** (ServiceMonitor and PrometheusRule CRDs must be available; this is typically provided by the Prometheus Operator)
4. **oc access** to the cluster with permission to create RBAC, ConfigMaps, Deployments, and custom resources

---

## Architecture

The manual approach deploys:

| Component | Purpose |
|---|---|
| **ServiceAccount** | Identity for the kube-state-metrics pod |
| **Role + RoleBinding** | Namespace-scoped permissions to `list`/`watch` `applications.argoproj.io` |
| **ClusterRole + ClusterRoleBinding** | Cluster-scoped permission to discover the Application CRD schema |
| **ConfigMap** | Embeds the Custom Resource State configuration |
| **Deployment** | Runs `kube-state-metrics` with the CRS config |
| **Service** | Exposes `/metrics` on port 8080 |
| **ServiceMonitor** | Registers the Service with Prometheus |
| **PrometheusRule** | Ships two alerts and one recording rule |

**Scope clarification:** The Role and RoleBinding grant namespace-scoped access to list and watch `Application` resources within the target namespace (default: `openshift-gitops`). The ClusterRole and ClusterRoleBinding grant cluster-scoped access to discover the Application CRD schema. All Kubernetes objects (ServiceAccount, ConfigMap, Deployment, Service, ServiceMonitor, PrometheusRule) are created in a single namespace, and metrics are exported only for `Application` resources in that namespace.

---

## Step-by-step Deployment

### 1. Prepare the Namespace

Ensure the namespace exists and is labeled so Prometheus picks up the `ServiceMonitor` and `PrometheusRule`:

```bash
NAMESPACE=openshift-gitops

# Create the namespace if it doesn't exist
oc create namespace $NAMESPACE --dry-run=client -o yaml | oc apply -f -

# Label it for platform monitoring (if on OpenShift)
oc label namespace $NAMESPACE openshift.io/cluster-monitoring=true --overwrite
```

For **non-OpenShift clusters** using user-workload or a separate Prometheus instance, adjust the label or ensure your `ServiceMonitor` and `PrometheusRule` are scraped by your Prometheus.

### 2. Create the RBAC Objects

Save the following as `rbac.yaml`:

```yaml
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: argocd-application-ksm
  namespace: openshift-gitops

---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: argocd-application-ksm
  namespace: openshift-gitops
rules:
  - apiGroups: ["argoproj.io"]
    resources: ["applications"]
    verbs: ["list", "watch"]

---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: argocd-application-ksm
  namespace: openshift-gitops
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: argocd-application-ksm
subjects:
  - kind: ServiceAccount
    name: argocd-application-ksm
    namespace: openshift-gitops

---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: argocd-application-ksm
rules:
  - apiGroups: ["apiextensions.k8s.io"]
    resources: ["customresourcedefinitions"]
    verbs: ["list", "watch"]

---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: argocd-application-ksm
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: argocd-application-ksm
subjects:
  - kind: ServiceAccount
    name: argocd-application-ksm
    namespace: openshift-gitops
```

Apply it:

```bash
oc apply -f rbac.yaml
```

### 3. Create the Custom Resource State Configuration

Save the following as `ksm-config.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argocd-application-ksm
  namespace: openshift-gitops
data:
  custom-resource-state.yaml: |
    kind: CustomResourceStateMetrics
    spec:
      resources:
        - groupVersionKind:
            group: argoproj.io
            version: v1alpha1
            kind: Application
          metricNamePrefix: kube_customresource
          labelsFromPath:
            name: [metadata, name]
            namespace: [metadata, namespace]
            project: [spec, project]
          metrics:
            - name: argocd_application_operation_phase
              help: Argo CD Application last operation phase
              each:
                type: StateSet
                stateSet:
                  labelName: phase
                  path: [status, operationState, phase]
                  list: [Running, Succeeded, Failed, Error, Terminating]

            - name: argocd_application_sync_hook_phase
              help: Per-resource hook phase in last sync result
              each:
                type: Info
                info:
                  path: [status, operationState, syncResult, resources]
                  labelsFromPath:
                    hook_type: [hookType]
                    hook_phase: [hookPhase]
                    sync_phase: [syncPhase]
                    resource_kind: [kind]
                    resource_name: [name]
                    resource_namespace: [namespace]

            - name: argocd_application_condition
              help: Application status.conditions projected by kube-state-metrics
              each:
                type: Info
                info:
                  path: [status, conditions]
                  labelsFromPath:
                    condition: [type]
```

Apply it:

```bash
oc apply -f ksm-config.yaml
```

### 4. Deploy kube-state-metrics

Save the following as `ksm-deployment.yaml`:

```yaml
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: argocd-application-ksm
  namespace: openshift-gitops
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: argocd-application-ksm
  template:
    metadata:
      labels:
        app.kubernetes.io/name: argocd-application-ksm
        app.kubernetes.io/component: metrics
        app.kubernetes.io/part-of: argocd
    spec:
      serviceAccountName: argocd-application-ksm
      containers:
        - name: kube-state-metrics
          image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.15.0
          args:
            - --port=8080
            - --custom-resource-state-config-file=/etc/kube-state-metrics/custom-resource-state.yaml
            - --custom-resource-state-only=true
            - --namespaces=openshift-gitops
          ports:
            - name: metrics
              containerPort: 8080
              protocol: TCP
          resources:
            requests:
              cpu: 10m
              memory: 32Mi
            limits:
              memory: 128Mi
          volumeMounts:
            - name: ksm-config
              mountPath: /etc/kube-state-metrics
              readOnly: true
          readinessProbe:
            httpGet:
              path: /metrics
              port: metrics
            initialDelaySeconds: 5
            periodSeconds: 15
      volumes:
        - name: ksm-config
          configMap:
            name: argocd-application-ksm

---
apiVersion: v1
kind: Service
metadata:
  name: argocd-application-ksm
  namespace: openshift-gitops
spec:
  selector:
    app.kubernetes.io/name: argocd-application-ksm
  ports:
    - name: metrics
      port: 8080
      targetPort: metrics
      protocol: TCP
  type: ClusterIP
```

Apply it:

```bash
oc apply -f ksm-deployment.yaml
```

Verify the pod is running:

```bash
oc get pods -n openshift-gitops -l app.kubernetes.io/name=argocd-application-ksm
oc logs -n openshift-gitops -l app.kubernetes.io/name=argocd-application-ksm --tail=30
```

The logs should show:

```text
I ... "Using namespaces" nameSpaces=["openshift-gitops"]
I ... "Custom resource state added metrics" familyNames=["kube_customresource_argocd_application_operation_phase","kube_customresource_argocd_application_sync_hook_phase","kube_customresource_argocd_application_condition"]
I ... "Active resources" activeStoreNames="argoproj.io/v1alpha1, Resource=applications"
```

### 5. Register with Prometheus

Save the following as `monitoring.yaml`:

```yaml
---
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: argocd-application-ksm
  namespace: openshift-gitops
  labels:
    release: prometheus-operator
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: argocd-application-ksm
  endpoints:
    - port: metrics
      interval: 30s

---
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: argocd-application-ksm-alerts
  namespace: openshift-gitops
spec:
  groups:
    - name: argocd.hooks
      rules:
        - alert: ArgoCDApplicationPostSyncHookFailed
          expr: kube_customresource_argocd_application_sync_hook_phase{namespace="openshift-gitops",hook_type="PostSync",hook_phase=~"Failed|Error"} == 1
          for: 2m
          labels:
            severity: critical
          annotations:
            summary: "PostSync hook failed for {{ $labels.name }}"
            description: >-
              Hook {{ $labels.resource_namespace }}/{{ $labels.resource_name }} failed.
              Inspect the Application operation view and the Job logs.

        - alert: ArgoCDApplicationOperationFailed
          expr: kube_customresource_argocd_application_operation_phase{namespace="openshift-gitops",phase=~"Failed|Error"} == 1
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: "Application {{ $labels.name }} last operation {{ $labels.phase }}"

        - record: argocd_app_condition
          expr: kube_customresource_argocd_application_sync_hook_phase{namespace="openshift-gitops",hook_type="PostSync",hook_phase=~"Failed|Error"}
          labels:
            condition: Failed
```

Apply it:

```bash
oc apply -f monitoring.yaml
```

Verify the `ServiceMonitor` is picked up by Prometheus:

```bash
# On OpenShift, port-forward the platform Prometheus:
oc port-forward -n openshift-monitoring svc/prometheus-k8s 9090:9091 &
sleep 3

# The platform Prometheus serves HTTPS on 9091 and requires a bearer token:
TOKEN=$(oc whoami -t)

# Query the metrics:
curl -sk -H "Authorization: Bearer $TOKEN" "https://localhost:9090/api/v1/query?query=kube_customresource_argocd_application_operation_phase" | jq .
```

Verify the `PrometheusRule` is loaded:

```bash
curl -sk -H "Authorization: Bearer $TOKEN" https://localhost:9090/api/v1/rules | jq '.data.groups[] | select(.name=="argocd.hooks")'
```

---

## Validation

### 1. Check the metrics directly from the KSM Service

```bash
oc port-forward -n openshift-gitops svc/argocd-application-ksm 8080:8080 &
sleep 2

# List all exported metric families
curl -s http://localhost:8080/metrics | grep "^# HELP kube_customresource"

# Query for a specific Application (if one exists)
curl -s http://localhost:8080/metrics | grep "kube_customresource_argocd_application" | head -20
```

### 2. Create a test Application with a failing PostSync hook

If you don't have a real Application to test, you can create a minimal one:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: test-postsync-fail
  namespace: openshift-gitops
spec:
  project: default
  source:
    repoURL: https://github.com/argoproj/argocd-example-apps.git
    path: guestbook
    targetRevision: HEAD
  destination:
    server: https://kubernetes.default.svc
    namespace: default
  syncPolicy:
    syncOptions:
      - CreateNamespace=true
```

And add a `Job` hook to fail (either embed it in the repo or use a post-sync hook resource).

### 3. Verify the metric appears

```bash
# Query from the KSM Service:
curl -s http://localhost:8080/metrics | grep 'hook_type="PostSync"'

# Query from Prometheus (TOKEN from the previous step):
curl -sk -H "Authorization: Bearer $TOKEN" "https://localhost:9090/api/v1/query?query=kube_customresource_argocd_application_sync_hook_phase%7Bhook_type%3D%22PostSync%22%7D" | jq .

# Check if the alert is firing:
curl -sk -H "Authorization: Bearer $TOKEN" https://localhost:9090/api/v1/rules | jq '.data.groups[] | select(.name=="argocd.hooks") | .rules[] | select(.name=="ArgoCDApplicationPostSyncHookFailed")'
```

---

## Customization

### Different namespace

To monitor Applications in a **different namespace** (e.g., `argocd`), edit the following:

1. **RBAC** — update `namespace:` and `--namespaces=` in Deployment args
2. **ConfigMap** — keep as-is (the config is generic)
3. **ServiceMonitor/PrometheusRule** — update `namespace:` and any `namespace="openshift-gitops"` in alert expressions

Example for namespace `argocd`:

```bash
sed -i 's/openshift-gitops/argocd/g' rbac.yaml ksm-config.yaml ksm-deployment.yaml monitoring.yaml
# Also edit the Deployment's --namespaces argument:
# --namespaces=argocd
```

### Different kube-state-metrics image

If you need a specific KSM version (e.g., for compatibility or features):

```bash
# In ksm-deployment.yaml, change:
# image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.13.0
```

Supported versions: any `v2.x` release. v2.8.0+ has stable Custom Resource State support.

### Modify alert thresholds

Edit the `PrometheusRule` in `monitoring.yaml`:

```yaml
- alert: ArgoCDApplicationPostSyncHookFailed
  expr: kube_customresource_argocd_application_sync_hook_phase{...} == 1
  for: 2m         # <-- change this (e.g., "5m" for longer patience)
```

### Export additional metrics

To add metrics beyond the three included, edit the ConfigMap:

```yaml
metrics:
  - name: argocd_application_my_custom_metric
    help: My custom metric description
    each:
      type: StateSet
      stateSet:
        labelName: my_label
        path: [status, myField]
        list: [value1, value2]
```

Then restart the Deployment for the config to take effect:

```bash
oc rollout restart deployment/argocd-application-ksm -n openshift-gitops
```

---

## Troubleshooting

### KSM pod not running

```bash
oc describe pod -n openshift-gitops -l app.kubernetes.io/name=argocd-application-ksm
oc logs -n openshift-gitops -l app.kubernetes.io/name=argocd-application-ksm
```

Common issues:
- **RBAC:** pod is in pending/evicted state — check `describe` for `is forbidden` errors
- **Config:** pod crashes immediately — check logs for YAML parse errors in the ConfigMap
- **Image:** pod not pulling KSM image — verify the registry is reachable

### ServiceMonitor not picked up

```bash
# Check if the ServiceMonitor exists:
oc get servicemonitor -n openshift-gitops argocd-application-ksm

# On OpenShift, check if Prometheus has RBAC to read the namespace:
oc get rolebinding,clusterrolebinding -A | grep prometheus-k8s
```

### Metrics not appearing in Prometheus

```bash
# Verify the Service exists and is healthy:
oc get svc -n openshift-gitops argocd-application-ksm
oc get endpoints svc/argocd-application-ksm -n openshift-gitops

# Check Prometheus's active targets (TOKEN=$(oc whoami -t):
curl -sk -H "Authorization: Bearer $TOKEN" https://localhost:9090/api/v1/targets | jq '.data.activeTargets[] | select(.labels.job=="argocd-application-ksm")'
```

### Alert not firing

```bash
# Check if the PrometheusRule is loaded:
curl -sk -H "Authorization: Bearer $TOKEN" https://localhost:9090/api/v1/rules | jq '.data.groups[] | select(.name=="argocd.hooks")'

# Check if the alert expression matches anything:
curl -sk -H "Authorization: Bearer $TOKEN" 'https://localhost:9090/api/v1/query?query=kube_customresource_argocd_application_sync_hook_phase{hook_type="PostSync",hook_phase=~"Failed|Error"}' | jq .
```

---

## Uninstallation

To remove all deployed assets:

```bash
oc delete -f rbac.yaml
oc delete -f ksm-config.yaml
oc delete -f ksm-deployment.yaml
oc delete -f monitoring.yaml
```

---

