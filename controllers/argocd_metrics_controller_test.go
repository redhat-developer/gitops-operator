/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	argoapp "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	"gotest.tools/assert"
	is "gotest.tools/assert/cmp"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newScheme() *runtime.Scheme {
	s := scheme.Scheme
	s.AddKnownTypes(argoapp.GroupVersion, &argoapp.ArgoCD{})
	s.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.Namespace{})
	s.AddKnownTypes(monitoringv1.SchemeGroupVersion, &monitoringv1.ServiceMonitor{})
	s.AddKnownTypes(monitoringv1.SchemeGroupVersion, &monitoringv1.PrometheusRule{})
	return s
}

const (
	argocdKind         = "ArgoCD"
	argoCDInstanceName = "openshift-gitops"
)

func newClient(s *runtime.Scheme, namespace, name string) client.Client {
	ns := corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: namespace,
		},
	}
	argocd := argoapp.ArgoCD{
		ObjectMeta: v1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	return fake.NewFakeClientWithScheme(s, &ns, &argocd)
}

func newMetricsReconciler(t *testing.T, namespace, name string) ArgoCDMetricsReconciler {
	t.Helper()
	s := newScheme()
	c := newClient(s, namespace, name)
	r := ArgoCDMetricsReconciler{Client: c, Scheme: s}
	return r
}

func TestReconcile_add_namespace_label(t *testing.T) {
	testCases := []struct {
		instanceName string
		namespace    string
	}{
		{
			instanceName: argoCDInstanceName,
			namespace:    "openshift-gitops",
		},
		{
			instanceName: "instance-two",
			namespace:    "namespace-two",
		},
	}
	for _, tc := range testCases {
		r := newMetricsReconciler(t, tc.namespace, tc.instanceName)
		_, err := r.Reconcile(context.TODO(), newRequest(tc.namespace, tc.instanceName))
		assert.NilError(t, err)

		ns := corev1.Namespace{}
		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: tc.namespace}, &ns)
		assert.NilError(t, err)
		value := ns.Labels["openshift.io/cluster-monitoring"]
		assert.Equal(t, value, "true")
	}
}

func TestReconcile_add_read_role(t *testing.T) {
	testCases := []struct {
		instanceName string
		namespace    string
	}{
		{
			instanceName: argoCDInstanceName,
			namespace:    "openshift-gitops",
		},
		{
			instanceName: "instance-two",
			namespace:    "namespace-two",
		},
	}
	for _, tc := range testCases {
		r := newMetricsReconciler(t, tc.namespace, tc.instanceName)
		_, err := r.Reconcile(context.TODO(), newRequest(tc.namespace, tc.instanceName))
		assert.NilError(t, err)

		role := rbacv1.Role{}
		readRoleName := fmt.Sprintf("%s-read", tc.namespace)
		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: readRoleName, Namespace: tc.namespace}, &role)
		assert.NilError(t, err)

		assert.Assert(t, is.Len(role.OwnerReferences, 1))
		assert.Equal(t, role.OwnerReferences[0].Kind, argocdKind)
		assert.Equal(t, role.OwnerReferences[0].Name, tc.instanceName)

		assert.Equal(t, len(role.Rules), 1)
		assert.Equal(t, role.Rules[0].APIGroups[0], "")

		assert.Assert(t, is.Len(role.Rules[0].Resources, 3))
		assert.Assert(t, is.Contains(role.Rules[0].Resources, "endpoints"))
		assert.Assert(t, is.Contains(role.Rules[0].Resources, "pods"))
		assert.Assert(t, is.Contains(role.Rules[0].Resources, "services"))

		assert.Assert(t, is.Len(role.Rules[0].Verbs, 3))
		assert.Assert(t, is.Contains(role.Rules[0].Verbs, "get"))
		assert.Assert(t, is.Contains(role.Rules[0].Verbs, "list"))
		assert.Assert(t, is.Contains(role.Rules[0].Verbs, "watch"))
	}
}

func TestReconcile_add_read_role_binding(t *testing.T) {
	testCases := []struct {
		instanceName string
		namespace    string
	}{
		{
			instanceName: argoCDInstanceName,
			namespace:    "openshift-gitops",
		},
		{
			instanceName: "instance-two",
			namespace:    "namespace-two",
		},
	}
	for _, tc := range testCases {
		r := newMetricsReconciler(t, tc.namespace, tc.instanceName)
		_, err := r.Reconcile(context.TODO(), newRequest(tc.namespace, tc.instanceName))
		assert.NilError(t, err)

		roleBinding := rbacv1.RoleBinding{}
		err = r.Client.Get(context.TODO(),
			types.NamespacedName{Name: fmt.Sprintf("%s-prometheus-k8s-read-binding", tc.namespace), Namespace: tc.namespace},
			&roleBinding)
		assert.NilError(t, err)

		assert.Assert(t, is.Len(roleBinding.OwnerReferences, 1))
		assert.Equal(t, roleBinding.OwnerReferences[0].Kind, argocdKind)
		assert.Equal(t, roleBinding.OwnerReferences[0].Name, tc.instanceName)

		readRoleName := fmt.Sprintf("%s-read", tc.namespace)
		assert.Equal(t, roleBinding.RoleRef, rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     readRoleName,
		},
		)
		assert.Equal(t, roleBinding.Subjects[0], rbacv1.Subject{
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
			instanceName: argoCDInstanceName,
			namespace:    "openshift-gitops",
		},
		{
			instanceName: "instance-two",
			namespace:    "namespace-two",
		},
	}
	for _, tc := range testCases {
		r := newMetricsReconciler(t, tc.namespace, tc.instanceName)
		_, err := r.Reconcile(context.TODO(), newRequest(tc.namespace, tc.instanceName))
		assert.NilError(t, err)

		serviceMonitor := monitoringv1.ServiceMonitor{}
		serviceMonitorName := tc.instanceName
		matchLabel := fmt.Sprintf("%s-metrics", serviceMonitorName)
		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: serviceMonitorName, Namespace: tc.namespace}, &serviceMonitor)
		assert.NilError(t, err)
		assert.Assert(t, is.Len(serviceMonitor.OwnerReferences, 1))
		assert.Equal(t, serviceMonitor.OwnerReferences[0].Kind, argocdKind)
		assert.Equal(t, serviceMonitor.OwnerReferences[0].Name, tc.instanceName)
		assert.Equal(t, len(serviceMonitor.ObjectMeta.Labels), 1)
		assert.Equal(t, serviceMonitor.ObjectMeta.Labels["release"], "prometheus-operator")
		assert.Equal(t, len(serviceMonitor.Spec.Selector.MatchLabels), 1)
		assert.Equal(t, serviceMonitor.Spec.Selector.MatchLabels["app.kubernetes.io/name"], matchLabel)
		assert.Equal(t, len(serviceMonitor.Spec.Endpoints), 1)
		assert.Equal(t, serviceMonitor.Spec.Endpoints[0].Port, "metrics")

		serviceMonitor = monitoringv1.ServiceMonitor{}
		serviceMonitorName = fmt.Sprintf("%s-server", tc.instanceName)
		matchLabel = fmt.Sprintf("%s-metrics", serviceMonitorName)
		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: serviceMonitorName, Namespace: tc.namespace}, &serviceMonitor)
		assert.NilError(t, err)
		assert.Assert(t, is.Len(serviceMonitor.OwnerReferences, 1))
		assert.Equal(t, serviceMonitor.OwnerReferences[0].Kind, argocdKind)
		assert.Equal(t, serviceMonitor.OwnerReferences[0].Name, tc.instanceName)
		assert.Equal(t, len(serviceMonitor.ObjectMeta.Labels), 1)
		assert.Equal(t, serviceMonitor.ObjectMeta.Labels["release"], "prometheus-operator")
		assert.Equal(t, len(serviceMonitor.Spec.Selector.MatchLabels), 1)
		assert.Equal(t, serviceMonitor.Spec.Selector.MatchLabels["app.kubernetes.io/name"], matchLabel)
		assert.Equal(t, len(serviceMonitor.Spec.Endpoints), 1)
		assert.Equal(t, serviceMonitor.Spec.Endpoints[0].Port, "metrics")

		serviceMonitor = monitoringv1.ServiceMonitor{}
		serviceMonitorName = fmt.Sprintf("%s-repo-server", tc.instanceName)
		matchLabel = serviceMonitorName
		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: serviceMonitorName, Namespace: tc.namespace}, &serviceMonitor)
		assert.NilError(t, err)
		assert.Assert(t, is.Len(serviceMonitor.OwnerReferences, 1))
		assert.Equal(t, serviceMonitor.OwnerReferences[0].Kind, argocdKind)
		assert.Equal(t, serviceMonitor.OwnerReferences[0].Name, tc.instanceName)
		assert.Equal(t, len(serviceMonitor.ObjectMeta.Labels), 1)
		assert.Equal(t, serviceMonitor.ObjectMeta.Labels["release"], "prometheus-operator")
		assert.Equal(t, len(serviceMonitor.Spec.Selector.MatchLabels), 1)
		assert.Equal(t, serviceMonitor.Spec.Selector.MatchLabels["app.kubernetes.io/name"], matchLabel)
		assert.Equal(t, len(serviceMonitor.Spec.Endpoints), 1)
		assert.Equal(t, serviceMonitor.Spec.Endpoints[0].Port, "metrics")
	}
}

func TestReconciler_add_prometheus_rule(t *testing.T) {
	testCases := []struct {
		instanceName string
		namespace    string
	}{
		{
			instanceName: argoCDInstanceName,
			namespace:    "openshift-gitops",
		},
		{
			instanceName: "instance-two",
			namespace:    "namespace-two",
		},
	}
	for _, tc := range testCases {
		r := newMetricsReconciler(t, tc.namespace, tc.instanceName)
		_, err := r.Reconcile(context.TODO(), newRequest(tc.namespace, tc.instanceName))
		assert.NilError(t, err)

		rule := monitoringv1.PrometheusRule{}
		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: "gitops-operator-argocd-alerts", Namespace: tc.namespace}, &rule)
		assert.NilError(t, err)

		assert.Assert(t, is.Len(rule.OwnerReferences, 1))
		assert.Equal(t, rule.OwnerReferences[0].Kind, argocdKind)
		assert.Equal(t, rule.OwnerReferences[0].Name, tc.instanceName)

		assert.Equal(t, rule.Spec.Groups[0].Rules[0].Alert, "ArgoCDSyncAlert")
		assert.Assert(t, rule.Spec.Groups[0].Rules[0].Annotations["message"] != "")
		assert.Assert(t, rule.Spec.Groups[0].Rules[0].Labels["severity"] != "")
		assert.Equal(t, rule.Spec.Groups[0].Rules[0].For, "5m")
		expr := fmt.Sprintf("argocd_app_info{namespace=\"%s\",sync_status=\"OutOfSync\"} > 0", tc.namespace)
		assert.Equal(t, rule.Spec.Groups[0].Rules[0].Expr.StrVal, expr)
	}
}

func TestReconciler_add_dashboard(t *testing.T) {

	// Need to create openshift-config-managed namespace for dashboards
	ns := corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: dashboardNamespace,
		},
	}

	// Need to create one configmap to test update existing versus create
	cm := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gitops-overview",
			Namespace: dashboardNamespace,
		},
	}

	testCases := []struct {
		instanceName string
		namespace    string
	}{
		{
			instanceName: argoCDInstanceName,
			namespace:    "openshift-gitops",
		},
	}
	for _, tc := range testCases {
		r := newMetricsReconciler(t, tc.namespace, tc.instanceName)
		// Create dashboard namespace
		err := r.Client.Create(context.TODO(), &ns)
		assert.NilError(t, err)
		// Create update test dashboard
		err = r.Client.Create(context.TODO(), &cm)
		assert.NilError(t, err)

		_, err = r.Reconcile(context.TODO(), newRequest(tc.namespace, tc.instanceName))
		assert.NilError(t, err)

		entries, err := dashboards.ReadDir(dashboardFolder)
		assert.NilError(t, err)

		for _, entry := range entries {
			name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
			content, err := dashboards.ReadFile(dashboardFolder + "/" + entry.Name())
			assert.NilError(t, err)

			dashboard := &corev1.ConfigMap{}
			err = r.Client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: dashboardNamespace}, dashboard)
			assert.NilError(t, err)

			assert.Assert(t, dashboard.ObjectMeta.Labels["console.openshift.io/dashboard"] == "true")
			assert.Assert(t, dashboard.Data[entry.Name()] == string(content))
		}
	}
}
