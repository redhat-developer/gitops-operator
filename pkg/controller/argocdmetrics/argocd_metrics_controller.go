package argocdmetrics

import (
	"context"
	"fmt"
	argoapp "github.com/argoproj-labs/argocd-operator/pkg/apis/argoproj/v1alpha1"
	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	readRoleNameFormat        = "%s-read"
	readRoleBindingNameFormat = "%s-prometheus-k8s-read-binding"
	alertRuleName             = "gitops-operator-argocd-alerts"
)

var logs = logf.Log.WithName("controller_argocd_metrics")

type ArgoCDMetricsReconciler struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// blank assignment to verify that ReconcileArgoCDRoute implements reconcile.Reconciler
var _ reconcile.Reconciler = &ArgoCDMetricsReconciler{}

// Add creates a new ArgoCD Metrics Controller and adds it to the Manager.
// The Manager will set fields on the Controller and start it when the
// Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

func add(mgr manager.Manager, reconciler reconcile.Reconciler) error {
	reqLogger := logs.WithValues()

	reqLogger.Info("Creating ArgoCD metrics controller")
	c, err := controller.New("argocd-metrics-controller", mgr, controller.Options{Reconciler: reconciler})
	if err != nil {
		return err
	}

	reqLogger.Info("Watching for ArgoCD instance creation")
	err = c.Watch(&source.Kind{Type: &argoapp.ArgoCD{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ArgoCDMetricsReconciler{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

func (r *ArgoCDMetricsReconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := logs.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling ArgoCD Metrics")

	namespace := corev1.Namespace{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: request.Namespace}, &namespace)
	if err != nil {
		if errors.IsNotFound(err) {
			// Namespace not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		reqLogger.Error(err, "Error getting namespace",
			"Namespace", request.Namespace)
		return reconcile.Result{}, err
	}

	argocd := &argoapp.ArgoCD{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: request.Name, Namespace: request.Namespace}, argocd)
	if err != nil {
		if errors.IsNotFound(err) {
			// ArgoCD not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		reqLogger.Error(err, "Error getting ArgoCD instsance")
		return reconcile.Result{}, err
	}

	const clusterMonitoringLabel = "openshift.io/cluster-monitoring"
	_, exists := namespace.Labels[clusterMonitoringLabel]
	if !exists {
		if namespace.Labels == nil {
			namespace.Labels = make(map[string]string)
		}
		namespace.Labels[clusterMonitoringLabel] = "true"
		err = r.client.Update(context.TODO(), &namespace)
		if err != nil {
			reqLogger.Error(err, "Error updating namespace",
				"Namespace", namespace.Name)
			return reconcile.Result{}, err
		}
	} else {
		reqLogger.Info("Namespace already has cluster-monitoring label",
			"Namespace", namespace.Name)
	}

	// Create role to grant read permission to the openshift metrics stack
	err = r.createReadRoleIfAbsent(request.Namespace, argocd, reqLogger)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Create role binding to grant read permission to the openshift metrics stack
	err = r.createReadRoleBindingIfAbsent(request.Namespace, argocd, reqLogger)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Create ServiceMonitor for ArgoCD application metrics
	serviceMonitorLabel := fmt.Sprintf("%s-metrics", request.Name)
	serviceMonitorName := request.Name
	err = r.createServiceMonitorIfAbsent(request.Namespace, argocd, serviceMonitorName, serviceMonitorLabel, reqLogger)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Create ServiceMonitor for ArgoCD API server metrics
	serviceMonitorLabel = fmt.Sprintf("%s-server-metrics", request.Name)
	serviceMonitorName = fmt.Sprintf("%s-server", request.Name)
	err = r.createServiceMonitorIfAbsent(request.Namespace, argocd, serviceMonitorName, serviceMonitorLabel, reqLogger)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Create ServiceMonitor for ArgoCD repo server metrics
	serviceMonitorLabel = fmt.Sprintf("%s-repo-server", request.Name)
	serviceMonitorName = fmt.Sprintf("%s-repo-server", request.Name)
	err = r.createServiceMonitorIfAbsent(request.Namespace, argocd, serviceMonitorName, serviceMonitorLabel, reqLogger)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Create alert rule
	err = r.createPrometheusRuleIfAbsent(request.Namespace, argocd, reqLogger)
	if err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *ArgoCDMetricsReconciler) createReadRoleIfAbsent(namespace string, argocd *argoapp.ArgoCD, reqLogger logr.Logger) error {
	readRole := newReadRole(namespace)
	existingReadRole := &rbacv1.Role{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: readRole.Name, Namespace: readRole.Namespace}, existingReadRole)
	if err == nil {
		reqLogger.Info("Read role already exists",
			"Namespace", readRole.Namespace, "Name", readRole.Name)
		return nil
	}
	if errors.IsNotFound(err) {
		reqLogger.Info("Creating new read role",
			"Namespace", readRole.Namespace, "Name", readRole.Name)

		// Set the ArgoCD instance as the owner and controller
		if err := controllerutil.SetControllerReference(argocd, readRole, r.scheme); err != nil {
			reqLogger.Error(err, "Error setting read role owner ref",
				"Namespace", readRole.Namespace, "Name", readRole.Name, "ArgoCD Name", argocd.Name)
			return err
		}

		err = r.client.Create(context.TODO(), readRole)
		if err != nil {
			reqLogger.Error(err, "Error creating a new read role",
				"Namespace", readRole.Namespace, "Name", readRole.Name)
			return err
		}

		return nil
	}
	reqLogger.Info("Error querying for read role",
		"Name", readRole.Name, "Namespace", readRole.Namespace)
	return err
}

func (r *ArgoCDMetricsReconciler) createReadRoleBindingIfAbsent(namespace string, argocd *argoapp.ArgoCD, reqLogger logr.Logger) error {
	readRoleBinding := newReadRoleBinding(namespace)
	existingReadRoleBinding := &rbacv1.RoleBinding{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: readRoleBinding.Name, Namespace: readRoleBinding.Namespace}, existingReadRoleBinding)
	if err == nil {
		reqLogger.Info("Read role binding already exists",
			"Namespace", readRoleBinding.Namespace, "Name", readRoleBinding.Name)
		return nil
	}
	if errors.IsNotFound(err) {
		reqLogger.Info("Creating new read role binding",
			"Namespace", readRoleBinding.Namespace, "Name", readRoleBinding.Name)

		// Set the ArgoCD instance as the owner and controller
		if err := controllerutil.SetControllerReference(argocd, readRoleBinding, r.scheme); err != nil {
			reqLogger.Error(err, "Error setting read role owner ref",
				"Namespace", readRoleBinding.Namespace, "Name", readRoleBinding.Name, "ArgoCD Name", argocd.Name)
			return err
		}

		err = r.client.Create(context.TODO(), readRoleBinding)
		if err != nil {
			reqLogger.Error(err, "Error creating a new read role binding",
				"Namespace", readRoleBinding.Namespace, "Name", readRoleBinding.Name)
			return err
		}

		return nil
	}
	reqLogger.Error(err, "Error querying for read role binding",
		"Name", readRoleBinding.Name, "Namespace", readRoleBinding.Namespace)
	return err
}

func (r *ArgoCDMetricsReconciler) createServiceMonitorIfAbsent(namespace string, argocd *argoapp.ArgoCD, name, serviceMonitorLabel string, reqLogger logr.Logger) error {
	existingServiceMonitor := &monitoringv1.ServiceMonitor{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, existingServiceMonitor)
	if err == nil {
		reqLogger.Info("A ServiceMonitor instance already exists",
			"Namespace", existingServiceMonitor.Namespace, "Name", existingServiceMonitor.Name)
		return nil
	}
	if errors.IsNotFound(err) {
		serviceMonitor := newServiceMonitor(namespace, name, serviceMonitorLabel)
		reqLogger.Info("Creating a new ServiceMonitor instance",
			"Namespace", serviceMonitor.Namespace, "Name", serviceMonitor.Name)

		// Set the ArgoCD instance as the owner and controller
		if err := controllerutil.SetControllerReference(argocd, serviceMonitor, r.scheme); err != nil {
			reqLogger.Error(err, "Error setting read role owner ref",
				"Namespace", serviceMonitor.Namespace, "Name", serviceMonitor.Name, "ArgoCD Name", argocd.Name)
			return err
		}

		err = r.client.Create(context.TODO(), serviceMonitor)
		if err != nil {
			reqLogger.Error(err, "Error creating a new ServiceMonitor instance",
				"Namespace", serviceMonitor.Namespace, "Name", serviceMonitor.Name)
			return err
		}

		return nil
	}
	reqLogger.Error(err, "Error querying for ServiceMonitor", "Namespace", namespace, "Name", name)
	return err
}

func (r *ArgoCDMetricsReconciler) createPrometheusRuleIfAbsent(namespace string, argocd *argoapp.ArgoCD, reqLogger logr.Logger) error {
	alertRule := newPrometheusRule(namespace)
	existingAlertRule := &monitoringv1.PrometheusRule{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: alertRule.Name, Namespace: alertRule.Namespace}, existingAlertRule)
	if err == nil {
		reqLogger.Info("An alert rule instance already exists",
			"Namespace", existingAlertRule.Namespace, "Name", existingAlertRule.Name)
		return nil
	}
	if errors.IsNotFound(err) {
		reqLogger.Info("Creating new alert rule",
			"Namespace", alertRule.Namespace, "Name", alertRule.Name)

		// Set the ArgoCD instance as the owner and controller
		if err := controllerutil.SetControllerReference(argocd, alertRule, r.scheme); err != nil {
			reqLogger.Error(err, "Error setting read role owner ref",
				"Namespace", alertRule.Namespace, "Name", alertRule.Name, "ArgoCD Name", argocd.Name)
			return err
		}

		err := r.client.Create(context.TODO(), alertRule)
		if err != nil {
			reqLogger.Error(err, "Error creating a new alert rule",
				"Namespace", alertRule.Namespace, "Name", alertRule.Name)
			return err
		}

		return nil
	}
	reqLogger.Error(err, "Error querying for existing alert rule",
		"Namespace", namespace, "Name", alertRuleName)
	return err
}

func newReadRole(namespace string) *rbacv1.Role {
	objectMeta := metav1.ObjectMeta{
		Name:      fmt.Sprintf(readRoleNameFormat, namespace),
		Namespace: namespace,
	}
	rules := []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"endpoints", "services", "pods"},
			Verbs:     []string{"get", "list", "watch"},
		},
	}
	return &rbacv1.Role{
		ObjectMeta: objectMeta,
		Rules:      rules,
	}
}

func newReadRoleBinding(namespace string) *rbacv1.RoleBinding {
	objectMeta := metav1.ObjectMeta{
		Name:      fmt.Sprintf(readRoleBindingNameFormat, namespace),
		Namespace: namespace,
	}
	roleRef := rbacv1.RoleRef{
		APIGroup: "rbac.authorization.k8s.io",
		Kind:     "Role",
		Name:     fmt.Sprintf(readRoleNameFormat, namespace),
	}
	subjects := []rbacv1.Subject{
		{
			Kind:      "ServiceAccount",
			Name:      "prometheus-k8s",
			Namespace: "openshift-monitoring",
		},
	}
	return &rbacv1.RoleBinding{
		ObjectMeta: objectMeta,
		RoleRef:    roleRef,
		Subjects:   subjects,
	}
}

func newServiceMonitor(namespace, name, matchLabel string) *monitoringv1.ServiceMonitor {
	objectMeta := metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
		Labels: map[string]string{
			"release": "prometheus-operator",
		},
	}
	spec := monitoringv1.ServiceMonitorSpec{
		Selector: metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app.kubernetes.io/name": matchLabel,
			},
		},
		Endpoints: []monitoringv1.Endpoint{
			{
				Port: "metrics",
			},
		},
	}
	return &monitoringv1.ServiceMonitor{
		ObjectMeta: objectMeta,
		Spec:       spec,
	}
}

func newPrometheusRule(namespace string) *monitoringv1.PrometheusRule {
	// The namespace used in the alert rule is not the namespace of the
	// running application, it is the namespace that the corresponding
	// ArgoCD application metadata was created in.  This is needed to
	// scope this alert rule to only fire for applications managed
	// by the ArgoCD instance installed in this namespace.
	expr := fmt.Sprintf("argocd_app_info{namespace=\"%s\",sync_status=\"OutOfSync\"} > 0", namespace)

	objectMeta := metav1.ObjectMeta{
		Name:      alertRuleName,
		Namespace: namespace,
	}
	spec := monitoringv1.PrometheusRuleSpec{
		Groups: []monitoringv1.RuleGroup{
			{
				Name: "GitOpsOperatorArgoCD",
				Rules: []monitoringv1.Rule{
					{
						Alert: "ArgoCDSyncAlert",
						Annotations: map[string]string{
							"message": "ArgoCD application {{ $labels.name }} is out of sync",
						},
						Expr: intstr.IntOrString{
							Type:   intstr.String,
							StrVal: expr,
						},
						Labels: map[string]string{
							"severity": "warning",
						},
					},
				},
			},
		},
	}
	return &monitoringv1.PrometheusRule{
		ObjectMeta: objectMeta,
		Spec:       spec,
	}
}
