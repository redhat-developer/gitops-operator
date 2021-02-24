package gitopsservice

import (
	"context"
	"encoding/base64"
	"os"
	"testing"

	argoapp "github.com/argoproj-labs/argocd-operator/pkg/apis/argoproj/v1alpha1"
	keycloakv1alpha1 "github.com/keycloak/keycloak-operator/pkg/apis/keycloak/v1alpha1"
	configv1 "github.com/openshift/api/config/v1"
	console "github.com/openshift/api/console/v1"
	oauthv1 "github.com/openshift/api/oauth/v1"
	routev1 "github.com/openshift/api/route/v1"
	pipelinesv1alpha1 "github.com/redhat-developer/gitops-operator/pkg/apis/pipelines/v1alpha1"
	"github.com/redhat-developer/gitops-operator/pkg/controller/argocd"
	"github.com/redhat-developer/gitops-operator/pkg/controller/util"
	"gotest.tools/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/yaml"
)

const (
	dummyArgoCDRouteName    = "openshift-gitops-server"
	dummyKeycloakRouteName  = "keycloak"
	dummyKeycloakSecretName = "keycloak-client-secret-openshift-gitops"
	dummyArgoSecretName     = "argocd-secret"
)

var (
	dummyKeycloakData     = dummyKeycloakSecretData()
	dummyArgoData         = dummyArgoSecretData()
	dummyKeycloakRealmURL = "https://keycloak.com/auth/realms/openshift-gitops"
)

var (
	dummyArgoCDRoute = &routev1.Route{
		ObjectMeta: v1.ObjectMeta{
			Name:      dummyArgoCDRouteName,
			Namespace: serviceNamespace,
		},
		Spec: routev1.RouteSpec{
			Host: "argocd.com",
		},
	}

	dummyKeycloakRoute = &routev1.Route{
		ObjectMeta: v1.ObjectMeta{
			Name:      dummyKeycloakRouteName,
			Namespace: serviceNamespace,
		},
		Spec: routev1.RouteSpec{
			Host: "keycloak.com",
		},
	}

	dummyKeycloakSecret = &corev1.Secret{
		ObjectMeta: v1.ObjectMeta{
			Name:      dummyKeycloakSecretName,
			Namespace: serviceNamespace,
		},
		Data: dummyKeycloakData,
	}

	dummyArgoSecret = &corev1.Secret{
		ObjectMeta: v1.ObjectMeta{
			Name:      dummyArgoSecretName,
			Namespace: serviceNamespace,
		},
		Data: dummyArgoData,
	}
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

func TestReconcile(t *testing.T) {
	logf.SetLogger(logf.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewFakeClient(newGitopsService(), dummyArgoCDRoute,
		dummyKeycloakRoute, dummyKeycloakSecret, dummyArgoSecret)
	reconciler := newReconcileGitOpsService(fakeClient, s)

	_, err := reconciler.Reconcile(newRequest("test", "test"))
	assertNoError(t, err)

	// Check if backend resources are created in openshift-gitops namespace
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceNamespace}, &corev1.Namespace{})
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}, &appsv1.Deployment{})
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}, &corev1.Service{})
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}, &routev1.Route{})
	assertNoError(t, err)

	// Check if argocd instance is created in openshift-gitops namespace
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "openshift-gitops", Namespace: serviceNamespace}, &argoapp.ArgoCD{})
	assertNoError(t, err)
}

func TestReconcileWithSSO(t *testing.T) {
	logf.SetLogger(logf.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	gitopsService := newGitopsService()
	fakeClient := fake.NewFakeClient(gitopsService, dummyArgoCDRoute,
		dummyKeycloakRoute, dummyKeycloakSecret, dummyArgoSecret)
	reconciler := newReconcileGitOpsService(fakeClient, s)

	_, err := reconciler.Reconcile(newRequest("test", "test"))
	assertNoError(t, err)

	// Check if backend resources are created in openshift-gitops namespace
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceNamespace}, &corev1.Namespace{})
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}, &appsv1.Deployment{})
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}, &corev1.Service{})
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}, &routev1.Route{})
	assertNoError(t, err)

	// Check if argocd instance is created in openshift-gitops namespace
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "openshift-gitops", Namespace: serviceNamespace}, &argoapp.ArgoCD{})
	assertNoError(t, err)

	// switch back SSO = true and test if OIDC config is updated.
	gitopsService.Spec.EnableSSO = true
	err = fakeClient.Update(context.TODO(), gitopsService)
	assertNoError(t, err)
	_, err = reconciler.Reconcile(newRequest("test", "test"))

	// Check if keycloak instance is created in openshift-gitops namespace
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "keycloak-openshift-gitops", Namespace: serviceNamespace}, &keycloakv1alpha1.Keycloak{})
	assertNoError(t, err)

	// Check if keycloakrealm instance is created in openshift-gitops namespace
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "keycloakrealm-openshift-gitops", Namespace: serviceNamespace}, &keycloakv1alpha1.KeycloakRealm{})
	assertNoError(t, err)

	// Check if keycloakclient instance is created in openshift-gitops namespace
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "keycloakclient-openshift-gitops", Namespace: serviceNamespace}, &keycloakv1alpha1.KeycloakClient{})
	assertNoError(t, err)

	// Check if oauthclient instance is created in openshift-gitops namespace
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "oauthclient-openshift-gitops", Namespace: serviceNamespace}, &oauthv1.OAuthClient{})
	assertNoError(t, err)
}

func TestReconcile_AppDeliveryNamespace(t *testing.T) {
	logf.SetLogger(logf.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewFakeClient(util.NewClusterVersion("4.6.15"), newGitopsService(), dummyArgoCDRoute,
		dummyKeycloakRoute, dummyKeycloakSecret, dummyArgoSecret)
	reconciler := newReconcileGitOpsService(fakeClient, s)

	_, err := reconciler.Reconcile(newRequest("test", "test"))
	assertNoError(t, err)

	// Check if both openshift-gitops and openshift-pipelines-app-delivey namespace is created
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: depracatedServiceNamespace}, &corev1.Namespace{})
	assertNoError(t, err)
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceNamespace}, &corev1.Namespace{})
	assertNoError(t, err)

	// Check if backend resources are created in openshift-pipelines-app-delivery namespace
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: depracatedServiceNamespace}, &corev1.Namespace{})
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: depracatedServiceNamespace}, &appsv1.Deployment{})
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: depracatedServiceNamespace}, &corev1.Service{})
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: depracatedServiceNamespace}, &routev1.Route{})
	assertNoError(t, err)

	// Check if argocd instance is created in openshift-gitops namespace
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "openshift-gitops", Namespace: serviceNamespace}, &argoapp.ArgoCD{})
	assertNoError(t, err)
}

func TestReconcile_AppDeliveryNamespaceWithSSO(t *testing.T) {
	logf.SetLogger(logf.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	gitopsService := newGitopsService()
	fakeClient := fake.NewFakeClient(util.NewClusterVersion("4.6.15"), gitopsService, dummyArgoCDRoute,
		dummyKeycloakRoute, dummyKeycloakSecret, dummyArgoSecret)
	reconciler := newReconcileGitOpsService(fakeClient, s)

	_, err := reconciler.Reconcile(newRequest("test", "test"))
	assertNoError(t, err)

	// Check if both openshift-gitops and openshift-pipelines-app-delivey namespace is created
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: depracatedServiceNamespace}, &corev1.Namespace{})
	assertNoError(t, err)
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceNamespace}, &corev1.Namespace{})
	assertNoError(t, err)

	// Check if backend resources are created in openshift-pipelines-app-delivery namespace
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: depracatedServiceNamespace}, &corev1.Namespace{})
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: depracatedServiceNamespace}, &appsv1.Deployment{})
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: depracatedServiceNamespace}, &corev1.Service{})
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: depracatedServiceNamespace}, &routev1.Route{})
	assertNoError(t, err)

	// Check if argocd instance is created in openshift-gitops namespace
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "openshift-gitops", Namespace: serviceNamespace}, &argoapp.ArgoCD{})
	assertNoError(t, err)

	// switch back SSO = true and test if OIDC config is updated.
	gitopsService.Spec.EnableSSO = true
	err = fakeClient.Update(context.TODO(), gitopsService)
	assertNoError(t, err)
	_, err = reconciler.Reconcile(newRequest("test", "test"))

	// Check if keycloak instance is created in openshift-gitops namespace
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "keycloak-openshift-gitops", Namespace: serviceNamespace}, &keycloakv1alpha1.Keycloak{})
	assertNoError(t, err)

	// Check if keycloakrealm instance is created in openshift-gitops namespace
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "keycloakrealm-openshift-gitops", Namespace: serviceNamespace}, &keycloakv1alpha1.KeycloakRealm{})
	assertNoError(t, err)

	// Check if keycloakclient instance is created in openshift-gitops namespace
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "keycloakclient-openshift-gitops", Namespace: serviceNamespace}, &keycloakv1alpha1.KeycloakClient{})
	assertNoError(t, err)

	// Check if oauthclient instance is created in openshift-gitops namespace
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "oauthclient-openshift-gitops", Namespace: serviceNamespace}, &oauthv1.OAuthClient{})
	assertNoError(t, err)
}

func TestReconcile_testSSOFlag(t *testing.T) {
	logf.SetLogger(logf.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	gitopsService := newGitopsService()
	fakeClient := fake.NewFakeClient(gitopsService, dummyArgoCDRoute,
		dummyKeycloakRoute, dummyKeycloakSecret, dummyArgoSecret)
	reconciler := newReconcileGitOpsService(fakeClient, s)

	_, err := reconciler.Reconcile(newRequest("test", "test"))
	assertNoError(t, err)

	// By default, SSO = false flag is enabled in argoCD Instance.
	testArgoCD := &argoapp.ArgoCD{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "openshift-gitops", Namespace: serviceNamespace}, testArgoCD)
	assert.DeepEqual(t, testArgoCD.Spec.OIDCConfig, "")

	// switch SSO = true and test if OIDC config is updated.
	gitopsService.Spec.EnableSSO = true
	err = fakeClient.Update(context.TODO(), gitopsService)
	assertNoError(t, err)

	_, err = reconciler.Reconcile(newRequest("test", "test"))
	assertNoError(t, err)
	testArgoCD = &argoapp.ArgoCD{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "openshift-gitops", Namespace: serviceNamespace}, testArgoCD)
	testOIDCConfig := dummyOIDCConfig()
	assert.DeepEqual(t, testArgoCD.Spec.OIDCConfig, testOIDCConfig)

	// switch back SSO = false and test if OIDC config is removed.
	gitopsService.Spec.EnableSSO = false
	err = fakeClient.Update(context.TODO(), gitopsService)
	assertNoError(t, err)

	_, err = reconciler.Reconcile(newRequest("test", "test"))
	assertNoError(t, err)
	testArgoCD = &argoapp.ArgoCD{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "openshift-gitops", Namespace: serviceNamespace}, testArgoCD)
	assert.DeepEqual(t, testArgoCD.Spec.OIDCConfig, "")

}

func TestReconcile_GitOpsNamespace(t *testing.T) {
	logf.SetLogger(logf.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewFakeClient(util.NewClusterVersion("4.7.1"), newGitopsService())
	reconciler := newReconcileGitOpsService(fakeClient, s)

	_, err := reconciler.Reconcile(newRequest("test", "test"))
	assertNoError(t, err)

	// Check if only openshift-gitops namespace is created
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceNamespace}, &corev1.Namespace{})
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: depracatedServiceNamespace}, &corev1.Namespace{})
	wantErr := `namespaces "openshift-pipelines-app-delivery" not found`
	if err == nil {
		t.Fatalf("was expecting an error %s, but got nil", wantErr)
	}
}

func TestGetBackendNamespace(t *testing.T) {
	logf.SetLogger(logf.ZapLogger(true))
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
		assertNamespace(t, err, namespace, depracatedServiceNamespace)
	})

	t.Run("Using a 4.X Cluster", func(t *testing.T) {
		fakeClient := fake.NewFakeClient(util.NewClusterVersion("4.X.1"), newGitopsService())
		namespace, err := GetBackendNamespace(fakeClient)
		assertNamespace(t, err, namespace, serviceNamespace)
	})
}

func TestFilters(t *testing.T) {
	logf.SetLogger(logf.ZapLogger(true))

	assert.DeepEqual(t, true, filterKeycloakRoute("openshift-gitops", "keycloak"))
	assert.DeepEqual(t, true, filterOIDCSecrets("openshift-gitops", "argocd-secret"))
	assert.DeepEqual(t, true, filterOIDCSecrets("openshift-gitops", "keycloak-client-secret-openshift-gitops"))
}

func addKnownTypesToScheme(scheme *runtime.Scheme) {
	scheme.AddKnownTypes(console.GroupVersion, &console.ConsoleCLIDownload{})
	scheme.AddKnownTypes(configv1.GroupVersion, &configv1.ClusterVersion{})
	scheme.AddKnownTypes(pipelinesv1alpha1.SchemeGroupVersion, &pipelinesv1alpha1.GitopsService{})
	scheme.AddKnownTypes(routev1.GroupVersion, &routev1.Route{})
	scheme.AddKnownTypes(argoapp.SchemeGroupVersion, &argoapp.ArgoCD{})
	scheme.AddKnownTypes(keycloakv1alpha1.SchemeGroupVersion, &keycloakv1alpha1.Keycloak{})
	scheme.AddKnownTypes(keycloakv1alpha1.SchemeGroupVersion, &keycloakv1alpha1.KeycloakRealm{})
	scheme.AddKnownTypes(keycloakv1alpha1.SchemeGroupVersion, &keycloakv1alpha1.KeycloakClient{})
	scheme.AddKnownTypes(oauthv1.GroupVersion, &oauthv1.OAuthClient{})
}

func newReconcileGitOpsService(client client.Client, scheme *runtime.Scheme) *ReconcileGitopsService {
	return &ReconcileGitopsService{
		client: client,
		scheme: scheme,
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

func dummyKeycloakSecretData() map[string][]byte {
	secret := []byte("test")
	data := make(map[string][]byte)
	encoded := base64.StdEncoding.EncodeToString(secret)
	data["CLIENT_SECRET"] = []byte(encoded)
	return data
}

func dummyArgoSecretData() map[string][]byte {
	secret := []byte("test")
	data := make(map[string][]byte)
	encoded := base64.StdEncoding.EncodeToString(secret)
	data["oidc.keycloak.clientSecret"] = []byte(encoded)
	return data
}

func dummyOIDCConfig() string {
	o, _ := yaml.Marshal(argocd.OIDCConfig{
		Name:           "Keycloak",
		Issuer:         dummyKeycloakRealmURL,
		ClientID:       argoClientID,
		ClientSecret:   "$oidc.keycloak.clientSecret",
		RequestedScope: []string{"openid", "profile", "email", "groups"},
	})
	return string(o)
}
