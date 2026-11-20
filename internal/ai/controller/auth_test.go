package controller

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	aiconfig "github.com/redhat-developer/gitops-operator/argocd-operator/pkg/ai"
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

const testOperatorNamespace = "gitops-operator"

func testNotificationsUID(namespace string) string { return "notifications-" + namespace }

func testEnrollmentRegistry(instances ...*unstructured.Unstructured) *corev1.ConfigMap {
	registry := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: aiconfig.EnrollmentConfigMapName, Namespace: testOperatorNamespace,
			Labels: map[string]string{aiconfig.EnrollmentManagedByLabel: aiconfig.EnrollmentManager}},
		Data: map[string]string{},
	}
	for _, instance := range instances {
		data, _ := json.Marshal(aiconfig.InstanceEnrollment{Name: instance.GetName(), UID: string(instance.GetUID()), ServiceAccountUID: testNotificationsUID(instance.GetNamespace())})
		registry.Data[instance.GetNamespace()] = string(data)
	}
	return registry
}

// newArgoCDInstanceObject builds a minimal operator-managed ArgoCD CR. Only the
// fields the authorization path reads are set.
func newArgoCDInstanceObject(name, namespace string, aiEnabled bool, sourceNamespaces ...string) *unstructured.Unstructured {
	cr := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1beta1",
		"kind":       "ArgoCD",
		"metadata":   map[string]any{"name": name, "namespace": namespace, "uid": "argocd-" + namespace + "-" + name},
		"spec":       map[string]any{"ai": map[string]any{"troubleshooting": aiEnabled}},
	}}
	if len(sourceNamespaces) > 0 {
		_ = unstructured.SetNestedStringSlice(cr.Object, sourceNamespaces, "spec", "sourceNamespaces")
	}
	return cr
}

// authFixture builds a TriggerService whose TokenReview attributes every token
// to username, and whose cluster contains the given ArgoCD instances.
func authFixture(username string, instances ...*unstructured.Unstructured) *TriggerService {
	kube := kubefake.NewClientset(testEnrollmentRegistry(instances...))
	kube.PrependReactor("create", "tokenreviews", func(ktesting.Action) (bool, runtime.Object, error) {
		if username == "" {
			return true, &authv1.TokenReview{Status: authv1.TokenReviewStatus{Authenticated: false, Error: "invalid bearer token"}}, nil
		}
		namespace, _, _ := parseServiceAccountUsername(username)
		return true, &authv1.TokenReview{Status: authv1.TokenReviewStatus{Authenticated: true, User: authv1.UserInfo{Username: username, UID: testNotificationsUID(namespace)}}}, nil
	})

	objects := make([]runtime.Object, 0, len(instances))
	for _, instance := range instances {
		objects = append(objects, instance)
	}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{applicationGVR: "ApplicationList", agenticRunGVR: "AgenticRunList", argocdGVR: "ArgoCDList"},
		objects...,
	)
	return &TriggerService{kubeClient: kube, dynClient: dyn, logger: logr.Discard(), config: ServiceConfig{RunNamespace: "lightspeed", OperatorNamespace: testOperatorNamespace}}
}

func saUsername(namespace, name string) string {
	return "system:serviceaccount:" + namespace + ":" + name
}

// Cases 1-7: which callers the webhook accepts, and why the rest are refused.
func TestAuthorizeCallerAcrossArgoCDInstances(t *testing.T) {
	defaultInstance := newArgoCDInstanceObject("openshift-gitops", "openshift-gitops", true)
	teamAInstance := newArgoCDInstanceObject("team-a-argocd", "team-a", true)

	for _, tc := range []struct {
		name          string
		username      string
		instances     []*unstructured.Unstructured
		authenticated bool
		authorized    bool
		wantInstance  string
		wantReason    string
	}{
		{
			name:          "default instance is accepted when AI is enabled",
			username:      saUsername("openshift-gitops", notificationsSAName),
			instances:     []*unstructured.Unstructured{defaultInstance, teamAInstance},
			authenticated: true,
			authorized:    true,
			wantInstance:  "openshift-gitops/openshift-gitops",
		},
		{
			name:          "second instance in another namespace is accepted",
			username:      saUsername("team-a", notificationsSAName),
			instances:     []*unstructured.Unstructured{defaultInstance, teamAInstance},
			authenticated: true,
			authorized:    true,
			wantInstance:  "team-a/team-a-argocd",
		},
		{
			name:          "invalid bearer token is rejected",
			username:      "",
			instances:     []*unstructured.Unstructured{defaultInstance},
			authenticated: false,
		},
		{
			name:          "unrelated service account is rejected",
			username:      saUsername("openshift-gitops", "openshift-gitops-argocd-server"),
			instances:     []*unstructured.Unstructured{defaultInstance},
			authenticated: true,
			authorized:    false,
			wantReason:    "caller is not the notifications service account",
		},
		{
			name:          "notifications SA in a namespace without an instance is rejected",
			username:      saUsername("rogue", notificationsSAName),
			instances:     []*unstructured.Unstructured{defaultInstance},
			authenticated: true,
			authorized:    false,
			wantReason:    errNoArgoCDInstance.Error(),
		},
		{
			name:          "notifications SA of an AI-disabled instance is rejected",
			username:      saUsername("team-b", notificationsSAName),
			instances:     []*unstructured.Unstructured{newArgoCDInstanceObject("team-b-argocd", "team-b", false)},
			authenticated: true,
			authorized:    false,
			wantReason:    errAITroubleshootingDisabled.Error(),
		},
		{
			name:     "ambiguous instance identity is rejected rather than guessed",
			username: saUsername("team-c", notificationsSAName),
			instances: []*unstructured.Unstructured{
				newArgoCDInstanceObject("first", "team-c", true),
				newArgoCDInstanceObject("second", "team-c", true),
			},
			authenticated: true,
			authorized:    false,
			wantReason:    errAmbiguousArgoCDInstance.Error(),
		},
		{
			name:          "a non-service-account principal is rejected",
			username:      "kube:admin",
			instances:     []*unstructured.Unstructured{defaultInstance},
			authenticated: true,
			authorized:    false,
			wantReason:    "caller is not a service account",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := authFixture(tc.username, tc.instances...)
			result, err := s.authorizeCaller(context.Background(), "test-token", logr.Discard())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Authenticated != tc.authenticated || result.Authorized != tc.authorized {
				t.Fatalf("authenticated=%v authorized=%v, want %v/%v (reason %q)",
					result.Authenticated, result.Authorized, tc.authenticated, tc.authorized, result.Reason)
			}
			if tc.wantReason != "" && result.Reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", result.Reason, tc.wantReason)
			}
			if tc.wantInstance != "" {
				if result.Instance == nil {
					t.Fatal("authorized caller has no resolved ArgoCD instance")
				}
				if got := result.Instance.String(); got != tc.wantInstance {
					t.Errorf("instance = %q, want %q", got, tc.wantInstance)
				}
			}
			if !tc.authorized && result.Instance != nil {
				t.Errorf("denied caller carries an instance: %s", result.Instance)
			}
		})
	}
}

// The resolved instance namespace must come from the token, never from a
// namespace that happens to appear in the payload.
func TestAuthorizeCallerIgnoresPayloadNamespace(t *testing.T) {
	s := authFixture(saUsername("team-a", notificationsSAName),
		newArgoCDInstanceObject("team-a-argocd", "team-a", true),
		newArgoCDInstanceObject("openshift-gitops", "openshift-gitops", true),
	)
	result, err := s.authorizeCaller(context.Background(), "test-token", logr.Discard())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Authorized || result.Instance.Namespace != "team-a" {
		t.Fatalf("instance resolved to %v, want team-a", result.Instance)
	}
	if result.Identity.Namespace != "team-a" || result.Identity.Name != notificationsSAName {
		t.Fatalf("identity = %+v", result.Identity)
	}
}

// Case 8: the Application living somewhere other than the ArgoCD namespace is
// not an authentication problem. The caller is still identified and verified.
func TestApplicationNamespaceDoesNotAffectAuthentication(t *testing.T) {
	s := authFixture(saUsername("team-a", notificationsSAName),
		newArgoCDInstanceObject("team-a-argocd", "team-a", true, "team-a-apps"),
	)
	result, err := s.authorizeCaller(context.Background(), "test-token", logr.Discard())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Authenticated || !result.Authorized {
		t.Fatalf("authentication was affected by the Application namespace: %+v", result)
	}
	if result.Instance.Namespace != "team-a" {
		t.Fatalf("instance namespace = %q, want team-a", result.Instance.Namespace)
	}
	// The instance's Applications in Any Namespace list is carried for the
	// Application-level authorization that replaces authorizeApplication.
	if len(result.Instance.SourceNamespaces) != 1 || result.Instance.SourceNamespaces[0] != "team-a-apps" {
		t.Fatalf("sourceNamespaces = %v, want [team-a-apps]", result.Instance.SourceNamespaces)
	}
}

// A TokenReview API failure must not be reported as a denial.
func TestAuthorizeCallerSurfacesTokenReviewFailure(t *testing.T) {
	s := authFixture(saUsername("openshift-gitops", notificationsSAName))
	s.kubeClient.(*kubefake.Clientset).PrependReactor("create", "tokenreviews", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewServiceUnavailable("apiserver down")
	})
	result, err := s.authorizeCaller(context.Background(), "test-token", logr.Discard())
	if err == nil || result != nil {
		t.Fatalf("expected a server error, got result=%+v err=%v", result, err)
	}
}

// An ArgoCD list failure must not be reported as a denial either: failing open
// is wrong, but so is telling a legitimate caller it is unauthorized.
func TestAuthorizeCallerSurfacesInstanceLookupFailure(t *testing.T) {
	s := authFixture(saUsername("openshift-gitops", notificationsSAName))
	s.dynClient.(*dynamicfake.FakeDynamicClient).PrependReactor("list", "argocds", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewInternalError(errors.New("etcd unavailable"))
	})
	result, err := s.authorizeCaller(context.Background(), "test-token", logr.Discard())
	if err == nil || result != nil {
		t.Fatalf("expected a server error, got result=%+v err=%v", result, err)
	}
}

// A terminating instance must not vouch for a caller.
func TestTerminatingInstanceDoesNotAuthorize(t *testing.T) {
	instance := newArgoCDInstanceObject("team-a-argocd", "team-a", true)
	instance.SetDeletionTimestamp(&metav1.Time{Time: metav1.Now().Time})
	s := authFixture(saUsername("team-a", notificationsSAName), instance)
	result, err := s.authorizeCaller(context.Background(), "test-token", logr.Discard())
	if err != nil {
		t.Fatal(err)
	}
	if result.Authorized || result.Reason != errNoArgoCDInstance.Error() {
		t.Fatalf("terminating instance authorized the caller: %+v", result)
	}
}

func TestAuthorizeApplicationFailsClosedOutsideTheInstanceNamespace(t *testing.T) {
	instance := &argocdInstance{Name: "team-a-argocd", Namespace: "team-a", SourceNamespaces: []string{"team-a-apps"}}

	if err := authorizeApplication(instance, "team-a"); err != nil {
		t.Errorf("Application in the instance namespace was refused: %v", err)
	}
	// Ownership of a sourceNamespaces Application cannot be established yet.
	if err := authorizeApplication(instance, "team-a-apps"); !errors.Is(err, errApplicationNotOwned) {
		t.Errorf("sourceNamespaces Application was admitted before ownership checks exist: %v", err)
	}
	if err := authorizeApplication(instance, "someone-else"); !errors.Is(err, errApplicationNotOwned) {
		t.Errorf("unrelated namespace was admitted: %v", err)
	}
	if err := authorizeApplication(nil, "team-a"); !errors.Is(err, errApplicationNotOwned) {
		t.Errorf("missing instance was admitted: %v", err)
	}
}

func TestParseServiceAccountUsername(t *testing.T) {
	for _, tc := range []struct {
		username  string
		namespace string
		name      string
		ok        bool
	}{
		{"system:serviceaccount:team-a:gitops-ai-controller-notifications", "team-a", "gitops-ai-controller-notifications", true},
		{"system:serviceaccount:team-a:", "", "", false},
		{"system:serviceaccount::name", "", "", false},
		{"system:serviceaccount:team-a", "", "", false},
		{"system:serviceaccount:a:b:c", "", "", false},
		{"kube:admin", "", "", false},
		{"system:node:worker-0", "", "", false},
		{"", "", "", false},
	} {
		t.Run(tc.username, func(t *testing.T) {
			namespace, name, ok := parseServiceAccountUsername(tc.username)
			if ok != tc.ok || namespace != tc.namespace || name != tc.name {
				t.Fatalf("got (%q, %q, %v), want (%q, %q, %v)", namespace, name, ok, tc.namespace, tc.name, tc.ok)
			}
		})
	}
}

// End-to-end through the handler: the status codes a caller actually sees, and
// the guarantee that nothing is queued unless the request was fully authorized.
func TestHandleDiagnoseAuthorizationStatusCodes(t *testing.T) {
	for _, tc := range []struct {
		name       string
		username   string
		instances  []*unstructured.Unstructured
		appNS      string
		authHeader string
		wantStatus int
		wantQueued int
	}{
		{
			name:       "authorized caller diagnosing its own namespace is queued",
			username:   saUsername("team-a", notificationsSAName),
			instances:  []*unstructured.Unstructured{newArgoCDInstanceObject("team-a-argocd", "team-a", true)},
			appNS:      "team-a",
			wantStatus: http.StatusAccepted,
			wantQueued: 1,
		},
		{
			name:       "missing authorization header",
			username:   saUsername("team-a", notificationsSAName),
			instances:  []*unstructured.Unstructured{newArgoCDInstanceObject("team-a-argocd", "team-a", true)},
			appNS:      "team-a",
			authHeader: "none",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid token",
			username:   "",
			instances:  []*unstructured.Unstructured{newArgoCDInstanceObject("team-a-argocd", "team-a", true)},
			appNS:      "team-a",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "namespace without an ArgoCD instance",
			username:   saUsername("rogue", notificationsSAName),
			instances:  []*unstructured.Unstructured{newArgoCDInstanceObject("team-a-argocd", "team-a", true)},
			appNS:      "rogue",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "AI-disabled instance",
			username:   saUsername("team-b", notificationsSAName),
			instances:  []*unstructured.Unstructured{newArgoCDInstanceObject("team-b-argocd", "team-b", false)},
			appNS:      "team-b",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "cross-namespace Application fails closed",
			username:   saUsername("team-a", notificationsSAName),
			instances:  []*unstructured.Unstructured{newArgoCDInstanceObject("team-a-argocd", "team-a", true, "team-a-apps")},
			appNS:      "team-a-apps",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "a caller may not diagnose another instance's Application",
			username:   saUsername("team-a", notificationsSAName),
			instances:  []*unstructured.Unstructured{newArgoCDInstanceObject("team-a-argocd", "team-a", true), newArgoCDInstanceObject("team-b-argocd", "team-b", true)},
			appNS:      "team-b",
			wantStatus: http.StatusForbidden,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := authFixture(tc.username, tc.instances...)
			s.queue = newDiagnosisQueue()
			t.Cleanup(s.queue.ShutDown)

			body := `{"application":"demo","applicationNamespace":"` + tc.appNS + `"}`
			request := httptest.NewRequest(http.MethodPost, "/api/v1/diagnose", strings.NewReader(body))
			if tc.authHeader != "none" {
				request.Header.Set("Authorization", "Bearer test-token")
			}
			response := httptest.NewRecorder()
			s.HandleDiagnose(response, request)

			if response.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", response.Code, tc.wantStatus, response.Body.String())
			}
			if got := s.queue.Len(); got != tc.wantQueued {
				t.Fatalf("queued %d Applications, want %d", got, tc.wantQueued)
			}
			decoded := &DiagnoseResponse{}
			if err := json.Unmarshal(response.Body.Bytes(), decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Accepted != (tc.wantStatus == http.StatusAccepted) {
				t.Fatalf("accepted = %v for status %d", decoded.Accepted, response.Code)
			}
		})
	}
}

// Case 9: the single-instance default install keeps working unchanged.
func TestDefaultSingleInstanceInstallStillWorks(t *testing.T) {
	s := authFixture(saUsername("openshift-gitops", notificationsSAName),
		newArgoCDInstanceObject("openshift-gitops", "openshift-gitops", true),
	)
	s.queue = newDiagnosisQueue()
	t.Cleanup(s.queue.ShutDown)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/diagnose",
		strings.NewReader(`{"application":"demo","applicationNamespace":"openshift-gitops"}`))
	request.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	s.HandleDiagnose(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (body %s)", response.Code, response.Body.String())
	}
	if got := s.queue.Len(); got != 1 {
		t.Fatalf("queued %d Applications, want 1", got)
	}
}
