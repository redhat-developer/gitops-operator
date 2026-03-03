This script may be used to audit the namespace-scoped Roles/RoleBindings that are created by the GitOps operator's 'applications in any namespace/applicationsets in any namespace' features. 
(The 'apps/applications in any namespace' features are not enabled by default. They are enabled via `ArgoCD` CR `.spec.sourceNamespaces` and `.spec.applicationSet.sourceNamespaces`.)

This is a simple script that will look for Roles/RoleBindings across ALL namespaces that meet ALL of the following criteria:
- A) The Role allows access to `argoproj.io/Application` resource
- B) The Role has label `app.kubernetes.io/part-of: argocd`
- C) The RoleBinding references a service-account in another namespace (cross-namespace access)

This criteria ensures that the Role/RoleBinding was likely created by GitOps operator, and that an Argo CD instance on the cluster has (or had) access to that namespace.

## Procedure:
1) Ensure that `jq` and `oc` executables are installed and on path.
2) Ensure that you are logged into cluster via `oc` or `kubectl` CLI.
3) Execute `./audit-operator-roles.sh`
4) Examine the output list of Roles/RoleBindings.

For each Role/RoleBinding that is listed:
- If a Role/RoleBinding is listed, that means another namespace on the cluster has access to the namespace containing the Role/RoleBinding
- Verify that it is correct for the namespace containing the Role/RoleBinding to be accessed by the namespace listed in subject field of the RoleBinding.
	- For example, it is correct if you need an Argo CD instance (installed in the namespace listed in subject field of the RoleBinding) to deploy to the namespace containing the RoleBinding.
    - In contrast, it is likely not correct if there exist Roles/RoleBindings in namespaces that Argo CD is not explicitly deploying to.
- If a Role/RoleBinding exists that is not required, delete them.
	- NOTE: They will be recreated by the operator if there exists an `ArgoCD` CR that references the namespace via the `.spec.sourceNamespaces` or `.spec.applicationSet.sourceNamespaces`.
	- If this is the case, first remove the namespace from these fields, then delete the Role/RoleBinding.
	

Example:

In this example, the script indicates that the `my-argocd` namespace has access to the `app-ns` namespaces via multiple GitOps-operator-created Roles/RoleBindings:

```
=========================================================
SEARCH CRITERIA (Must match ALL):
  1. API/Resource: argoproj.io / applications
  2. Label:        app.kubernetes.io/part-of=argocd
  3. Scope:        Cross-namespace only
=========================================================

Scanning Cluster (this may take a moment)...

Roles with cross-namespace access:
  • Role: app-ns/example-my-argocd-applicationset
  • Role: app-ns/example_app-ns

Cross-namespace bindings detail:
--------------------------------------------------
BINDING:   app-ns / example-my-argocd-applicationset
ROLE REF:  example-my-argocd-applicationset
SUBJECTS (cross-namespace only):
  • ServiceAccount: example-applicationset-controller (ns: my-argocd)

• Namespace my-argocd has access to app-ns

--------------------------------------------------
BINDING:   app-ns / example_app-ns
ROLE REF:  example_app-ns
SUBJECTS (cross-namespace only):
  • ServiceAccount: example-argocd-server (ns: my-argocd)
  • ServiceAccount: example-argocd-application-controller (ns: my-argocd)

• Namespace my-argocd has access to app-ns
```
