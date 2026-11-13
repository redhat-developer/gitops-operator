package operator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	argoapp "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/go-logr/logr"
	"github.com/redhat-developer/gitops-operator/common"
	"github.com/redhat-developer/gitops-operator/internal/ai/notifications"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const testServiceNamespace = "gitops"

func agenticOperatorFixture(t *testing.T) client.Client {
	t.Helper()
	t.Setenv(TLSEnv, "true")
	t.Setenv("GITOPS_AI_AGENTIC_RUN_NAMESPACE", "lightspeed")
	t.Setenv("GITOPS_AI_DIAGNOSIS_COOLDOWN", "10m")
	t.Setenv("GITOPS_AI_DIAGNOSIS_UNHEALTHY_DURATION", "90s")
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, appsv1.AddToScheme, rbacv1.AddToScheme, argoapp.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	argoCD := &argoapp.ArgoCD{
		ObjectMeta: metav1.ObjectMeta{Name: common.ArgoCDInstanceName, Namespace: testServiceNamespace},
		Spec:       argoapp.ArgoCDSpec{AI: &argoapp.ArgoCDAISpec{Troubleshooting: true}},
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(argoCD).Build()
}

func doReconcile(t *testing.T, cl client.Client, enableAI bool, ns string) {
	t.Helper()
	key := types.NamespacedName{Name: common.ArgoCDInstanceName, Namespace: ns}
	if _, err := reconcileAIController(cl, enableAI, ns, key, logr.Discard()); err != nil {
		t.Fatal(err)
	}
}

func TestAgenticSetupCreatesResourcesAndDisablesCleanly(t *testing.T) {
	cl := agenticOperatorFixture(t)
	ctx := context.Background()

	doReconcile(t, cl, true, "gitops")

	if err := cl.Get(ctx, types.NamespacedName{Name: ServiceName, Namespace: "gitops"}, &appsv1.Deployment{}); err != nil {
		t.Fatalf("deployment missing after enable: %v", err)
	}

	// Turning off the operator flag (enableAIFeatures=false) must remove operator-namespace resources.
	doReconcile(t, cl, false, "gitops")
	if err := cl.Get(ctx, types.NamespacedName{Name: ServiceName, Namespace: "gitops"}, &appsv1.Deployment{}); !apierrors.IsNotFound(err) {
		t.Fatalf("deployment retained after operator flag disabled: %v", err)
	}
}

func TestAgenticTLSReconciliationPreservesServiceAddress(t *testing.T) {
	cl := agenticOperatorFixture(t)
	ctx := context.Background()
	for _, tls := range []string{"false", "true", "false"} {
		t.Setenv(TLSEnv, tls)
		doReconcile(t, cl, true, "gitops")
		svc := &corev1.Service{}
		_ = cl.Get(ctx, types.NamespacedName{Name: ServiceName, Namespace: "gitops"}, svc)
		if svc.Spec.ClusterIP == "" {
			svc.Spec.ClusterIP = "10.0.0.1"
			if err := cl.Update(ctx, svc); err != nil {
				t.Fatal(err)
			}
		} else if svc.Spec.ClusterIP != "10.0.0.1" {
			t.Fatal("clusterIP overwritten")
		}
		deployment := &appsv1.Deployment{}
		_ = cl.Get(ctx, types.NamespacedName{Name: ServiceName, Namespace: "gitops"}, deployment)
		port := int32(8080)
		if tls == "true" {
			port = 8443
		}
		if svc.Spec.Ports[0].Port != port || deployment.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort != port {
			t.Fatal("TLS ports disagree")
		}
		if (len(deployment.Spec.Template.Spec.Volumes) > 0) != (tls == "true") {
			t.Fatal("TLS volume mismatch")
		}
	}
}

func TestAgenticOperatorFlagGatesIntegration(t *testing.T) {
	cl := agenticOperatorFixture(t)
	doReconcile(t, cl, false, "gitops")
	if err := cl.Get(context.Background(), types.NamespacedName{Name: ServiceName, Namespace: "gitops"}, &appsv1.Deployment{}); !apierrors.IsNotFound(err) {
		t.Fatal("deployment created despite flag being off")
	}
}

func TestAgenticDeploymentNotGatedByArgoCDOptIn(t *testing.T) {
	// spec.ai.troubleshooting only gates notification config; the Deployment
	// must be provisioned as long as the operator-level flag is on.
	cl := agenticOperatorFixture(t)
	argoCD := &argoapp.ArgoCD{}
	_ = cl.Get(context.Background(), types.NamespacedName{Name: common.ArgoCDInstanceName, Namespace: testServiceNamespace}, argoCD)
	argoCD.Spec.AI.Troubleshooting = false
	_ = cl.Update(context.Background(), argoCD)
	doReconcile(t, cl, true, "gitops")
	if err := cl.Get(context.Background(), types.NamespacedName{Name: ServiceName, Namespace: "gitops"}, &appsv1.Deployment{}); err != nil {
		t.Fatalf("deployment missing despite operator flag being on: %v", err)
	}
}

func TestAgenticUnownedDeploymentIsPreserved(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("enabled=%v", enabled), func(t *testing.T) {
			cl := agenticOperatorFixture(t)
			if !enabled {
				argoCD := &argoapp.ArgoCD{}
				_ = cl.Get(context.Background(), types.NamespacedName{Name: common.ArgoCDInstanceName, Namespace: testServiceNamespace}, argoCD)
				argoCD.Spec.AI.Troubleshooting = false
				_ = cl.Update(context.Background(), argoCD)
			}
			ctx := context.Background()
			deployment := NewDeployment("gitops", "gitops", true)
			if err := cl.Create(ctx, deployment); err != nil {
				t.Fatal(err)
			}
			before := deployment.DeepCopy()
			if _, err := reconcileAIController(cl, enabled, "gitops", types.NamespacedName{Name: common.ArgoCDInstanceName, Namespace: "gitops"}, logr.Discard()); err == nil {
				t.Fatal("accepted deployment without management label")
			}
			if err := cl.Get(ctx, client.ObjectKeyFromObject(deployment), deployment); err != nil {
				t.Fatal(err)
			}
			if deployment.ResourceVersion != before.ResourceVersion {
				t.Fatal("unmanaged deployment was modified")
			}
		})
	}
}

func TestAgenticOwnedResourceRecreated(t *testing.T) {
	cl := agenticOperatorFixture(t)
	doReconcile(t, cl, true, "gitops")
	obj := NewServiceAccount("gitops")
	if err := cl.Delete(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	doReconcile(t, cl, true, "gitops")
	if err := cl.Get(context.Background(), client.ObjectKeyFromObject(obj), obj); err != nil {
		t.Fatal(fmt.Errorf("not recreated: %w", err))
	}
}

func TestAgenticSubscriptionMergeInNotifications(t *testing.T) {
	original := "- recipients: [slack:ops, gitops-ai-controller]\n  triggers: [on-sync-failed, on-gitops-ai-diagnose]\n  selector: team=platform\n"
	enabled, err := notifications.MergeSubscription(original, true)
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := notifications.MergeSubscription(enabled, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(disabled, "slack:ops") || !strings.Contains(disabled, "on-sync-failed") {
		t.Fatal("user subscription entries lost after disable")
	}
	if strings.Contains(disabled, "gitops-ai-controller") || strings.Contains(disabled, "on-gitops-ai-diagnose") {
		t.Fatal("AI entries still present after disable")
	}
}

func TestAgenticArgoCDReadErrorPreservesResources(t *testing.T) {
	cl := agenticOperatorFixture(t)
	doReconcile(t, cl, true, testServiceNamespace)
	wantErr := errors.New("temporary API failure")
	failing := interceptor.NewClient(cl.(client.WithWatch), interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*argoapp.ArgoCD); ok {
				return wantErr
			}
			return c.Get(ctx, key, obj, opts...)
		},
	})
	_, err := reconcileAIController(failing, true, testServiceNamespace, types.NamespacedName{Name: common.ArgoCDInstanceName, Namespace: testServiceNamespace}, logr.Discard())
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected API error, got %v", err)
	}
	deployment := &appsv1.Deployment{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: ServiceName, Namespace: testServiceNamespace}, deployment); err != nil {
		t.Fatal(err)
	}
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 {
		t.Fatal("API failure scaled down the deployment")
	}
}


func TestGitOpsAIControllerReconciler(t *testing.T) {
	t.Setenv("GITOPS_AI_AGENTIC_RUN_NAMESPACE", "openshift-lightspeed")
	t.Setenv("GITOPS_AI_DIAGNOSIS_COOLDOWN", "10m")
	t.Setenv("GITOPS_AI_DIAGNOSIS_UNHEALTHY_DURATION", "10m")
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, appsv1.AddToScheme, rbacv1.AddToScheme, argoapp.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	const operatorNS, argocdNS = "openshift-gitops-operator", "openshift-gitops"
	argoCD := &argoapp.ArgoCD{
		ObjectMeta: metav1.ObjectMeta{Name: common.ArgoCDInstanceName, Namespace: argocdNS},
		Spec:       argoapp.ArgoCDSpec{AI: &argoapp.ArgoCDAISpec{Troubleshooting: true}},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(argoCD).Build()
	r := &GitOpsAIControllerReconciler{
		Client: cl, EnableAIFeatures: true,
		OperatorNamespace: operatorNS,
	}
	request := reconcile.Request{NamespacedName: types.NamespacedName{Name: common.ArgoCDInstanceName, Namespace: argocdNS}}
	ctx := context.Background()
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	// Deployment goes into the operator namespace.
	if err := cl.Get(ctx, client.ObjectKey{Name: ServiceName, Namespace: operatorNS}, &appsv1.Deployment{}); err != nil {
		t.Fatalf("deployment missing from operator namespace: %v", err)
	}
	// spec.ai.troubleshooting=false does NOT remove the Deployment.
	argoCD.Spec.AI.Troubleshooting = false
	if err := cl.Update(ctx, argoCD); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := cl.Get(ctx, client.ObjectKey{Name: ServiceName, Namespace: operatorNS}, &appsv1.Deployment{}); err != nil {
		t.Fatalf("deployment removed by ArgoCD opt-out (should only be gated by operator flag): %v", err)
	}
	// Turning off the operator flag removes all resources.
	r.EnableAIFeatures = false
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := cl.Get(ctx, client.ObjectKey{Name: ServiceName, Namespace: operatorNS}, &appsv1.Deployment{}); !apierrors.IsNotFound(err) {
		t.Fatalf("deployment retained after operator flag disabled: %v", err)
	}
}
