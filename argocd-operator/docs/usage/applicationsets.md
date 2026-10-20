## Using ApplicationSets

#### Background

Operators may be responsible for hundreds of clusters (including cluster additions and deletions), and thus installing and managing these add-ons must be fully automated. Monorepos present problems with manual effort, and giving users the correct level of access to applications can be a challenge overall. 

A lot of these problems can be solved with the use of the App-of-Apps pattern; however, the App-of-Apps pattern still presents some issues: 
- Users must still maintain a collection of individual Application resources in git
- Because of the repetitive nature of writing these Application specs, users often end up scripting the generation of Application specs
- Because the Application CR exposes sensitive fields such as project, cluster, namespace a high degree of trust is required of developers
- sync and deletion behaviour of a 'parent app' can sometimes produce counterintuitive results on child apps; etc.. 

A better way to solve these problems than using the App-of-Apps pattern is through the use of ApplicationSets.

### Introducing ApplicationSets

ApplicationSets are a separate CRD that allows cluster administrators to define a single ApplicationSet Kubernetes YAML resource that will generate (and update or delete) multiple corresponding Argo CD Application CRs.

ApplicationSets offers a number of improvements via automation. With ApplicationSets you can: 
- automatically deploy to multiple cluster at once, and automatically adapt to the addition/removal of clusters
- handle large deployments of Argo CD Applications from a single mono-repository, automatically responding to the addition/removal of new applications to the repository
- enable development teams to manage large groups of applications securely, via self-service, without cluster administrator review, on a cluster managed via Argo CD.
- have applications managed by the ApplicationSet controller, which can be managed by only a single instance of an ApplicationSet custom resource (CR)... that means no more juggling of large numbers of Argo CD Application objects when targeting multiple clusters/repositories!

When you create, update, or delete an ApplicationSet resource, the ApplicationSet Controller responds by creating, updating, or deleting one or more corresponding Argo CD Application resources.

The ApplicationSet Controller does: 
- create, update, and delete Application resources within the default `gitops-operator` namespace. 
- ensure that the Application resources remain consistent with the defined declarative ApplicationSet resource

The ApplicationSet Controller does NOT:
- Create/modify/delete Kubernetes resources (other than the Application CR)
- Connect to clusters other than the one Argo CD is deployed to
- Interact with namespaces other than the one Argo CD is deployed within
- Manage the application resources of the applications it creates (Argo CD itself is responsible for the actual deployment of the generated child Application resources, such as Deployments, Services, and ConfigMaps)

The ApplicationSet Controller can thus be thought of as an Application 'factory', taking an ApplicationSet resource as input, and outputting one or more Argo CD Application resources that correspond to the parameters of that set.

Creation, update, or deletion of ApplicationSets will have a direct effect on the Applications present in the Argo CD namespace. Likewise, cluster events (the addition/deletion of Argo CD cluster secrets, when using Cluster generator), or changes in Git (when using Git generator), will be used as input to the ApplicationSet controller in constructing Application resources.

Because it is such a powerful tool, **only admins may create, update, and delete ApplicationSets.**

Note: All ApplicationSet resources and the ApplicationSet controller must be installed in the same namespace as Argo CD. ApplicationSet resources in a different namespace will be ignored.

### ApplicationSets Getting Started and Installation

In all current supported versions of GitOps Operator, there is no extra installation needed to install the ApplicationSet controller with your existing GitOps Operator Installation. 

The name of the ApplicationSet Controller that installs as part of the OpenShift GitOps Operator is `openshift-gitops-applicationset-controller`. 

The following options are available for the Argo CD ApplicationSet Controller in the ArgoCD CR under `.spec.applicationSet`: 

| Name    | Default | Description |
| -------- | ------- |------- |
| env  | Empty   | Env lets you specify environment for applicationSet controller pods |
| extraCommandArgs | Empty     | ExtraCommandArgs allows users to pass command line arguments to ApplicationSet controller. They get added to default command line arguments provided by the operator. Please note that the command line arguments provided as part of ExtraCommandArgs will not overwrite the default command line arguments. |
| image    | Empty  | Image is the Argo CD ApplicationSet image (optional) |
| logLevel   | ArgoCDDefaultLogLevel  | The log level to be used by the ArgoCD Application Controller component. Valid options are `debug` , `info` , `error` , and `warn`. |
| resources   | Empty  | Resources defines the Compute Resources required by the container for ApplicationSet. |
| version   | Empty  | Version is the Argo CD ApplicationSet image tag.(optional) |
| webhookServer    | Empty  | WebhookServerSpec defines the options for the ApplicationSet Webhook Server component.|
| scmRootCAConfigMap    | Empty  | SCMRootCAConfigMap is the name of the config map that stores the Gitlab SCM Provider's TLS certificate which will be mounted on the ApplicationSet Controller (optional)|
<!--- Add "enabled" option once it's functional- either v1.11.1 or v1.12? -->
<!--- | enabled    | true  | Enabled is the flag to enable the Application Set Controller during ArgoCD installation. (optional) | -->


**ApplicationSet ExtraCommandArgs:**

The example shows how a user can add command arguments to the ApplicationSet Controller. 

```
apiVersion: argoproj.io/v1beta1
kind: ArgoCD
metadata:
  name: example
spec:
  applicationSet:
    extraCommandArgs:
      - --foo
      - bar
```

The following describes all the available fields of an ApplicationSet: 
```
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: hello-world-appset
  namespace: openshift-gitops
spec:
  # See docs for available generators and their specs.
  generators:
  - list:
      elements:
      - cluster: https://kubernetes.default.svc
  # Determines whether go templating will be used in the `template` field below.
  goTemplate: false
  # Optional list of go templating options, see https://pkg.go.dev/text/template#Template.Option
  # This is only relevant if `goTemplate` is true
  goTemplateOptions: ["missingkey="]
  # These fields are identical to the Application spec.
  template:
    metadata:
      name: hello-world-app
    spec:
      project: my-project
  # This sync policy pertains to the ApplicationSet, not to the Applications it creates.
  syncPolicy:
    # Determines whether the controller will delete Applications when an ApplicationSet is deleted.
    preserveResourcesOnDeletion: false
  # Alpha feature to determine the order in which ApplicationSet applies changes.
  strategy:
  # This field lets you define fields which should be ignored when applying Application resources. This is helpful if you
  # want to use ApplicationSets to create apps, but also want to allow users to modify those apps without having their
  # changes overwritten by the ApplicationSet.
  ignoreApplicationDifferences:
  - jsonPointers:
    - /spec/source/targetRevision
  - name: some-app
    jqPathExpressions:
    - .spec.source.helm.values
```

### ApplicationSet Generators

Generators are responsible for generating parameters, which are then rendered into the template fields of the ApplicationSet resource.

There are currently (as of GitOps v1.11.0) 9 types of Generators:
- List Generator: The List generator allows you to target Argo CD Applications to clusters based on a fixed list of any chosen key/value element pairs.
- Cluster Generator: The Cluster generator allows you to target Argo CD Applications to clusters, based on the list of clusters defined within (and managed by) Argo CD (which includes automatically responding to cluster addition/removal events from Argo CD).
- Git Generator: The Git generator allows you to create Applications based on files within a Git repository, or based on the directory structure of a Git repository.
- Matrix Generator: The Matrix generator may be used to combine the generated parameters of two separate generators.
- Merge Generator: The Merge generator may be used to merge the generated parameters of two or more Generators. Additional generators can override the values of the base generator.
- SCM Provider Generator: The Source Code Management (SCM) Provider generator uses the API of an SCM provider (eg GitHub) to automatically discover repositories within an organization.
- Pull Request Generator: The Pull Request generator uses the API of an SCMaaS provider (eg GitHub) to automatically discover open pull requests within an repository.
- Cluster Decision Resource Generator: The Cluster Decision Resource generator is used to interface with Kubernetes custom resources that use custom resource-specific logic to decide which set of Argo CD clusters to deploy to.
- Plugin Generator: The Plugin generator makes RPC HTTP requests to provide parameters.

NOTE: Clusters that you put into any generators must already be defined in Argo CD in order to use them with ApplicationSets. The ApplicationSet controller does not create clusters within Argo CD as it does not have credentials to do so. 


### ApplicationSet Resource Updating, Pruning & Deletion

The ApplicationSet Controller creates/modifies/deletes Argo CD Application resources, based on the contents of the ApplicationSet CR.

Once again, please note that **only admins may create, update, and delete ApplicationSets.**

All Argo CD Application resources created by the ApplicationSet controller (from an ApplicationSet) will contain:
- A `.metadata.ownerReferences` reference back to the parent ApplicationSet resource
  - This means that when the ApplicationSet is deleted, the Applications will automatically be deleted.
- An Argo CD `resources-finalizer.argocd.argoproj.io` finalizer in `.metadata.finalizers` of the Application (only applies if `.syncPolicy.preserveResourcesOnDeletion` is set to false)
  - This means that when the Application is deleted, all of the child resources of that Application will be deleted.

When an applicationSet is deleted, the following will occur:
1. The ApplicationSet itself is deleted
2. Any Applications that were created from the now-deleted ApplicationSet will be deleted (or Applications that have an owner-reference to the deleted ApplicationSet)
3. Any deployed resources (Deployments, Services, Configmaps, etc), from the deleted Applications will also be deleted. 

However, the ApplicationSet controller has settings that can limit its ability to make changes to Applications it has generated, for example, preventing the controller from deleting child Applications.

Some of these settings are…

#### 1. Dry-run: Prevent ApplicationSet from creating/modifying/deleting all Applications

To prevent the ApplicationSet controller from creating, modifying, or deleting any Application resources, you may enable dry-run mode. This essentially switches the controller into a "read only" mode, where the controller reconcile loop will run, but no resources will be modified.

  To enable dry-run: 

  1. Add `--dryrun true` to the ApplicationSet Deployment's container launch parameters with `kubectl edit deployment/openshift-gitops-applicationset-controller -n openshift-gitops`
  2. Locate the `.spec.template.spec.containers[0].command` field, and add the required parameter(s):
  ```
  spec:
    # (...)
  template:
    # (...)
    spec:
      containers:
      - command:
        - entrypoint.sh
        - openshift-gitops-applicationset-controller
        # Insert new parameters here, for example:
        # --dryrun true
    # (...)
  ```
#### 2. Using Managed Application Modification Policies

The ApplicationSet Controller spec allows you to add a `syncPolicy.applicationsSync` field, like so: 
```
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
spec:
  # (...)
  syncPolicy:
    applicationsSync: create-only 
```
The options for the `applicationsSync` field are:
- create-only: Prevents ApplicationSet controller from modifying or deleting Applications.
- create-update: Prevents ApplicationSet controller from deleting Applications. Update is allowed.
- create-delete: Prevents ApplicationSet controller from modifying Applications. Delete is allowed.
- sync (default): Update and Delete are allowed.

You could also do the same via the `--policy` addition to the ApplicationSet controller Deployment.
Run `kubectl edit deployment/gitops-applicationset-controller -n openshift-gitops` and then edit the `.spec.template.spec.containers[0].command` to have the following: 
```
spec:
    # (...)
  template:
    # (...)
    spec:
      containers:
      - command:
        - entrypoint.sh
        - openshift-gitops-applicationset-controller
        # Insert new parameters here, for example:
        # --policy create-update
    # (...)
```
When `--policy` is set as it is above, it does take precedence over the previously mentioned `.spec.syncPolicy.applicationsSync` field. 

Note: this does not prevent deletion of Applications if the ApplicationSet itself is deleted. 

#### 3. Ignoring certain changes to Applications

The ApplicationSet spec has an `ignoreApplicationDifferences` field which allows you to specify which fields of the ApplicationSet should be ignored when comparing Applications. Multiple ignore rules can be used and each ignore rule may specify a list of either `jsonPointers` or `jqPathExpressions` to ignore. You may optionally also specify a name to apply the ignore rule to a specific Application, or omit the name to apply the ignore rule to all Applications.

```
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
spec:
  ignoreApplicationDifferences:
    - jsonPointers:
        - /spec/source/targetRevision
    - name: some-app
      jqPathExpressions:
        - .spec.source.helm.values
```
**Temporarily Ignoring Certain Changes to Applications**

If you have an ApplicationSet that is configured to automatically sync Applications, there may come a time when you may want to temporarily disable auto-sync for a specific Application. You can do this by adding an ignore rule for the spec.syncPolicy.automated field:
```
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
spec:
  ignoreApplicationDifferences:
    - jsonPointers:
        - /spec/syncPolicy
```

**Limitations of `ignoreApplicationDifferences`: **

When the ignored field is part of a list, such as when you have an Application with multiple sources, changes to other fields or in other sources will cause the entire list to be replaced, and the related fields will be reset to the value defined in the ApplicationSet. This is due to how ApplicationSets generate MergePatches, which state that “existing lists will be completely replaced by new lists" when there is a change to the list. So in the case of having multiple sources, if we have an ApplicationSet that looks like this: 
```
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
spec:
  ignoreApplicationDifferences:
    - jqPathExpressions:
        - .spec.sources[] | select(.repoURL == "https://git.example.com/org/repo1").targetRevision
  template:
    spec:
      sources:
      - repoURL: https://git.example.com/org/repo1
        targetRevision: main
      - repoURL: https://git.example.com/org/repo2
        targetRevision: main
```
We can change the `targetRevision` of the first repo source and the ApplicationSet controller will not overwrite the changes. 
```
apiVersion: argoproj.io/v1alpha1
kind: Application
spec:
  sources:
  - repoURL: https://git.example.com/org/repo1
    targetRevision: fix/bug-123
  - repoURL: https://git.example.com/org/repo2
    targetRevision: main
```
But if we change the targetRevision of the second repo source, the ApplicationSet will overwrite the entire sources field. 
```
apiVersion: argoproj.io/v1alpha1
kind: Application
spec:
  sources:
  - repoURL: https://git.example.com/org/repo1
    targetRevision: main
  - repoURL: https://git.example.com/org/repo2
    targetRevision: main
```

#### 4. Preventing an Application’s child resources from being deleted when the parent application is deleted

By default, when an Application resource is deleted by the ApplicationSet controller, all of the child resources of the Application will be deleted as well. To prevent this behavior and keep the child resources, add the `preserveResourcesOnDeletion: true` field to the syncPolicy of the ApplicationSet:
```
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
spec:
  # (...)
  syncPolicy:
    preserveResourcesOnDeletion: true
```
Using the `preserveResourcesOnDeletion` policy will prevent the finalizer from being added to Applications, thus stopping their deletion. 

Another way of achieving this behavior would be to delete the ApplicationSet using a non-cascading delete:
```
kubectl delete ApplicationSet (NAME) --cascade=orphan
```

Please note though that when using the non-cascaded delete, the `resources-finalizer.argocd.argoproj.io` is still specified on the Application. This means that when the Application is deleted, all of its deployed resources will also be deleted. If you don’t want this behavior, use the `preserveResourcesOnDeletion` policy above instead. 


#### 5. Preventing an Application’s child resources from being modified

When an Application resource is updated by the ApplicationSet controller, all of the child resources of the Application will be updated accordingly. However, by default (and as a safety mechanism), automated syncs will not delete resources when Argo CD detects the resource is no longer defined in Git. You can "pause" updates to cluster resources managed by the `Application` resource by unsetting the `spec.template.syncPolicy.automated`. As an extra precaution, you could also have `spec.template.syncPolicy.automated.prune` set to false to prevent any unexpected changes. 
```
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
spec:
  # (...)
  syncPolicy:
    automated:
      prune: false 
```

#### 6. Preserving Application annotation and labels

If you have annotations and/or labels that you have applied to Applications after their generation by an ApplicationSet, to keep those annotations/labels when syncing you must use the `preservedFields` property like below:
```
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
spec:
  # (...)
  preservedFields:
    annotations: ["my-custom-annotation"]
    labels: ["my-custom-label"]
```
By default, the Argo CD notifications and the Argo CD refresh type annotations are also preserved.
