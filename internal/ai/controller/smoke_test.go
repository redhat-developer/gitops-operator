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

// Opt-in API smoke test. It creates and removes only a fresh namespace, uses a
// nonexistent Agent (no model calls or execution), and runs the HTTP receiver
// locally against real TokenReview, Application and AgenticRun APIs. Deployment
// and notification configuration lifecycle are covered by controller tests.
func TestOpenShiftAgenticAPISmoke(t *testing.T) {
	if os.Getenv("AGENTIC_API_SMOKE") != "1" {
		t.Skip("set AGENTIC_API_SMOKE=1 with a test-cluster kubeconfig")
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
	t.Logf("isolated smoke namespace: %s", ns.Name)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if err := kube.CoreV1().Namespaces().Delete(cleanup, ns.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("cleanup %s: %v", ns.Name, err)
		}
	})
	_, err = kube.CoreV1().ServiceAccounts(ns.Name).Create(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: notificationsSAName}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	token, err := kube.CoreV1().ServiceAccounts(ns.Name).CreateToken(ctx, notificationsSAName, &authv1.TokenRequest{}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	app := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "argoproj.io/v1alpha1", "kind": "Application", "metadata": map[string]any{"name": "diagnosis-smoke", "namespace": ns.Name, "annotations": map[string]any{"argocd.argoproj.io/skip-reconcile": "true"}}, "spec": map[string]any{"project": "default", "source": map[string]any{"repoURL": "https://example.invalid/agentic-smoke.git", "path": ".", "targetRevision": "HEAD"}, "destination": map[string]any{"server": "https://kubernetes.default.svc", "namespace": ns.Name}}}}
	_, err = dyn.Resource(applicationGVR).Namespace(ns.Name).Create(ctx, app, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	s := &TriggerService{kubeClient: kube, dynClient: dyn, logger: logr.Discard(), config: ServiceConfig{RunNamespace: ns.Name, ArgocdNamespace: ns.Name, Cooldown: time.Second}, now: func() time.Time { return now }, queue: newDiagnosisQueue()}
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
		processQueuedApplication(t, s)
	}
	fail := func(name string) {
		t.Helper()
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			run, err := dyn.Resource(agenticRunGVR).Namespace(ns.Name).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			run.Object["status"] = map[string]any{"conditions": []any{map[string]any{"type": "Analyzed", "status": "False", "reason": "SmokeFailure", "message": "Intentional smoke-test failure; no model invocation", "lastTransitionTime": now.Format(time.RFC3339)}}}
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
	processQueuedApplication(t, s)
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
	if err != nil || len(runs.Items) != 3 {
		t.Fatalf("fresh incident: count=%d err=%v", len(runs.Items), err)
	}
}
