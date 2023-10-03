package openshift

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/assert"
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
			Name:      a.Name + "-" + a.Namespace + "-" + testApplicationController,
			Namespace: a.Namespace,
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

	testDeployment.ObjectMeta.Name = a.Name + "-" + "redis"
	want := append(getArgsForRedhatRedis(), testDeployment.Spec.Template.Spec.Containers[0].Args...)

	assert.NoError(t, ReconcilerHook(a, testDeployment, ""))
	assert.Equal(t, testDeployment.Spec.Template.Spec.Containers[0].Args, want)

	testDeployment.ObjectMeta.Name = a.Name + "-" + "not-redis"
	want = testDeployment.Spec.Template.Spec.Containers[0].Args

	assert.NoError(t, ReconcilerHook(a, testDeployment, ""))
	assert.Equal(t, testDeployment.Spec.Template.Spec.Containers[0].Args, want)
}

func TestReconcileArgoCD_reconcileRedisHaProxyDeployment(t *testing.T) {
	a := makeTestArgoCD()
	testDeployment := makeTestDeployment()

	testDeployment.ObjectMeta.Name = a.Name + "-redis-ha-haproxy"
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
	testDeployment.ObjectMeta.Name = a.Name + "-redis-ha-haproxy"
	testDeployment.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
		Capabilities: &corev1.Capabilities{},
	}

	assert.NoError(t, ReconcilerHook(a, testDeployment, "4.10.0"))
	assert.Nil(t, testDeployment.Spec.Template.Spec.Containers[0].SecurityContext.Capabilities)

	testDeployment = makeTestDeployment()
	testDeployment.ObjectMeta.Name = a.Name + "-" + "not-redis-ha-haproxy"
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
