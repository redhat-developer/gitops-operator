package openshift

import (
	"context"
	"sort"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"

	argoapp "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestReconcileArgoCD_reconcileApplicableClusterRole(t *testing.T) {

	setClusterConfigNamespaces(t)

	a := makeTestArgoCDForClusterConfig()
	testClusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: a.Name + "-" + a.Namespace + "-" + testApplicationController,
		},
		Rules: makeTestPolicyRules(),
	}
	assert.NoError(t, ReconcilerHook(a, testClusterRole, ""))

	want := policyRulesForClusterConfig()
	assert.Equal(t, want, testClusterRole.Rules)
}

func TestReconcileArgoCD_reconcileNotApplicableClusterRole(t *testing.T) {

	setClusterConfigNamespaces(t)

	a := makeTestArgoCDForClusterConfig()
	testClusterRole := makeTestClusterRole()

	assert.NoError(t, ReconcilerHook(a, testClusterRole, ""))
	assert.Equal(t, makeTestPolicyRules(), testClusterRole.Rules)
}

func TestReconcileArgoCD_reconcileMultipleClusterRoles(t *testing.T) {

	setClusterConfigNamespaces(t)

	a := makeTestArgoCDForClusterConfig()
	testApplicableClusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: a.Name + "-" + a.Namespace + "-" + testApplicationController,
		},
		Rules: makeTestPolicyRules(),
	}

	testNotApplicableClusterRole := makeTestClusterRole()

	assert.NoError(t, ReconcilerHook(a, testApplicableClusterRole, ""))
	want := policyRulesForClusterConfig()
	assert.Equal(t, want, testApplicableClusterRole.Rules)

	assert.NoError(t, ReconcilerHook(a, testNotApplicableClusterRole, ""))
	assert.Equal(t, makeTestPolicyRules(), testNotApplicableClusterRole.Rules)
}

func TestReconcileArgoCD_testDeployment(t *testing.T) {

	setClusterConfigNamespaces(t)

	a := makeTestArgoCDForClusterConfig()
	testDeployment := makeTestDeployment()
	// ReconcilerHook should not error on a Deployment resource
	assert.NoError(t, ReconcilerHook(a, testDeployment, ""))
}

func TestReconcileArgoCD_notInClusterConfigNamespaces(t *testing.T) {

	setClusterConfigNamespaces(t)

	a := makeTestArgoCD()
	testClusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: a.Name + a.Namespace + "-" + testApplicationController,
		},
		Rules: makeTestPolicyRules(),
	}
	assert.NoError(t, ReconcilerHook(a, testClusterRole, ""))

	want := makeTestPolicyRules()
	assert.Equal(t, want, testClusterRole.Rules)
}

func TestAllowedNamespaces(t *testing.T) {

	argocdNamespace := testNamespace
	clusterConfigNamespaces := "foo,bar,argocd"
	assert.Equal(t, true, allowedNamespace(argocdNamespace, clusterConfigNamespaces))

	clusterConfigNamespaces = "foo, bar, argocd"
	assert.Equal(t, true, allowedNamespace(argocdNamespace, clusterConfigNamespaces))

	clusterConfigNamespaces = "*"
	assert.Equal(t, true, allowedNamespace(argocdNamespace, clusterConfigNamespaces))

	clusterConfigNamespaces = "foo,bar"
	assert.Equal(t, false, allowedNamespace(argocdNamespace, clusterConfigNamespaces))
}

func TestReconcileArgoCD_reconcileRedisDeployment(t *testing.T) {
	a := makeTestArgoCD()
	testDeployment := makeTestDeployment()

	testDeployment.Name = a.Name + "-" + "redis"
	want := append(getArgsForRedhatRedis(), testDeployment.Spec.Template.Spec.Containers[0].Args...)

	assert.NoError(t, ReconcilerHook(a, testDeployment, ""))
	assert.Equal(t, testDeployment.Spec.Template.Spec.Containers[0].Args, want)

	testDeployment.Name = a.Name + "-" + "not-redis"
	want = testDeployment.Spec.Template.Spec.Containers[0].Args

	assert.NoError(t, ReconcilerHook(a, testDeployment, ""))
	assert.Equal(t, testDeployment.Spec.Template.Spec.Containers[0].Args, want)
}

func TestReconcileArgoCD_reconcileRedisHaProxyDeployment(t *testing.T) {
	a := makeTestArgoCD()
	testDeployment := makeTestDeployment()

	testDeployment.Name = a.Name + "-redis-ha-haproxy"
	testDeployment.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
		Capabilities: &corev1.Capabilities{},
	}
	want := append(getCommandForRedhatRedisHaProxy(), testDeployment.Spec.Template.Spec.Containers[0].Command...)
	wantc := corev1.Capabilities{
		Add: []corev1.Capability{
			"NET_BIND_SERVICE",
		},
	}

	assert.NoError(t, ReconcilerHook(a, testDeployment, "4.11.0"))
	assert.Equal(t, testDeployment.Spec.Template.Spec.Containers[0].Command, want)
	assert.Equal(t, 0, len(testDeployment.Spec.Template.Spec.Containers[0].Args))
	assert.Equal(t, wantc, *testDeployment.Spec.Template.Spec.Containers[0].SecurityContext.Capabilities)

	testDeployment = makeTestDeployment()
	testDeployment.Name = a.Name + "-redis-ha-haproxy"
	testDeployment.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
		Capabilities: &corev1.Capabilities{},
	}

	assert.NoError(t, ReconcilerHook(a, testDeployment, "4.10.0"))
	assert.Nil(t, testDeployment.Spec.Template.Spec.Containers[0].SecurityContext.Capabilities)

	testDeployment = makeTestDeployment()
	testDeployment.Name = a.Name + "-" + "not-redis-ha-haproxy"
	want = testDeployment.Spec.Template.Spec.Containers[0].Command

	assert.NoError(t, ReconcilerHook(a, testDeployment, ""))
	assert.Equal(t, testDeployment.Spec.Template.Spec.Containers[0].Command, want)
}

func TestReconcileArgoCD_reconcileRedisHaServerStatefulSet(t *testing.T) {
	a := makeTestArgoCD()
	s := newStatefulSetWithSuffix("redis-ha-server", "redis", a)

	assert.NoError(t, ReconcilerHook(a, s, ""))

	// Check the name to ensure we're looking at the right container definition
	assert.Equal(t, s.Spec.Template.Spec.Containers[0].Name, "redis")
	assert.Equal(t, s.Spec.Template.Spec.Containers[0].Args, getArgsForRedhatHaRedisServer())
	assert.Equal(t, 0, len(s.Spec.Template.Spec.Containers[0].Command))

	// Check the name to ensure we're looking at the right container definition
	assert.Equal(t, s.Spec.Template.Spec.Containers[1].Name, "sentinel")
	assert.Equal(t, s.Spec.Template.Spec.Containers[1].Args, getArgsForRedhatHaRedisSentinel())
	assert.Equal(t, 0, len(s.Spec.Template.Spec.Containers[1].Command))

	assert.Equal(t, s.Spec.Template.Spec.InitContainers[0].Args, getArgsForRedhatHaRedisInitContainer())
	assert.Equal(t, 0, len(s.Spec.Template.Spec.InitContainers[0].Command))

	s = newStatefulSetWithSuffix("not-redis-ha-server", "redis", a)

	want0 := s.Spec.Template.Spec.Containers[0].Args
	want1 := s.Spec.Template.Spec.Containers[1].Args

	assert.NoError(t, ReconcilerHook(a, s, ""))
	assert.Equal(t, s.Spec.Template.Spec.Containers[0].Args, want0)
	assert.Equal(t, s.Spec.Template.Spec.Containers[1].Args, want1)
}

func TestReconcileArgoCD_reconcileSecrets(t *testing.T) {
	setClusterConfigNamespaces(t)

	a := makeTestArgoCDForClusterConfig()
	testSecret := &corev1.Secret{
		Data: map[string][]byte{
			"namespaces": []byte(testNamespace),
		},
	}
	assert.NoError(t, ReconcilerHook(a, testSecret, ""))
	assert.Equal(t, string(testSecret.Data["namespaces"]), "")

	a.Namespace = "someRandomNamespace"
	testSecret = &corev1.Secret{
		Data: map[string][]byte{
			"namespaces": []byte("someRandomNamespace"),
		},
	}
	assert.NoError(t, ReconcilerHook(a, testSecret, ""))
	assert.Equal(t, string(testSecret.Data["namespaces"]), "someRandomNamespace")
}

func TestAdminClusterRoleMapper(t *testing.T) {
	s := scheme.Scheme
	s.AddKnownTypes(argoapp.GroupVersion, &argoapp.ArgoCD{}, &argoapp.ArgoCDList{})

	t.Run("non-admin object returns empty result", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(s).Build()

		mapFunc := adminClusterRoleMapper(fakeClient)

		nonAdminClusterRole := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: "not-admin",
			},
		}

		result := mapFunc(context.TODO(), nonAdminClusterRole)

		assert.Empty(t, result)
	})

	t.Run("admin object with no Argo CD instances returns empty result", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(s).Build()

		mapFunc := adminClusterRoleMapper(fakeClient)

		// Create admin cluster role
		adminClusterRole := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: "admin",
			},
		}

		result := mapFunc(context.TODO(), adminClusterRole)

		assert.Empty(t, result)
	})

	t.Run("admin object with Argo CD instances returns reconcile requests", func(t *testing.T) {
		// Create test Argo CD instances
		argocd1 := &argoapp.ArgoCD{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "argocd-1",
				Namespace: "namespace-1",
			},
		}

		argocd2 := &argoapp.ArgoCD{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "argocd-2",
				Namespace: "namespace-2",
			},
		}

		// Create fake client with Argo CD instances
		fakeClient := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(argocd1, argocd2).
			Build()

		mapFunc := adminClusterRoleMapper(fakeClient)

		// Create admin cluster role
		adminClusterRole := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: "admin",
			},
		}

		result := mapFunc(context.TODO(), adminClusterRole)

		// Should return reconcile requests for both Argo CD instances
		assert.Len(t, result, 2)

		// Check that the reconcile requests contain the correct namespaced names
		expectedRequests := []reconcile.Request{
			{
				NamespacedName: client.ObjectKey{
					Name:      "argocd-1",
					Namespace: "namespace-1",
				},
			},
			{
				NamespacedName: client.ObjectKey{
					Name:      "argocd-2",
					Namespace: "namespace-2",
				},
			},
		}

		// Sort both slices to ensure consistent comparison
		sort.Slice(result, func(i, j int) bool {
			return result[i].NamespacedName.Name < result[j].NamespacedName.Name
		})
		sort.Slice(expectedRequests, func(i, j int) bool {
			return expectedRequests[i].NamespacedName.Name < expectedRequests[j].NamespacedName.Name
		})

		assert.Equal(t, expectedRequests, result)
	})
}
