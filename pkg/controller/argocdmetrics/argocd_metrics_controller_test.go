package argocdmetrics

import (
	"context"
	"fmt"
	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"testing"
)

func newScheme() *runtime.Scheme {
	s := scheme.Scheme
	s.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.Namespace{})
	s.AddKnownTypes(monitoringv1.SchemeGroupVersion, &monitoringv1.ServiceMonitor{})
	s.AddKnownTypes(monitoringv1.SchemeGroupVersion, &monitoringv1.PrometheusRule{})
	return s
}

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func assertEquals(t *testing.T, actual interface{}, expected interface{}) {
	t.Helper()
	if actual != expected {
		t.Fatal(fmt.Sprintf("expected %+v got %+v", expected, actual))
	}
}

func assertNotEquals(t *testing.T, actual interface{}, unexpected interface{}) {
	t.Helper()
	if actual == unexpected {
		t.Fatal(fmt.Sprintf("value should not be equal to \"%+v\"", actual))
	}
}

func assertSameElements(t *testing.T, actual []string, expected ...string) {
	t.Helper()
	if len(actual) != len(expected) {
		t.Fatal(fmt.Sprintf("expected %+v got %+v", expected, actual))
	}
	actualMap := make(map[string]bool)
	for _, v := range actual {
		actualMap[v] = true
	}
	for _, v := range expected {
		if !actualMap[v] {
			t.Fatal(fmt.Sprintf("actual %+v does not contain element \"%+v\"", actual, v))
		}
	}
}

func newClient(s *runtime.Scheme, namespace string) client.Client {
	ns := corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: namespace,
		},
	}
	return fake.NewFakeClientWithScheme(s, &ns)
}

func newRequest(namespace, name string) reconcile.Request {
	return reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}
}

func newMetricsReconciler(t *testing.T, namespace string) ArgoCDMetricsReconciler {
	t.Helper()
	s := newScheme()
	c := newClient(s, namespace)
	r := ArgoCDMetricsReconciler{client: c, scheme: s}
	return r
}

func TestReconcile_add_namespace_label(t *testing.T) {
	testCases := []struct {
		instanceName string
		namespace    string
	}{
		{
			instanceName: "argocd-cluster",
			namespace:    "openshift-gitops",
		},
		{
			instanceName: "instance-two",
			namespace:    "namespace-two",
		},
	}
	for _, tc := range testCases {
		r := newMetricsReconciler(t, tc.namespace)
		_, err := r.Reconcile(newRequest(tc.namespace, tc.instanceName))
		assertNoError(t, err)

		ns := corev1.Namespace{}
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: tc.namespace}, &ns)
		assertNoError(t, err)
		value := ns.Labels["openshift.io/cluster-monitoring"]
		assertEquals(t, value, "true")
	}
}

func TestReconcile_add_read_role(t *testing.T) {
	testCases := []struct {
		instanceName string
		namespace    string
	}{
		{
			instanceName: "argocd-cluster",
			namespace:    "openshift-gitops",
		},
		{
			instanceName: "instance-two",
			namespace:    "namespace-two",
		},
	}
	for _, tc := range testCases {
		r := newMetricsReconciler(t, tc.namespace)
		_, err := r.Reconcile(newRequest(tc.namespace, tc.instanceName))
		assertNoError(t, err)

		role := rbacv1.Role{}
		readRoleName := fmt.Sprintf("%s-read", tc.namespace)
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: readRoleName, Namespace: tc.namespace}, &role)
		assertNoError(t, err)

		assertEquals(t, len(role.Rules), 1)
		assertEquals(t, role.Rules[0].APIGroups[0], "")
		assertSameElements(t, role.Rules[0].Resources, "endpoints", "pods", "services")
		assertSameElements(t, role.Rules[0].Verbs, "get", "list", "watch")
	}
}

func TestReconcile_add_read_role_binding(t *testing.T) {
	testCases := []struct {
		instanceName string
		namespace    string
	}{
		{
			instanceName: "argocd-cluster",
			namespace:    "openshift-gitops",
		},
		{
			instanceName: "instance-two",
			namespace:    "namespace-two",
		},
	}
	for _, tc := range testCases {
		r := newMetricsReconciler(t, tc.namespace)
		_, err := r.Reconcile(newRequest(tc.namespace, tc.instanceName))
		assertNoError(t, err)

		roleBinding := rbacv1.RoleBinding{}
		err = r.client.Get(context.TODO(),
			types.NamespacedName{Name: fmt.Sprintf("%s-prometheus-k8s-read-binding", tc.namespace), Namespace: tc.namespace},
			&roleBinding)
		assertNoError(t, err)

		readRoleName := fmt.Sprintf("%s-read", tc.namespace)
		assertEquals(t, roleBinding.RoleRef, rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     readRoleName,
		},
		)
		assertEquals(t, roleBinding.Subjects[0], rbacv1.Subject{
			Kind:      "ServiceAccount",
			Name:      "prometheus-k8s",
			Namespace: "openshift-monitoring",
		})
	}
}

func TestReconcile_add_service_monitors(t *testing.T) {
	testCases := []struct {
		instanceName string
		namespace    string
	}{
		{
			instanceName: "argocd-cluster",
			namespace:    "openshift-gitops",
		},
		{
			instanceName: "instance-two",
			namespace:    "namespace-two",
		},
	}
	for _, tc := range testCases {
		r := newMetricsReconciler(t, tc.namespace)
		_, err := r.Reconcile(newRequest(tc.namespace, tc.instanceName))
		assertNoError(t, err)

		serviceMonitor := monitoringv1.ServiceMonitor{}
		serviceMonitorName := tc.instanceName
		matchLabel := fmt.Sprintf("%s-metrics", serviceMonitorName)
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: serviceMonitorName, Namespace: tc.namespace}, &serviceMonitor)
		assertNoError(t, err)
		assertEquals(t, len(serviceMonitor.ObjectMeta.Labels), 1)
		assertEquals(t, serviceMonitor.ObjectMeta.Labels["release"], "prometheus-operator")
		assertEquals(t, len(serviceMonitor.Spec.Selector.MatchLabels), 1)
		assertEquals(t, serviceMonitor.Spec.Selector.MatchLabels["app.kubernetes.io/name"], matchLabel)
		assertEquals(t, len(serviceMonitor.Spec.Endpoints), 1)
		assertEquals(t, serviceMonitor.Spec.Endpoints[0].Port, "metrics")

		serviceMonitor = monitoringv1.ServiceMonitor{}
		serviceMonitorName = fmt.Sprintf("%s-server", tc.instanceName)
		matchLabel = fmt.Sprintf("%s-metrics", serviceMonitorName)
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: serviceMonitorName, Namespace: tc.namespace}, &serviceMonitor)
		assertNoError(t, err)
		assertEquals(t, len(serviceMonitor.ObjectMeta.Labels), 1)
		assertEquals(t, serviceMonitor.ObjectMeta.Labels["release"], "prometheus-operator")
		assertEquals(t, len(serviceMonitor.Spec.Selector.MatchLabels), 1)
		assertEquals(t, serviceMonitor.Spec.Selector.MatchLabels["app.kubernetes.io/name"], matchLabel)
		assertEquals(t, len(serviceMonitor.Spec.Endpoints), 1)
		assertEquals(t, serviceMonitor.Spec.Endpoints[0].Port, "metrics")

		serviceMonitor = monitoringv1.ServiceMonitor{}
		serviceMonitorName = fmt.Sprintf("%s-repo-server", tc.instanceName)
		matchLabel = serviceMonitorName
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: serviceMonitorName, Namespace: tc.namespace}, &serviceMonitor)
		assertNoError(t, err)
		assertEquals(t, len(serviceMonitor.ObjectMeta.Labels), 1)
		assertEquals(t, serviceMonitor.ObjectMeta.Labels["release"], "prometheus-operator")
		assertEquals(t, len(serviceMonitor.Spec.Selector.MatchLabels), 1)
		assertEquals(t, serviceMonitor.Spec.Selector.MatchLabels["app.kubernetes.io/name"], matchLabel)
		assertEquals(t, len(serviceMonitor.Spec.Endpoints), 1)
		assertEquals(t, serviceMonitor.Spec.Endpoints[0].Port, "metrics")
	}
}

func TestReconciler_add_prometheus_rule(t *testing.T) {
	testCases := []struct {
		instanceName string
		namespace    string
	}{
		{
			instanceName: "argocd-cluster",
			namespace:    "openshift-gitops",
		},
		{
			instanceName: "instance-two",
			namespace:    "namespace-two",
		},
	}
	for _, tc := range testCases {
		r := newMetricsReconciler(t, tc.namespace)
		_, err := r.Reconcile(newRequest(tc.namespace, tc.instanceName))
		assertNoError(t, err)

		rule := monitoringv1.PrometheusRule{}
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: "gitops-operator-argocd-alerts", Namespace: tc.namespace}, &rule)
		assertNoError(t, err)

		assertEquals(t, rule.Spec.Groups[0].Rules[0].Alert, "ArgoCDSyncAlert")
		assertNotEquals(t, rule.Spec.Groups[0].Rules[0].Annotations["message"], "")
		assertNotEquals(t, rule.Spec.Groups[0].Rules[0].Labels["severity"], "")
		expr := fmt.Sprintf("argocd_app_info{namespace=\"%s\",sync_status=\"OutOfSync\"} > 0", tc.namespace)
		assertEquals(t, rule.Spec.Groups[0].Rules[0].Expr.StrVal, expr)
	}
}
