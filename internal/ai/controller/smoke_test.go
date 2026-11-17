package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	aiconfig "github.com/redhat-developer/gitops-operator/argocd-operator/pkg/ai"
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
)

// Opt-in API smoke test for a dedicated test cluster with the ArgoCD and
// Lightspeed CRDs installed and their reconcilers stopped. The test owns status
// transitions and runs the HTTP receiver locally against real TokenReview,
// Application and AgenticRun APIs. Runs reference the default Agent: a running
// AgenticRun controller can invoke a model even in the fresh test namespace.
// Deployment and notification lifecycle are covered by controller tests.
func TestOpenShiftAgenticAPISmoke(t *testing.T) {
	if os.Getenv("AGENTIC_API_SMOKE") != "1" {
		t.Skip("set AGENTIC_API_SMOKE=1 with a dedicated test-cluster kubeconfig; install ArgoCD and Lightspeed CRDs and stop their reconcilers first")
	}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(clientcmd.NewDefaultClientConfigLoadingRules(), &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		t.Fatal(err)
	}
	config.Timeout = 15 * time.Second
	kube, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	dyn, err := dynamic.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ns, err := kube.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "agentic-api-smoke-"}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("smoke namespace: %s; this test requires ArgoCD and AgenticRun reconcilers to be stopped", ns.Name)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if err := kube.CoreV1().Namespaces().Delete(cleanup, ns.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("cleanup %s: %v", ns.Name, err)
		}
	})
	sa, err := kube.CoreV1().ServiceAccounts(ns.Name).Create(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: notificationsSAName}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	token, err := kube.CoreV1().ServiceAccounts(ns.Name).CreateToken(ctx, notificationsSAName, &authv1.TokenRequest{}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// The webhook authorizes the caller against an AI-enabled ArgoCD instance
	// in its own ServiceAccount namespace, so the smoke namespace needs one.
	// Only the fields the authorization path reads are set; the test's API-only
	// environment keeps operators from reconciling additional resources.
	argocd := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1beta1", "kind": "ArgoCD",
		"metadata": map[string]any{"name": "diagnosis-smoke", "namespace": ns.Name},
		"spec":     map[string]any{"ai": map[string]any{"troubleshooting": true}},
	}}
	argocd, err = dyn.Resource(argocdGVR).Namespace(ns.Name).Create(ctx, argocd, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Keep this local receiver's enrollment registry isolated from the live
	// operator registry; the temporary namespace is its operator namespace.
	enrollment, err := json.Marshal(aiconfig.InstanceEnrollment{Name: argocd.GetName(), UID: string(argocd.GetUID()), ServiceAccountUID: string(sa.UID)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kube.CoreV1().ConfigMaps(ns.Name).Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: aiconfig.EnrollmentConfigMapName, Namespace: ns.Name, Labels: map[string]string{aiconfig.EnrollmentManagedByLabel: aiconfig.EnrollmentManager}},
		Data:       map[string]string{ns.Name: string(enrollment)},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	app := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "argoproj.io/v1alpha1", "kind": "Application", "metadata": map[string]any{"name": "diagnosis-smoke", "namespace": ns.Name, "annotations": map[string]any{"argocd.argoproj.io/skip-reconcile": "true"}}, "spec": map[string]any{"project": "default", "source": map[string]any{"repoURL": "https://example.invalid/agentic-smoke.git", "path": ".", "targetRevision": "HEAD"}, "destination": map[string]any{"server": "https://kubernetes.default.svc", "namespace": ns.Name}}}}
	_, err = dyn.Resource(applicationGVR).Namespace(ns.Name).Create(ctx, app, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	s := &TriggerService{kubeClient: kube, dynClient: dyn, logger: logr.Discard(), config: ServiceConfig{RunNamespace: ns.Name, OperatorNamespace: ns.Name, Cooldown: time.Second}, now: func() time.Time { return now }, queue: newDiagnosisQueue()}
	t.Cleanup(s.queue.ShutDown)
	setHealth := func(health string) {
		t.Helper()
		patch, _ := json.Marshal(map[string]any{"status": map[string]any{"sync": map[string]any{"status": "Synced", "revision": "smoke-revision"}, "health": map[string]any{"status": health, "lastTransitionTime": time.Now().UTC().Format(time.RFC3339)}}})
		// The Application CRD has no status subresource.
		_, err := dyn.Resource(applicationGVR).Namespace(ns.Name).Patch(ctx, app.GetName(), types.MergePatchType, patch, metav1.PatchOptions{})
		if err != nil {
			t.Fatal(err)
		}
	}
	post := func(want int) {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/diagnose", strings.NewReader(fmt.Sprintf(`{"application":%q,"applicationNamespace":%q}`, app.GetName(), ns.Name))).WithContext(ctx)
		request.Header.Set("Authorization", "Bearer "+token.Status.Token)
		w := httptest.NewRecorder()
		s.HandleDiagnose(w, request)
		if w.Code != want {
			t.Fatalf("HTTP %d, want %d: %s", w.Code, want, w.Body.String())
		}
		response := &DiagnoseResponse{}
		if err := json.Unmarshal(w.Body.Bytes(), response); err != nil {
			t.Fatal(err)
		}
		if !response.Accepted {
			t.Fatalf("diagnosis was not queued: %+v", response)
		}
		drainSmokeQueue(t, ctx, s)
	}
	fail := func(name string) {
		t.Helper()
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			run, err := dyn.Resource(agenticRunGVR).Namespace(ns.Name).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			run.Object["status"] = map[string]any{"conditions": []any{map[string]any{"type": "Analyzed", "status": "False", "reason": "SmokeFailure", "message": "Intentional smoke-test failure", "lastTransitionTime": now.Format(time.RFC3339)}}}
			_, err = dyn.Resource(agenticRunGVR).Namespace(ns.Name).UpdateStatus(ctx, run, metav1.UpdateOptions{})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	setHealth("Degraded")
	post(http.StatusAccepted)
	currentApp, err := s.getApplication(ctx, ns.Name, app.GetName())
	if err != nil {
		t.Fatal(err)
	}
	first, err := decodeIncident(currentApp)
	if err != nil || first == nil {
		t.Fatalf("first incident missing: %v", err)
	}
	post(http.StatusAccepted)
	fail(first.RunName)
	now = now.Add(2 * time.Second)
	s.enqueueRunOwner(ctx, mustGetRun(t, s, first.RunName))
	drainSmokeQueue(t, ctx, s)
	runs, err := dyn.Resource(agenticRunGVR).Namespace(ns.Name).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs.Items) != 2 {
		t.Fatalf("expected two attempts, got %d", len(runs.Items))
	}
	for i := range runs.Items {
		fail(runs.Items[i].GetName())
	}
	now = now.Add(time.Minute)
	post(http.StatusAccepted)
	setHealth("Healthy")
	post(http.StatusAccepted)
	setHealth("Degraded")
	post(http.StatusAccepted)
	runs, err = dyn.Resource(agenticRunGVR).Namespace(ns.Name).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs.Items) != 3 {
		t.Fatalf("fresh incident: count=%d", len(runs.Items))
	}
}

// drainSmokeQueue finishes immediate persisted-state transitions without waiting
// for delayed cooldown entries. Tests advance their clock and enqueue another
// event when a cooldown is due. No worker runs concurrently with this helper.
func drainSmokeQueue(t *testing.T, ctx context.Context, s *TriggerService) {
	t.Helper()
	if s.queue.Len() == 0 {
		t.Fatal("expected a queued smoke-test Application")
	}
	for range 12 {
		if err := ctx.Err(); err != nil {
			t.Fatal(err)
		}
		if s.queue.Len() == 0 {
			return
		}
		if !s.processNextWorkItem(ctx) {
			t.Fatal("smoke-test queue shut down")
		}
	}
	if s.queue.Len() != 0 {
		t.Fatal("smoke-test reconciliation did not settle after 12 queue items")
	}
}
