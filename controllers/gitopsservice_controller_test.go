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
	"os"
	"testing"

	argoapp "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	argocommon "github.com/argoproj-labs/argocd-operator/common"
	"github.com/argoproj-labs/argocd-operator/controllers/argocd"
	"github.com/argoproj-labs/argocd-operator/controllers/argoutil"
	configv1 "github.com/openshift/api/config/v1"
	consolev1 "github.com/openshift/api/console/v1"
	routev1 "github.com/openshift/api/route/v1"
	pipelinesv1alpha1 "github.com/redhat-developer/gitops-operator/api/v1alpha1"
	"github.com/redhat-developer/gitops-operator/common"
	"github.com/redhat-developer/gitops-operator/controllers/util"
	"gotest.tools/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	resourcev1 "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestImageFromEnvVariable(t *testing.T) {
	ns := types.NamespacedName{Name: "test", Namespace: "test"}
	t.Run("Image present as env variable", func(t *testing.T) {
		image := "quay.io/org/test"
		os.Setenv(backendImageEnvName, image)
		defer os.Unsetenv(backendImageEnvName)

		deployment := newBackendDeployment(ns)

		got := deployment.Spec.Template.Spec.Containers[0].Image
		if got != image {
			t.Errorf("Image mismatch: got %s, want %s", got, image)
		}
	})
	t.Run("env variable for image not found", func(t *testing.T) {
		deployment := newBackendDeployment(ns)

		got := deployment.Spec.Template.Spec.Containers[0].Image
		if got != backendImage {
			t.Errorf("Image mismatch: got %s, want %s", got, backendImage)
		}
	})

	t.Run("Kam Image present as env variable", func(t *testing.T) {
		image := "quay.io/org/test"
		os.Setenv(cliImageEnvName, image)
		defer os.Unsetenv(cliImageEnvName)

		deployment := newDeploymentForCLI()

		got := deployment.Spec.Template.Spec.Containers[0].Image
		if got != image {
			t.Errorf("Image mismatch: got %s, want %s", got, image)
		}
	})
	t.Run("env variable for Kam image not found", func(t *testing.T) {
		deployment := newDeploymentForCLI()

		got := deployment.Spec.Template.Spec.Containers[0].Image
		if got != cliImage {
			t.Errorf("Image mismatch: got %s, want %s", got, cliImage)
		}
	})

}

// If the DISABLE_DEFAULT_ARGOCD_INSTANCE is set, ensure that the default ArgoCD instance is not created.
func TestReconcileDisableDefault(t *testing.T) {

	logf.SetLogger(argocd.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	var err error

	fakeClient := fake.NewFakeClient(newGitopsService())
	reconciler := newReconcileGitOpsService(fakeClient, s)
	reconciler.DisableDefaultInstall = true

	_, err = reconciler.Reconcile(context.TODO(), newRequest("test", "test"))
	assertNoError(t, err)

	argoCD := &argoapp.ArgoCD{}

	// ArgoCD instance SHOULD NOT created (in openshift-gitops namespace)
	if err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: common.ArgoCDInstanceName, Namespace: serviceNamespace},
		argoCD); err == nil || !errors.IsNotFound(err) {

		t.Fatalf("ArgoCD instance should not exist in namespace, error: %v", err)
	}

	// openshift-gitops namespace SHOULD be created
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceNamespace}, &corev1.Namespace{})
	assertNoError(t, err)

	// backend Deployment SHOULD be created
	deploy := &appsv1.Deployment{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}, deploy)
	assertNoError(t, err)

}

// If the DISABLE_DEFAULT_ARGOCD_INSTANCE is set, ensure that the default ArgoCD instance is deleted if it already exists.
func TestReconcileDisableDefault_DeleteIfAlreadyExists(t *testing.T) {

	logf.SetLogger(argocd.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	var err error

	fakeClient := fake.NewFakeClient(newGitopsService())
	reconciler := newReconcileGitOpsService(fakeClient, s)
	reconciler.DisableDefaultInstall = false

	_, err = reconciler.Reconcile(context.TODO(), newRequest("test", "test"))
	assertNoError(t, err)

	argoCD := &argoapp.ArgoCD{}

	// ArgoCD instance SHOULD be created (in openshift-gitops namespace)
	if err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: common.ArgoCDInstanceName, Namespace: serviceNamespace},
		argoCD); err != nil {

		t.Fatalf("ArgoCD instance should exist in namespace, error: %v", err)
	}

	reconciler.DisableDefaultInstall = true
	_, err = reconciler.Reconcile(context.TODO(), newRequest("test", "test"))
	assertNoError(t, err)

	// ArgoCD instance SHOULD be deleted from namespace (in openshift-gitops namespace)
	if err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: common.ArgoCDInstanceName, Namespace: serviceNamespace},
		argoCD); err == nil || !errors.IsNotFound(err) {

		t.Fatalf("ArgoCD instance should not exist in namespace, error: %v", err)
	}

	// openshift-gitops namespace SHOULD still exist
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceNamespace}, &corev1.Namespace{})
	assertNoError(t, err)

	// backend Deployment SHOULD still exist
	deploy := &appsv1.Deployment{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}, deploy)
	assertNoError(t, err)

}

func TestReconcile(t *testing.T) {
	defer util.SetConsoleAPIFound(util.IsConsoleAPIFound())
	util.SetConsoleAPIFound(true)

	logf.SetLogger(argocd.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewFakeClient(util.NewClusterVersion("4.15.1"), newGitopsService())
	reconciler := newReconcileGitOpsService(fakeClient, s)

	_, err := reconciler.Reconcile(context.TODO(), newRequest("test", "test"))
	assertNoError(t, err)

	// Check if backend resources are created in openshift-gitops namespace
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceNamespace}, &corev1.Namespace{})
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsServicePrefix + serviceName, Namespace: serviceNamespace}, &corev1.ServiceAccount{})
	assertNoError(t, err)

	role := &rbacv1.ClusterRole{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsServicePrefix + serviceName}, role)
	assertNoError(t, err)
	assert.DeepEqual(t, role.Rules, policyRuleForBackendServiceClusterRole())

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsServicePrefix + serviceName}, &rbacv1.ClusterRoleBinding{})
	assertNoError(t, err)

	deploy := &appsv1.Deployment{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}, deploy)
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}, &corev1.Service{})
	assertNoError(t, err)

	// Check if argocd instance is created in openshift-gitops namespace
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "openshift-gitops", Namespace: serviceNamespace}, &argoapp.ArgoCD{})
	assertNoError(t, err)

	// update Cluster Role and Backend Deployment
	updatedPolicyRules := policyRuleForBackendServiceClusterRole()
	updatedPolicyRules[0].Resources = append(updatedPolicyRules[0].Resources, "testResource")
	role.Rules = updatedPolicyRules

	err = fakeClient.Update(context.TODO(), role)
	assertNoError(t, err)

	deploy.Spec.Template.Spec.Containers[0].Image = "newTestImage:test"
	err = fakeClient.Update(context.TODO(), deploy)
	assertNoError(t, err)

	// reconcile
	_, err = reconciler.Reconcile(context.TODO(), newRequest("test", "test"))
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsServicePrefix + serviceName}, role)
	assertNoError(t, err)
	assert.DeepEqual(t, role.Rules, policyRuleForBackendServiceClusterRole())

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}, deploy)
	assertNoError(t, err)
	assert.DeepEqual(t, deploy.Spec.Template.Spec.Containers[0].Image, backendImage)

	// Check if plugin instance is created in openshift-gitops namespace
	consolePlugin := &consolev1.ConsolePlugin{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName}, consolePlugin)
	assertNoError(t, err)
	assert.DeepEqual(t, consolePlugin.Spec.Backend.Service.Name, gitopsPluginName)

	pluginDeploy := &appsv1.Deployment{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName, Namespace: serviceNamespace}, pluginDeploy)
	assertNoError(t, err)

	pluginService := &corev1.Service{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName, Namespace: serviceNamespace}, pluginService)
	assertNoError(t, err)

	pluginConfigMap := &corev1.ConfigMap{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: httpdConfigMapName, Namespace: serviceNamespace}, pluginConfigMap)
	assertNoError(t, err)
}

func TestReconcile_AppDeliveryNamespace(t *testing.T) {
	logf.SetLogger(argocd.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewFakeClient(util.NewClusterVersion("4.6.15"), newGitopsService())
	reconciler := newReconcileGitOpsService(fakeClient, s)

	_, err := reconciler.Reconcile(context.TODO(), newRequest("test", "test"))
	assertNoError(t, err)

	// Check if both openshift-gitops and openshift-pipelines-app-delivey namespace is created
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: deprecatedServiceNamespace}, &corev1.Namespace{})
	assertNoError(t, err)
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceNamespace}, &corev1.Namespace{})
	assertNoError(t, err)

	// Check if backend resources are created in openshift-pipelines-app-delivery namespace
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: deprecatedServiceNamespace}, &corev1.Namespace{})
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: deprecatedServiceNamespace}, &appsv1.Deployment{})
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: deprecatedServiceNamespace}, &corev1.Service{})
	assertNoError(t, err)

	// Check if argocd instance is created in openshift-gitops namespace
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "openshift-gitops", Namespace: serviceNamespace}, &argoapp.ArgoCD{})
	assertNoError(t, err)
}

func TestReconcile_consoleAPINotFound(t *testing.T) {
	defer util.SetConsoleAPIFound(util.IsConsoleAPIFound())
	util.SetConsoleAPIFound(false)

	logf.SetLogger(argocd.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewFakeClient(newGitopsService())
	reconciler := newReconcileGitOpsService(fakeClient, s)

	_, err := reconciler.Reconcile(context.TODO(), newRequest("test", "test"))
	assertNoError(t, err)

	// Check consolePlugin and other resources are not created
	consolePlugin := &consolev1.ConsolePlugin{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName}, consolePlugin)
	assert.Error(t, err, "consoleplugins.console.openshift.io \"gitops-plugin\" not found")

	pluginDeploy := &appsv1.Deployment{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName, Namespace: serviceNamespace}, pluginDeploy)
	assert.Error(t, err, "deployments.apps \"gitops-plugin\" not found")

	pluginService := &corev1.Service{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName, Namespace: serviceNamespace}, pluginService)
	assert.Error(t, err, "services \"gitops-plugin\" not found")

	pluginConfigMap := &corev1.ConfigMap{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: httpdConfigMapName, Namespace: serviceNamespace}, pluginConfigMap)
	assert.Error(t, err, "configmaps \"httpd-cfg\" not found")
}

func TestReconcile_ocpVersionLowerThan4_15(t *testing.T) {
	defer util.SetConsoleAPIFound(util.IsConsoleAPIFound())
	util.SetConsoleAPIFound(false)

	logf.SetLogger(argocd.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewFakeClient(util.NewClusterVersion("4.11.1"), newGitopsService())
	reconciler := newReconcileGitOpsService(fakeClient, s)

	_, err := reconciler.Reconcile(context.TODO(), newRequest("test", "test"))
	assertNoError(t, err)

	// Check consolePlugin and other resources are not created
	consolePlugin := &consolev1.ConsolePlugin{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName}, consolePlugin)
	assert.Error(t, err, "consoleplugins.console.openshift.io \"gitops-plugin\" not found")

	pluginDeploy := &appsv1.Deployment{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName, Namespace: serviceNamespace}, pluginDeploy)
	assert.Error(t, err, "deployments.apps \"gitops-plugin\" not found")

	pluginService := &corev1.Service{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName, Namespace: serviceNamespace}, pluginService)
	assert.Error(t, err, "services \"gitops-plugin\" not found")

	pluginConfigMap := &corev1.ConfigMap{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: httpdConfigMapName, Namespace: serviceNamespace}, pluginConfigMap)
	assert.Error(t, err, "configmaps \"httpd-cfg\" not found")
}

func TestReconcile_GitOpsNamespace(t *testing.T) {
	logf.SetLogger(argocd.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewFakeClient(util.NewClusterVersion("4.7.1"), newGitopsService())
	reconciler := newReconcileGitOpsService(fakeClient, s)

	_, err := reconciler.Reconcile(context.TODO(), newRequest("test", "test"))
	assertNoError(t, err)

	// Check if only openshift-gitops namespace is created
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceNamespace}, &corev1.Namespace{})
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: deprecatedServiceNamespace}, &corev1.Namespace{})
	wantErr := `namespaces "openshift-pipelines-app-delivery" not found`
	if err == nil {
		t.Fatalf("was expecting an error %s, but got nil", wantErr)
	}
}

func TestReconcile_BackendResourceLimits(t *testing.T) {
	logf.SetLogger(argocd.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewFakeClientWithScheme(s, util.NewClusterVersion("4.7.1"), newGitopsService())
	reconciler := newReconcileGitOpsService(fakeClient, s)

	_, err := reconciler.Reconcile(context.TODO(), newRequest("test", "test"))
	assertNoError(t, err)

	deployment := appsv1.Deployment{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}, &deployment)
	assertNoError(t, err)

	resources := deployment.Spec.Template.Spec.Containers[0].Resources
	assert.Equal(t, resources.Requests[corev1.ResourceCPU], resourcev1.MustParse("250m"))
	assert.Equal(t, resources.Requests[corev1.ResourceMemory], resourcev1.MustParse("128Mi"))
	assert.Equal(t, resources.Limits[corev1.ResourceCPU], resourcev1.MustParse("500m"))
	assert.Equal(t, resources.Limits[corev1.ResourceMemory], resourcev1.MustParse("256Mi"))
}

func TestReconcile_BackendSecurityContext(t *testing.T) {
	logf.SetLogger(argocd.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	// Testing on OCP versions < 4.11.0
	fakeClient := fake.NewFakeClientWithScheme(s, util.NewClusterVersion("4.10.1"), newGitopsService())
	reconciler := newReconcileGitOpsService(fakeClient, s)

	_, err := reconciler.Reconcile(context.TODO(), newRequest("test", "test"))
	assertNoError(t, err)

	deployment := appsv1.Deployment{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}, &deployment)
	assertNoError(t, err)

	// Testing on OCP versions < 4.11.0
	fakeClient = fake.NewFakeClientWithScheme(s, util.NewClusterVersion("4.12.1"), newGitopsService())
	reconciler = newReconcileGitOpsService(fakeClient, s)

	_, err = reconciler.Reconcile(context.TODO(), newRequest("test", "test"))
	assertNoError(t, err)

	deployment = appsv1.Deployment{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}, &deployment)
	assertNoError(t, err)

	securityContext := deployment.Spec.Template.Spec.Containers[0].SecurityContext
	want := &corev1.SecurityContext{
		AllowPrivilegeEscalation: util.BoolPtr(false),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{
				"ALL",
			},
		},
		RunAsNonRoot: util.BoolPtr(true),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
	assert.DeepEqual(t, securityContext, want)
}

func TestReconcile_KamSecurityContext(t *testing.T) {
	logf.SetLogger(argocd.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	util.SetConsoleAPIFound(true)
	defer util.SetConsoleAPIFound(false)

	// Testing on OCP versions < 4.11.0
	fakeClient := fake.NewFakeClientWithScheme(s, util.NewClusterVersion("4.12.1"), newGitopsService())
	reconciler := newReconcileGitOpsService(fakeClient, s)

	_, err := reconciler.Reconcile(context.TODO(), newRequest("test", "test"))
	assertNoError(t, err)

	deployment := appsv1.Deployment{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: cliName, Namespace: serviceNamespace}, &deployment)
	assertNoError(t, err)

	// Testing on OCP versions < 4.11.0

	fakeClient = fake.NewFakeClientWithScheme(s, util.NewClusterVersion("4.12.1"), newGitopsService())
	reconciler = newReconcileGitOpsService(fakeClient, s)

	_, err = reconciler.Reconcile(context.TODO(), newRequest("test", "test"))
	assertNoError(t, err)

	deployment = appsv1.Deployment{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: cliName, Namespace: serviceNamespace}, &deployment)
	assertNoError(t, err)

	securityContext := deployment.Spec.Template.Spec.Containers[0].SecurityContext
	want := &corev1.SecurityContext{
		AllowPrivilegeEscalation: util.BoolPtr(false),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{
				"ALL",
			},
		},
		RunAsNonRoot: util.BoolPtr(true),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
	assert.DeepEqual(t, securityContext, want)
}

func TestReconcile_testArgoCDForOperatorUpgrade(t *testing.T) {
	logf.SetLogger(argocd.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewFakeClientWithScheme(s, util.NewClusterVersion("4.7.1"), newGitopsService())
	reconciler := newReconcileGitOpsService(fakeClient, s)

	// Create a basic ArgoCD CR. ArgoCD created by Operator version >= v1.6.0
	existingArgoCD := &argoapp.ArgoCD{
		ObjectMeta: v1.ObjectMeta{
			Name:      serviceNamespace,
			Namespace: serviceNamespace,
		},
		Spec: argoapp.ArgoCDSpec{
			Server: argoapp.ArgoCDServerSpec{
				Route: argoapp.ArgoCDRouteSpec{
					Enabled: true,
				},
			},
			ApplicationSet: &argoapp.ArgoCDApplicationSet{},
			SSO: &argoapp.ArgoCDSSOSpec{
				Provider: "dex",
				Dex: &argoapp.ArgoCDDexSpec{
					Config: "test-config",
				},
			},
		},
	}

	err := fakeClient.Create(context.TODO(), existingArgoCD)
	assertNoError(t, err)

	_, err = reconciler.Reconcile(context.TODO(), newRequest("test", "test"))
	assertNoError(t, err)

	// ArgoCD instance SHOULD be updated with resource request/limits for each workload.
	updateArgoCD := &argoapp.ArgoCD{}

	if err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: common.ArgoCDInstanceName, Namespace: serviceNamespace},
		updateArgoCD); err != nil {
		t.Fatalf("ArgoCD instance should exist in namespace, error: %v", err)
	}

	assert.Check(t, updateArgoCD.Spec.ApplicationSet.Resources != nil)
	assert.Check(t, updateArgoCD.Spec.Controller.Resources != nil)
	assert.Check(t, updateArgoCD.Spec.SSO.Dex.Resources != nil)
	assert.Check(t, updateArgoCD.Spec.Grafana.Resources != nil)
	assert.Check(t, updateArgoCD.Spec.HA.Resources != nil)
	assert.Check(t, updateArgoCD.Spec.Redis.Resources != nil)
	assert.Check(t, updateArgoCD.Spec.Repo.Resources != nil)
	assert.Check(t, updateArgoCD.Spec.Server.Resources != nil)
}

func TestReconcile_VerifyResourceQuotaDeletionForUpgrade(t *testing.T) {
	logf.SetLogger(argocd.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewFakeClientWithScheme(s, util.NewClusterVersion("4.7.1"), newGitopsService())
	reconciler := newReconcileGitOpsService(fakeClient, s)

	// Create namespace object for default ArgoCD instance and set resource quota to it.
	defaultArgoNS := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceNamespace,
		},
	}
	fakeClient.Create(context.TODO(), defaultArgoNS)

	dummyResourceObj := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-compute-resources", serviceNamespace),
			Namespace: serviceNamespace,
		},
	}
	fakeClient.Create(context.TODO(), dummyResourceObj)

	_, err := reconciler.Reconcile(context.TODO(), newRequest("test", "test"))
	assertNoError(t, err)

	// Verify that resource quota object is deleted after reconciliation.
	resourceQuota := corev1.ResourceQuota{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceNamespace + "-compute-resources", Namespace: serviceNamespace}, &resourceQuota)
	assert.Error(t, err, "resourcequotas \"openshift-gitops-compute-resources\" not found")
}

func TestGetBackendNamespace(t *testing.T) {
	logf.SetLogger(zap.New(zap.UseDevMode(true)))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	assertNamespace := func(t *testing.T, err error, got, want string) {
		t.Helper()
		assertNoError(t, err)
		if got != want {
			t.Fatalf("namespace mismatch: got %s, want %s", got, want)
		}
	}

	t.Run("Using a 4.7 Cluster", func(t *testing.T) {
		fakeClient := fake.NewFakeClient(util.NewClusterVersion("4.7.1"), newGitopsService())
		namespace, err := GetBackendNamespace(fakeClient)
		assertNamespace(t, err, namespace, serviceNamespace)
	})

	t.Run("Using a 4.6 Cluster", func(t *testing.T) {
		fakeClient := fake.NewFakeClient(util.NewClusterVersion("4.6.1"), newGitopsService())
		namespace, err := GetBackendNamespace(fakeClient)
		assertNamespace(t, err, namespace, deprecatedServiceNamespace)
	})

	t.Run("Using a 4.X Cluster", func(t *testing.T) {
		fakeClient := fake.NewFakeClient(util.NewClusterVersion("4.X.1"), newGitopsService())
		namespace, err := GetBackendNamespace(fakeClient)
		assertNamespace(t, err, namespace, serviceNamespace)
	})
}

func TestReconcile_InfrastructureNode(t *testing.T) {
	logf.SetLogger(argocd.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)
	gitopsService := &pipelinesv1alpha1.GitopsService{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceName,
		},
		Spec: pipelinesv1alpha1.GitopsServiceSpec{
			RunOnInfra:  true,
			Tolerations: deploymentDefaultTolerations(),
		},
	}
	fakeClient := fake.NewFakeClient(gitopsService)
	reconciler := newReconcileGitOpsService(fakeClient, s)

	_, err := reconciler.Reconcile(context.TODO(), newRequest("test", "test"))
	assertNoError(t, err)

	deployment := appsv1.Deployment{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}, &deployment)
	assertNoError(t, err)
	nSelector := common.InfraNodeSelector()
	argoutil.AppendStringMap(nSelector, argocommon.DefaultNodeSelector())
	assert.DeepEqual(t, deployment.Spec.Template.Spec.NodeSelector, nSelector)
	assert.DeepEqual(t, deployment.Spec.Template.Spec.Tolerations, deploymentDefaultTolerations())

	argoCD := &argoapp.ArgoCD{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: common.ArgoCDInstanceName, Namespace: serviceNamespace},
		argoCD)
	assertNoError(t, err)
	assert.DeepEqual(t, argoCD.Spec.NodePlacement.NodeSelector, common.InfraNodeSelector())
	assert.DeepEqual(t, argoCD.Spec.NodePlacement.Tolerations, deploymentDefaultTolerations())

}

func addKnownTypesToScheme(scheme *runtime.Scheme) {
	scheme.AddKnownTypes(configv1.GroupVersion, &configv1.ClusterVersion{})
	scheme.AddKnownTypes(pipelinesv1alpha1.GroupVersion, &pipelinesv1alpha1.GitopsService{})
	scheme.AddKnownTypes(argoapp.GroupVersion, &argoapp.ArgoCD{})
	scheme.AddKnownTypes(consolev1.GroupVersion, &consolev1.ConsoleCLIDownload{})
	scheme.AddKnownTypes(routev1.GroupVersion, &routev1.Route{})
	scheme.AddKnownTypes(consolev1.GroupVersion, &consolev1.ConsolePlugin{})
}

func newReconcileGitOpsService(client client.Client, scheme *runtime.Scheme) *ReconcileGitopsService {
	return &ReconcileGitopsService{
		Client: client,
		Scheme: scheme,
	}
}

func newRequest(namespace, name string) reconcile.Request {
	return reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}
}

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func deploymentDefaultTolerations() []corev1.Toleration {
	toleration := []corev1.Toleration{
		{
			Key:    "test_key1",
			Value:  "test_value1",
			Effect: corev1.TaintEffectNoSchedule,
		},
		{
			Key:      "test_key2",
			Value:    "test_value2",
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoSchedule,
		},
	}
	return toleration
}
