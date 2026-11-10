package argocd

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	testclient "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	promoter "github.com/argoproj-labs/gitops-promoter/api/v1alpha1"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"

	argoproj "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/argocd-operator/common"
)

func TestReconcileArgoCD_reconcile_ServerIngress_ingressClassName(t *testing.T) {
	logf.SetLogger(ZapLogger(true))

	nginx := "nginx"

	tests := []struct {
		name             string
		ingressClassName *string
	}{
		{
			name:             "undefined ingress class name",
			ingressClassName: nil,
		},
		{
			name:             "ingress class name specified",
			ingressClassName: &nginx,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			a := makeTestArgoCD(func(a *argoproj.ArgoCD) {
				a.Spec.Server.Ingress.Enabled = true
				a.Spec.Server.Ingress.IngressClassName = test.ingressClassName
			})

			resObjs := []client.Object{a}
			subresObjs := []client.Object{a}
			runtimeObjs := []runtime.Object{}
			sch := makeTestReconcilerScheme(argoproj.AddToScheme, promoter.AddToScheme, apiregistrationv1.AddToScheme)
			cl := makeTestReconcilerClient(sch, resObjs, subresObjs, runtimeObjs)
			r := makeTestReconciler(cl, sch, testclient.NewSimpleClientset())

			err := r.reconcileArgoServerIngress(a)
			assert.NoError(t, err)

			ingress := &networkingv1.Ingress{}
			err = r.Get(context.TODO(), types.NamespacedName{
				Name:      "argocd-server",
				Namespace: testNamespace,
			}, ingress)
			assert.NoError(t, err)
			assert.Equal(t, test.ingressClassName, ingress.Spec.IngressClassName)
		})
	}
}

func TestReconcileArgoCD_reconcile_ServerIngress_annotations(t *testing.T) {
	logf.SetLogger(ZapLogger(true))

	a := makeTestArgoCD(func(a *argoproj.ArgoCD) {
		a.Spec.Server.Ingress.Enabled = true
	})

	resObjs := []client.Object{a}
	subresObjs := []client.Object{a}
	runtimeObjs := []runtime.Object{}
	sch := makeTestReconcilerScheme(argoproj.AddToScheme, promoter.AddToScheme, apiregistrationv1.AddToScheme)
	cl := makeTestReconcilerClient(sch, resObjs, subresObjs, runtimeObjs)
	r := makeTestReconciler(cl, sch, testclient.NewSimpleClientset())

	defaults := map[string]string{
		common.ArgoCDKeyIngressSSLRedirect:     "true",
		common.ArgoCDKeyIngressBackendProtocol: "HTTPS", // secure server (default) serves TLS
	}
	custom := map[string]string{common.ArgoCDKeyIngressBackendProtocol: "HTTP"}

	assertAnnotations := func(want map[string]string) {
		t.Helper()
		ingress := &networkingv1.Ingress{}
		assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-server", Namespace: testNamespace}, ingress))
		assert.Equal(t, want, ingress.Annotations)
	}

	// default annotations must survive the update path on later reconciles
	assert.NoError(t, r.reconcileArgoServerIngress(a))
	assert.NoError(t, r.reconcileArgoServerIngress(a))
	assertAnnotations(defaults)

	// custom annotations override defaults per key
	a.Spec.Server.Ingress.Annotations = custom
	assert.NoError(t, r.reconcileArgoServerIngress(a))
	assertAnnotations(map[string]string{
		common.ArgoCDKeyIngressSSLRedirect:     "true",
		common.ArgoCDKeyIngressBackendProtocol: "HTTP",
	})

	// removing custom annotations restores defaults
	a.Spec.Server.Ingress.Annotations = nil
	assert.NoError(t, r.reconcileArgoServerIngress(a))
	assertAnnotations(defaults)
}

func TestReconcileArgoCD_reconcile_ServerIngress_backendMatchesInsecure(t *testing.T) {
	for _, insecure := range []bool{false, true} {
		a := makeTestArgoCD(func(a *argoproj.ArgoCD) {
			a.Spec.Server.Ingress.Enabled = true
			a.Spec.Server.Insecure = insecure
		})
		sch := makeTestReconcilerScheme(argoproj.AddToScheme, promoter.AddToScheme, apiregistrationv1.AddToScheme)
		cl := makeTestReconcilerClient(sch, []client.Object{a}, []client.Object{a}, []runtime.Object{})
		r := makeTestReconciler(cl, sch, testclient.NewSimpleClientset())

		assert.NoError(t, r.reconcileArgoServerIngress(a))

		ingress := &networkingv1.Ingress{}
		assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-server", Namespace: testNamespace}, ingress))
		wantProto, wantPort := "HTTPS", "https"
		if insecure {
			wantProto, wantPort = "HTTP", "http"
		}
		assert.Equal(t, wantProto, ingress.Annotations[common.ArgoCDKeyIngressBackendProtocol])
		assert.Equal(t, wantPort, ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Port.Name)
	}
}

func TestReconcileArgoCD_reconcile_GRPCIngress_updates(t *testing.T) {
	a := makeTestArgoCD(func(a *argoproj.ArgoCD) {
		a.Spec.Server.GRPC.Ingress.Enabled = true
	})
	sch := makeTestReconcilerScheme(argoproj.AddToScheme, promoter.AddToScheme, apiregistrationv1.AddToScheme)
	cl := makeTestReconcilerClient(sch, []client.Object{a}, []client.Object{a}, []runtime.Object{})
	r := makeTestReconciler(cl, sch, testclient.NewSimpleClientset())

	get := func() *networkingv1.Ingress {
		t.Helper()
		i := &networkingv1.Ingress{}
		assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-grpc", Namespace: testNamespace}, i))
		return i
	}

	assert.NoError(t, r.reconcileArgoServerGRPCIngress(a))
	assert.Equal(t, "GRPC", get().Annotations[common.ArgoCDKeyIngressBackendProtocol])

	// CR changes must reach the existing ingress
	nginx := "nginx"
	a.Spec.Server.GRPC.Ingress.Annotations = map[string]string{"foo": "bar"}
	a.Spec.Server.GRPC.Ingress.IngressClassName = &nginx
	a.Spec.Server.GRPC.Ingress.Path = "/grpc"
	assert.NoError(t, r.reconcileArgoServerGRPCIngress(a))
	i := get()
	assert.Equal(t, map[string]string{"foo": "bar", common.ArgoCDKeyIngressBackendProtocol: "GRPC"}, i.Annotations)
	assert.Equal(t, "nginx", *i.Spec.IngressClassName)
	assert.Equal(t, "/grpc", i.Spec.Rules[0].HTTP.Paths[0].Path)

	// disable deletes
	a.Spec.Server.GRPC.Ingress.Enabled = false
	assert.NoError(t, r.reconcileArgoServerGRPCIngress(a))
	assert.Error(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-grpc", Namespace: testNamespace}, &networkingv1.Ingress{}))
}

func TestReconcileArgoCD_reconcile_ServerIngress_serverHost(t *testing.T) {
	logf.SetLogger(ZapLogger(true))

	nginx := "nginx"

	tests := []struct {
		name             string
		ingressClassName *string
		host             string
	}{
		{
			name:             "New Server host specified",
			ingressClassName: &nginx,
			host:             "foo.bar",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			a := makeTestArgoCD(func(a *argoproj.ArgoCD) {
				a.Spec.Server.Ingress.Enabled = true
				a.Spec.Server.Ingress.IngressClassName = test.ingressClassName
			})

			resObjs := []client.Object{a}
			subresObjs := []client.Object{a}
			runtimeObjs := []runtime.Object{}
			sch := makeTestReconcilerScheme(argoproj.AddToScheme, promoter.AddToScheme, apiregistrationv1.AddToScheme)
			cl := makeTestReconcilerClient(sch, resObjs, subresObjs, runtimeObjs)
			r := makeTestReconciler(cl, sch, testclient.NewSimpleClientset())
			err := r.reconcileArgoServerIngress(a)
			assert.NoError(t, err)

			ingress := &networkingv1.Ingress{}
			err = r.Get(context.TODO(), types.NamespacedName{
				Name:      "argocd-server",
				Namespace: testNamespace,
			}, ingress)
			assert.NoError(t, err)
			assert.Equal(t, test.ingressClassName, ingress.Spec.IngressClassName)
			assert.Equal(t, "argocd", ingress.Spec.TLS[0].Hosts[0])
			a = makeTestArgoCD(func(a *argoproj.ArgoCD) {
				a.Spec.Server.Ingress.Enabled = true
				a.Spec.Server.Ingress.IngressClassName = test.ingressClassName
				a.Spec.Server.Host = test.host
			})

			err = r.reconcileArgoServerIngress(a)
			assert.NoError(t, err)
			err = r.Get(context.TODO(), types.NamespacedName{
				Name:      "argocd-server",
				Namespace: testNamespace,
			}, ingress)
			assert.NoError(t, err)
			assert.Equal(t, test.host, ingress.Spec.TLS[0].Hosts[0])
			assert.Equal(t, test.host, ingress.Spec.Rules[0].Host)
			assert.Equal(t, test.ingressClassName, ingress.Spec.IngressClassName)
		})
	}
}

func TestReconcileArgoCD_reconcile_ServerIngress_ingressClassName_update(t *testing.T) {
	logf.SetLogger(ZapLogger(true))

	nginx := "nginx"
	existingIngressClassName := "test-name"

	a := makeTestArgoCD(func(a *argoproj.ArgoCD) {
		a.Spec.Server.Ingress.Enabled = true
		a.Spec.Server.Ingress.IngressClassName = &nginx
	})

	// Existing ingress with different ingressClassName
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "argocd-server",
			Namespace:       a.Namespace,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(a, argoproj.GroupVersion.WithKind("ArgoCD"))},
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &existingIngressClassName,
		},
	}

	resObjs := []client.Object{a, ingress}
	subresObjs := []client.Object{a}
	runtimeObjs := []runtime.Object{}
	sch := makeTestReconcilerScheme(argoproj.AddToScheme, promoter.AddToScheme, apiregistrationv1.AddToScheme)
	cl := makeTestReconcilerClient(sch, resObjs, subresObjs, runtimeObjs)
	r := makeTestReconciler(cl, sch, testclient.NewSimpleClientset())

	err := r.reconcileArgoServerIngress(a)
	assert.NoError(t, err)

	updatedIngress := &networkingv1.Ingress{}
	err = r.Get(context.TODO(), types.NamespacedName{
		Name:      "argocd-server",
		Namespace: testNamespace,
	}, updatedIngress)
	assert.NoError(t, err)
	assert.Equal(t, *a.Spec.Server.Ingress.IngressClassName, *updatedIngress.Spec.IngressClassName)
}

func TestReconcileArgoCD_reconcile_ServerGRPCIngress_ingressClassName(t *testing.T) {
	logf.SetLogger(ZapLogger(true))

	nginx := "nginx"

	tests := []struct {
		name             string
		ingressClassName *string
	}{
		{
			name:             "undefined ingress class name",
			ingressClassName: nil,
		},
		{
			name:             "ingress class name specified",
			ingressClassName: &nginx,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			a := makeTestArgoCD(func(a *argoproj.ArgoCD) {
				a.Spec.Server.GRPC.Ingress.Enabled = true
				a.Spec.Server.GRPC.Ingress.IngressClassName = test.ingressClassName
			})

			resObjs := []client.Object{a}
			subresObjs := []client.Object{a}
			runtimeObjs := []runtime.Object{}
			sch := makeTestReconcilerScheme(argoproj.AddToScheme, promoter.AddToScheme, apiregistrationv1.AddToScheme)
			cl := makeTestReconcilerClient(sch, resObjs, subresObjs, runtimeObjs)
			r := makeTestReconciler(cl, sch, testclient.NewSimpleClientset())

			err := r.reconcileArgoServerGRPCIngress(a)
			assert.NoError(t, err)

			ingress := &networkingv1.Ingress{}
			err = r.Get(context.TODO(), types.NamespacedName{
				Name:      "argocd-grpc",
				Namespace: testNamespace,
			}, ingress)
			assert.NoError(t, err)
			assert.Equal(t, test.ingressClassName, ingress.Spec.IngressClassName)
		})
	}
}

func TestReconcileApplicationSetService_Ingress(t *testing.T) {
	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD()
	obj := argoproj.ArgoCDApplicationSet{
		WebhookServer: argoproj.WebhookServerSpec{
			Ingress: argoproj.ArgoCDIngressSpec{
				Enabled: true,
			},
		},
	}
	a.Spec.ApplicationSet = &obj

	resObjs := []client.Object{a}
	subresObjs := []client.Object{a}
	runtimeObjs := []runtime.Object{}
	sch := makeTestReconcilerScheme(argoproj.AddToScheme, promoter.AddToScheme, apiregistrationv1.AddToScheme)
	cl := makeTestReconcilerClient(sch, resObjs, subresObjs, runtimeObjs)
	r := makeTestReconciler(cl, sch, testclient.NewSimpleClientset())

	ingress := newIngressWithSuffix(common.ApplicationSetServiceNameSuffix, a)
	assert.NoError(t, r.reconcileApplicationSetControllerIngress(a))
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Namespace: ingress.Namespace, Name: ingress.Name}, ingress))
}

func setupIngressTest(t *testing.T, mut func(*argoproj.ArgoCD)) (*ReconcileArgoCD, *argoproj.ArgoCD) {
	a := makeTestArgoCD(mut)
	sch := makeTestReconcilerScheme(argoproj.AddToScheme, promoter.AddToScheme, apiregistrationv1.AddToScheme)
	cl := makeTestReconcilerClient(sch, []client.Object{a}, []client.Object{a}, []runtime.Object{})
	return makeTestReconciler(cl, sch, testclient.NewSimpleClientset()), a
}

func getIngress(t *testing.T, r *ReconcileArgoCD, name string) *networkingv1.Ingress {
	i := &networkingv1.Ingress{}
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: testNamespace}, i))
	return i
}

// second reconcile with unchanged CR must not write
func TestReconcileArgoCD_reconcile_ServerIngress_idempotent(t *testing.T) {
	r, a := setupIngressTest(t, func(a *argoproj.ArgoCD) {
		a.Spec.Server.Ingress.Enabled = true
		a.Spec.Server.GRPC.Ingress.Enabled = true
	})
	assert.NoError(t, r.reconcileArgoServerIngress(a))
	assert.NoError(t, r.reconcileArgoServerGRPCIngress(a))
	for _, n := range []string{"argocd-server", "argocd-grpc"} {
		rv := getIngress(t, r, n).ResourceVersion
		assert.NoError(t, r.reconcileArgoServerIngress(a))
		assert.NoError(t, r.reconcileArgoServerGRPCIngress(a))
		assert.Equal(t, rv, getIngress(t, r, n).ResourceVersion, n+" rewritten with no CR change")
	}
}

// annotations added by other controllers must survive reconcile
func TestReconcileArgoCD_reconcile_ServerIngress_foreignAnnotationsKept(t *testing.T) {
	r, a := setupIngressTest(t, func(a *argoproj.ArgoCD) { a.Spec.Server.Ingress.Enabled = true })
	assert.NoError(t, r.reconcileArgoServerIngress(a))
	i := getIngress(t, r, "argocd-server")
	i.Annotations["cert-manager.io/issuer"] = "x"
	assert.NoError(t, r.Update(context.TODO(), i))
	assert.NoError(t, r.reconcileArgoServerIngress(a))
	assert.Equal(t, "x", getIngress(t, r, "argocd-server").Annotations["cert-manager.io/issuer"])
}

// custom annotations must not drop the backend-protocol default
func TestReconcileArgoCD_reconcile_ServerIngress_customAnnotationsKeepBackendProtocol(t *testing.T) {
	r, a := setupIngressTest(t, func(a *argoproj.ArgoCD) {
		a.Spec.Server.Ingress.Enabled = true
		a.Spec.Server.Ingress.Annotations = map[string]string{"foo": "bar"}
	})
	assert.NoError(t, r.reconcileArgoServerIngress(a))
	i := getIngress(t, r, "argocd-server")
	assert.Equal(t, "https", i.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Port.Name)
	assert.NotEmpty(t, i.Annotations["nginx.ingress.kubernetes.io/backend-protocol"])
}

// removing the TLS override falls back to the default secret
func TestReconcileArgoCD_reconcile_ServerIngress_tlsOverrideRemoved(t *testing.T) {
	r, a := setupIngressTest(t, func(a *argoproj.ArgoCD) {
		a.Spec.Server.Ingress.Enabled = true
		a.Spec.Server.Ingress.TLS = []networkingv1.IngressTLS{{Hosts: []string{"x"}, SecretName: "s"}}
	})
	assert.NoError(t, r.reconcileArgoServerIngress(a))
	a.Spec.Server.Ingress.TLS = nil
	assert.NoError(t, r.reconcileArgoServerIngress(a))
	assert.Equal(t, "argocd-secret", getIngress(t, r, "argocd-server").Spec.TLS[0].SecretName)
}

// server path honoured on update
func TestReconcileArgoCD_reconcile_ServerIngress_pathUpdate(t *testing.T) {
	r, a := setupIngressTest(t, func(a *argoproj.ArgoCD) { a.Spec.Server.Ingress.Enabled = true })
	assert.NoError(t, r.reconcileArgoServerIngress(a))
	a.Spec.Server.Ingress.Path = "/argo"
	assert.NoError(t, r.reconcileArgoServerIngress(a))
	assert.Equal(t, "/argo", getIngress(t, r, "argocd-server").Spec.Rules[0].HTTP.Paths[0].Path)
}

// a disabled ingress that the CR does not own must not be deleted
func TestReconcileArgoCD_reconcile_ServerIngress_unownedNotDeleted(t *testing.T) {
	r, a := setupIngressTest(t, func(a *argoproj.ArgoCD) {})
	other := newIngressWithSuffix("server", a) // pre-existing, user-owned, no ownerRef
	assert.NoError(t, r.Create(context.TODO(), other))
	assert.NoError(t, r.reconcileArgoServerIngress(a))
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-server", Namespace: testNamespace}, &networkingv1.Ingress{}), "unowned ingress must not be deleted")
}

// a disabled gRPC ingress that the CR does not own must not be deleted
func TestReconcileArgoCD_reconcile_ServerGRPCIngress_unownedNotDeleted(t *testing.T) {
	r, a := setupIngressTest(t, func(a *argoproj.ArgoCD) {})
	assert.NoError(t, r.Create(context.TODO(), newIngressWithSuffix("grpc", a)))
	assert.NoError(t, r.reconcileArgoServerGRPCIngress(a))
	getIngress(t, r, "argocd-grpc")
}

// flipping server.insecure on an existing ingress must update protocol and port together
func TestReconcileArgoCD_reconcile_ServerIngress_insecureFlip(t *testing.T) {
	r, a := setupIngressTest(t, func(a *argoproj.ArgoCD) { a.Spec.Server.Ingress.Enabled = true })
	assert.NoError(t, r.reconcileArgoServerIngress(a))
	a.Spec.Server.Insecure = true
	assert.NoError(t, r.reconcileArgoServerIngress(a))
	i := getIngress(t, r, "argocd-server")
	assert.Equal(t, "HTTP", i.Annotations[common.ArgoCDKeyIngressBackendProtocol])
	assert.Equal(t, "http", i.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Port.Name)
}

// an enabled ingress that the CR does not own must be left untouched
func TestReconcileArgoCD_reconcile_Ingress_unownedNotUpdated(t *testing.T) {
	r, a := setupIngressTest(t, func(a *argoproj.ArgoCD) {
		a.Spec.Server.Ingress.Enabled = true
		a.Spec.Server.GRPC.Ingress.Enabled = true
	})
	for _, name := range []string{"server", "grpc"} {
		other := newIngressWithSuffix(name, a)
		other.Annotations = map[string]string{"mine": "yes"}
		assert.NoError(t, r.Create(context.TODO(), other))
	}
	assert.NoError(t, r.reconcileArgoServerIngress(a))
	assert.NoError(t, r.reconcileArgoServerGRPCIngress(a))
	for _, name := range []string{"argocd-server", "argocd-grpc"} {
		i := getIngress(t, r, name)
		assert.Equal(t, map[string]string{"mine": "yes"}, i.Annotations, name)
		assert.Empty(t, i.Spec.Rules, name)
	}
}
