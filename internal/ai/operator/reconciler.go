package operator

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/go-logr/logr"
	argoproj "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/internal/ai/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ reconcile.Reconciler = &GitOpsAIControllerReconciler{}

// GitOpsAIControllerReconciler provisions and maintains the gitops-ai-controller
// Deployment and its supporting resources (RBAC, Service, token Secret, CA
// ConfigMap) in the operator namespace.
type GitOpsAIControllerReconciler struct {
	Client            client.Client
	EnableAIFeatures  bool
	OperatorNamespace string
}

// SetupWithManager registers the controller. All ArgoCD CRs are watched as the
// primary resource. The token Secret and CA ConfigMap trigger re-reconciliation
// by listing ArgoCD instances in the same namespace as the changed object.
func (r *GitOpsAIControllerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&argoproj.ArgoCD{}).
		Complete(r)
}

func (r *GitOpsAIControllerReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := logf.Log.WithName("gitops_ai_controller").WithValues(
		"Request.Namespace", req.Namespace, "Request.Name", req.Name,
	)
	return reconcileAIController(r.Client, r.EnableAIFeatures,
		r.OperatorNamespace, req.NamespacedName, log)
}

// reconcileAIController creates or updates all K8s resources for the
// gitops-ai-controller and cleans them up when AI is disabled.
// operatorNamespace hosts the Deployment, Service, and RBAC.
// argocdKey identifies the ArgoCD instance; its namespace hosts the token Secret
// and CA ConfigMap so the notifications controller can read them without
// cross-namespace access.
func reconcileAIController(
	cl client.Client,
	enableAIFeatures bool,
	operatorNamespace string,
	argocdKey types.NamespacedName,
	logger logr.Logger,
) (reconcile.Result, error) {
	ctx := context.TODO()
	ns := operatorNamespace
	argocdNamespace := argocdKey.Namespace
	runNS := envOrDefault("GITOPS_AI_AGENTIC_RUN_NAMESPACE", "openshift-lightspeed")

	// The operator-level flag is the sole gate for Deployment provisioning.
	// spec.ai.troubleshooting is handled by the ArgoCD reconciler to gate
	// notification configuration only.
	if !enableAIFeatures {
		return reconcile.Result{}, cleanupAIController(ctx, cl, ns, argocdNamespace, runNS)
	}

	// Confirm the ArgoCD instance still exists; its namespace drives per-instance
	// resources (notifications SA, token secret, CA configmap).
	if err := cl.Get(ctx, argocdKey, &argoproj.ArgoCD{}); err != nil {
		if apierrors.IsNotFound(err) {
			// ArgoCD was deleted; namespace-scoped resources are GC'd with the namespace.
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("reading ArgoCD CR: %w", err)
	}
	grace, err := utils.Duration(os.Getenv("GITOPS_AI_DIAGNOSIS_UNHEALTHY_DURATION"), 10*time.Minute, true)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("agentic grace: %w", err)
	}
	cooldown, err := utils.Duration(os.Getenv("GITOPS_AI_DIAGNOSIS_COOLDOWN"), 10*time.Minute, false)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("agentic cooldown: %w", err)
	}
	tlsEnabled := !strings.EqualFold(os.Getenv(TLSEnv), "false")
	deploy := NewDeployment(ns, argocdNamespace, tlsEnabled)
	for i := range deploy.Spec.Template.Spec.Containers[0].Env {
		e := &deploy.Spec.Template.Spec.Containers[0].Env[i]
		switch e.Name {
		case "GITOPS_AI_DIAGNOSIS_COOLDOWN":
			e.Value = cooldown.String()
		case "GITOPS_AI_DIAGNOSIS_UNHEALTHY_DURATION":
			e.Value = grace.String()
		}
	}
	if err := ensureNamespace(ctx, cl, runNS); err != nil {
		return reconcile.Result{}, fmt.Errorf("ensure namespace %s: %w", runNS, err)
	}
	objects := []client.Object{
		NewServiceAccount(ns),
		NewClusterRole(), NewClusterRoleBinding(ns),
		NewRole(runNS), NewRoleBinding(runNS, ns),
		deploy, NewService(ns, tlsEnabled),
	}
	for _, obj := range objects {
		if err := applyManagedResource(ctx, cl, obj); err != nil {
			return reconcile.Result{}, err
		}
	}
	return reconcile.Result{}, nil
}


// applyManagedResource creates or patches a single managed resource, preserving
// API-allocated fields (ClusterIP, NodePort, token/CA data).
func applyManagedResource(ctx context.Context, cl client.Client, desired client.Object) error {
	current := desired.DeepCopyObject().(client.Object)
	err := cl.Get(ctx, client.ObjectKeyFromObject(desired), current)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if apierrors.IsNotFound(err) {
		labels := desired.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels[utils.ManagedBy] = utils.Manager
		desired.SetLabels(labels)
		return cl.Create(ctx, desired)
	}
	if current.GetLabels()[utils.ManagedBy] != utils.Manager {
		return fmt.Errorf("refusing to update resource %s not managed by %s", client.ObjectKeyFromObject(current), utils.Manager)
	}
	before := current.DeepCopyObject().(client.Object)
	labels := current.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[utils.ManagedBy] = utils.Manager
	current.SetLabels(labels)
	annotations := current.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	for k, v := range desired.GetAnnotations() {
		annotations[k] = v
	}
	current.SetAnnotations(annotations)
	switch obj := current.(type) {
	case *appsv1.Deployment:
		d := desired.(*appsv1.Deployment)
		obj.Spec.Replicas = d.Spec.Replicas
		obj.Spec.Template.Labels = d.Spec.Template.Labels
		obj.Spec.Template.Spec.ServiceAccountName = d.Spec.Template.Spec.ServiceAccountName
		obj.Spec.Template.Spec.Volumes = d.Spec.Template.Spec.Volumes
		if len(obj.Spec.Template.Spec.Containers) == 0 {
			obj.Spec.Template.Spec.Containers = d.Spec.Template.Spec.Containers
		} else {
			c, dc := &obj.Spec.Template.Spec.Containers[0], d.Spec.Template.Spec.Containers[0]
			c.Image, c.ImagePullPolicy, c.Args, c.Env, c.Ports, c.VolumeMounts = dc.Image, dc.ImagePullPolicy, dc.Args, dc.Env, dc.Ports, dc.VolumeMounts
		}
	case *corev1.Service:
		d := desired.(*corev1.Service)
		obj.Spec.Selector = d.Spec.Selector
		if len(obj.Spec.Ports) == 1 && len(d.Spec.Ports) == 1 {
			d.Spec.Ports[0].NodePort = obj.Spec.Ports[0].NodePort
		}
		obj.Spec.Ports = d.Spec.Ports
		if _, ok := d.Annotations["service.beta.openshift.io/serving-cert-secret-name"]; !ok {
			delete(obj.Annotations, "service.beta.openshift.io/serving-cert-secret-name")
		}
	case *rbacv1.ClusterRole:
		obj.Rules = desired.(*rbacv1.ClusterRole).Rules
	case *rbacv1.Role:
		obj.Rules = desired.(*rbacv1.Role).Rules
	case *rbacv1.ClusterRoleBinding:
		obj.Subjects = desired.(*rbacv1.ClusterRoleBinding).Subjects
	case *rbacv1.RoleBinding:
		obj.Subjects = desired.(*rbacv1.RoleBinding).Subjects
	}
	if reflect.DeepEqual(before, current) {
		return nil
	}
	return cl.Patch(ctx, current, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
}

func ensureNamespace(ctx context.Context, cl client.Client, name string) error {
	ns := &corev1.Namespace{}
	err := cl.Get(ctx, types.NamespacedName{Name: name}, ns)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	return cl.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	})
}

func cleanupAIController(ctx context.Context, cl client.Client, ns, argocdNS, runNS string) error {
	deploy := &appsv1.Deployment{}
	if err := cl.Get(ctx, types.NamespacedName{Name: ServiceName, Namespace: ns}, deploy); err == nil {
		if deploy.GetLabels()[utils.ManagedBy] != utils.Manager {
			return fmt.Errorf("refusing to remove Deployment not managed by %s", utils.Manager)
		}
		before := deploy.DeepCopy()
		zero := int32(0)
		deploy.Spec.Replicas = &zero
		if !reflect.DeepEqual(before.Spec.Replicas, deploy.Spec.Replicas) {
			if err := cl.Patch(ctx, deploy, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
				return err
			}
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	objects := []client.Object{
		NewDeployment(ns, argocdNS, true), NewService(ns, true),
		NewRoleBinding(runNS, ns), NewRole(runNS),
		NewClusterRoleBinding(ns), NewClusterRole(),
		NewServiceAccount(ns),
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: ServiceName + "-tls", Namespace: ns}},
	}
	for _, obj := range objects {
		if err := cl.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}
