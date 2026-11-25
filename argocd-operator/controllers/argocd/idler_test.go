// Copyright 2019 ArgoCD Operator Developers
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package argocd

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	testclient "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"k8s.io/utils/ptr"

	promoter "github.com/argoproj-labs/gitops-promoter/api/v1alpha1"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"

	argoproj "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/argocd-operator/common"
)

func TestIsIdled(t *testing.T) {
	tests := []struct {
		name string
		cr   *argoproj.ArgoCD
		want bool
	}{
		{
			name: "nil CR",
			cr:   nil,
			want: false,
		},
		{
			name: "no annotations",
			cr:   makeTestArgoCD(),
			want: false,
		},
		{
			name: "annotation absent among other annotations",
			cr: makeTestArgoCD(func(a *argoproj.ArgoCD) {
				a.Annotations = map[string]string{"foo": "bar"}
			}),
			want: false,
		},
		{
			name: "annotation present with empty value",
			cr: makeTestArgoCD(func(a *argoproj.ArgoCD) {
				a.Annotations = map[string]string{ArgoCDIdledAnnotationKey: ""}
			}),
			want: true,
		},
		{
			name: "annotation present with true value",
			cr: makeTestArgoCD(func(a *argoproj.ArgoCD) {
				a.Annotations = map[string]string{ArgoCDIdledAnnotationKey: "true"}
			}),
			want: true,
		},
		{
			name: "annotation present with mixed-case true value",
			cr: makeTestArgoCD(func(a *argoproj.ArgoCD) {
				a.Annotations = map[string]string{ArgoCDIdledAnnotationKey: "True"}
			}),
			want: true,
		},
		{
			name: "annotation present with false value",
			cr: makeTestArgoCD(func(a *argoproj.ArgoCD) {
				a.Annotations = map[string]string{ArgoCDIdledAnnotationKey: "false"}
			}),
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, isIdled(test.cr))
		})
	}
}

func idle(a *argoproj.ArgoCD) {
	if a.Annotations == nil {
		a.Annotations = map[string]string{}
	}
	a.Annotations[ArgoCDIdledAnnotationKey] = "true"
}

func unidle(a *argoproj.ArgoCD) {
	delete(a.Annotations, ArgoCDIdledAnnotationKey)
}

func TestReconcileArgoCD_Idled_ServerDeployment(t *testing.T) {
	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD(func(a *argoproj.ArgoCD) {
		a.Spec.Server.Autoscale.Enabled = true
	})

	resObjs := []client.Object{a}
	subresObjs := []client.Object{a}
	runtimeObjs := []runtime.Object{}
	sch := makeTestReconcilerScheme(argoproj.AddToScheme, promoter.AddToScheme, apiregistrationv1.AddToScheme)
	cl := makeTestReconcilerClient(sch, resObjs, subresObjs, runtimeObjs)
	r := makeTestReconciler(cl, sch, testclient.NewSimpleClientset())

	assert.NoError(t, r.reconcileServerDeployment(a, false))
	assert.NoError(t, r.reconcileServerHPA(a))

	hpa := &autoscalingv1.HorizontalPodAutoscaler{}
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-server", Namespace: a.Namespace}, hpa))

	// Idle: replicas should be forced to 0 even though autoscale is enabled, and the HPA should be removed
	// so the Kubernetes HPA controller doesn't scale the Deployment back up.
	idle(a)
	assert.NoError(t, r.reconcileServerDeployment(a, false))
	assert.NoError(t, r.reconcileServerHPA(a))

	deployment := &appsv1.Deployment{}
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-server", Namespace: a.Namespace}, deployment))
	assert.NotNil(t, deployment.Spec.Replicas)
	assert.Equal(t, int32(0), *deployment.Spec.Replicas)

	err := r.Get(context.TODO(), types.NamespacedName{Name: "argocd-server", Namespace: a.Namespace}, hpa)
	assert.True(t, apierrors.IsNotFound(err))

	// Un-idle: HPA should come back. With autoscale enabled the operator deliberately avoids touching
	// .Spec.Replicas once un-idled (same as it always does when autoscale owns the Deployment) - the
	// K8s HPA controller is responsible for scaling it back up from here.
	unidle(a)
	assert.NoError(t, r.reconcileServerDeployment(a, false))
	assert.NoError(t, r.reconcileServerHPA(a))

	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-server", Namespace: a.Namespace}, deployment))
	assert.NotNil(t, deployment.Spec.Replicas)
	assert.Equal(t, int32(0), *deployment.Spec.Replicas)

	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-server", Namespace: a.Namespace}, hpa))
}

func TestReconcileArgoCD_Idled_ServerDeployment_NoAutoscale(t *testing.T) {
	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD()

	resObjs := []client.Object{a}
	subresObjs := []client.Object{a}
	runtimeObjs := []runtime.Object{}
	sch := makeTestReconcilerScheme(argoproj.AddToScheme, promoter.AddToScheme, apiregistrationv1.AddToScheme)
	cl := makeTestReconcilerClient(sch, resObjs, subresObjs, runtimeObjs)
	r := makeTestReconciler(cl, sch, testclient.NewSimpleClientset())

	assert.NoError(t, r.reconcileServerDeployment(a, false))

	deployment := &appsv1.Deployment{}
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-server", Namespace: a.Namespace}, deployment))
	assert.Nil(t, deployment.Spec.Replicas)

	idle(a)
	assert.NoError(t, r.reconcileServerDeployment(a, false))
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-server", Namespace: a.Namespace}, deployment))
	assert.NotNil(t, deployment.Spec.Replicas)
	assert.Equal(t, int32(0), *deployment.Spec.Replicas)

	// Un-idle with no autoscale: the Deployment should fully revert to the unmanaged (nil => k8s default) state.
	unidle(a)
	assert.NoError(t, r.reconcileServerDeployment(a, false))
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-server", Namespace: a.Namespace}, deployment))
	assert.Nil(t, deployment.Spec.Replicas)
}

func TestReconcileArgoCD_Idled_RepoServerDeployment(t *testing.T) {
	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD()

	resObjs := []client.Object{a}
	subresObjs := []client.Object{a}
	runtimeObjs := []runtime.Object{}
	sch := makeTestReconcilerScheme(argoproj.AddToScheme, promoter.AddToScheme, apiregistrationv1.AddToScheme)
	cl := makeTestReconcilerClient(sch, resObjs, subresObjs, runtimeObjs)
	r := makeTestReconciler(cl, sch, testclient.NewSimpleClientset())

	assert.NoError(t, r.reconcileRepoDeployment(a, false))

	deployment := &appsv1.Deployment{}
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-repo-server", Namespace: a.Namespace}, deployment))
	assert.Nil(t, deployment.Spec.Replicas)

	idle(a)
	assert.NoError(t, r.reconcileRepoDeployment(a, false))
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-repo-server", Namespace: a.Namespace}, deployment))
	assert.NotNil(t, deployment.Spec.Replicas)
	assert.Equal(t, int32(0), *deployment.Spec.Replicas)

	unidle(a)
	assert.NoError(t, r.reconcileRepoDeployment(a, false))
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-repo-server", Namespace: a.Namespace}, deployment))
	assert.Nil(t, deployment.Spec.Replicas)
}

func TestReconcileArgoCD_Idled_ApplicationControllerStatefulSet(t *testing.T) {
	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD()

	resObjs := []client.Object{a}
	subresObjs := []client.Object{a}
	runtimeObjs := []runtime.Object{}
	sch := makeTestReconcilerScheme(argoproj.AddToScheme, promoter.AddToScheme, apiregistrationv1.AddToScheme)
	cl := makeTestReconcilerClient(sch, resObjs, subresObjs, runtimeObjs)
	r := makeTestReconciler(cl, sch, testclient.NewSimpleClientset())

	assert.NoError(t, r.reconcileApplicationControllerStatefulSet(a, false))

	ss := &appsv1.StatefulSet{}
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: testApplicationController, Namespace: a.Namespace}, ss))
	assert.Equal(t, int32(1), *ss.Spec.Replicas)

	idle(a)
	assert.NoError(t, r.reconcileApplicationControllerStatefulSet(a, false))
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: testApplicationController, Namespace: a.Namespace}, ss))
	assert.Equal(t, int32(0), *ss.Spec.Replicas)

	unidle(a)
	assert.NoError(t, r.reconcileApplicationControllerStatefulSet(a, false))
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: testApplicationController, Namespace: a.Namespace}, ss))
	assert.Equal(t, int32(1), *ss.Spec.Replicas)
}

func TestReconcileArgoCD_Idled_ApplicationSetDeployment(t *testing.T) {
	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD()
	a.Spec.ApplicationSet = &argoproj.ArgoCDApplicationSet{}

	resObjs := []client.Object{a}
	subresObjs := []client.Object{a}
	runtimeObjs := []runtime.Object{}
	sch := makeTestReconcilerScheme(argoproj.AddToScheme, promoter.AddToScheme, apiregistrationv1.AddToScheme)
	cl := makeTestReconcilerClient(sch, resObjs, subresObjs, runtimeObjs)
	r := makeTestReconciler(cl, sch, testclient.NewSimpleClientset())

	sa := corev1.ServiceAccount{}
	assert.NoError(t, r.reconcileApplicationSetDeployment(a, &sa))

	deployment := &appsv1.Deployment{}
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-applicationset-controller", Namespace: a.Namespace}, deployment))
	assert.Equal(t, int32(1), *deployment.Spec.Replicas)

	idle(a)
	assert.NoError(t, r.reconcileApplicationSetDeployment(a, &sa))
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-applicationset-controller", Namespace: a.Namespace}, deployment))
	assert.Equal(t, int32(0), *deployment.Spec.Replicas)

	unidle(a)
	assert.NoError(t, r.reconcileApplicationSetDeployment(a, &sa))
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-applicationset-controller", Namespace: a.Namespace}, deployment))
	assert.Equal(t, int32(1), *deployment.Spec.Replicas)
}

func TestReconcileArgoCD_Idled_DexDeployment(t *testing.T) {
	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD()
	a.Spec.SSO = &argoproj.ArgoCDSSOSpec{
		Provider: argoproj.SSOProviderTypeDex,
	}

	resObjs := []client.Object{a}
	subresObjs := []client.Object{a}
	runtimeObjs := []runtime.Object{}
	sch := makeTestReconcilerScheme(argoproj.AddToScheme, promoter.AddToScheme, apiregistrationv1.AddToScheme)
	cl := makeTestReconcilerClient(sch, resObjs, subresObjs, runtimeObjs)
	r := makeTestReconciler(cl, sch, testclient.NewSimpleClientset())

	assert.NoError(t, r.reconcileDexDeployment(a))

	deployment := &appsv1.Deployment{}
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-dex-server", Namespace: a.Namespace}, deployment))
	assert.Equal(t, int32(1), *deployment.Spec.Replicas)

	idle(a)
	assert.NoError(t, r.reconcileDexDeployment(a))
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-dex-server", Namespace: a.Namespace}, deployment))
	assert.Equal(t, int32(0), *deployment.Spec.Replicas)

	unidle(a)
	assert.NoError(t, r.reconcileDexDeployment(a))
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-dex-server", Namespace: a.Namespace}, deployment))
	assert.Equal(t, int32(1), *deployment.Spec.Replicas)
}

func TestReconcileArgoCD_Idled_RedisDeployment(t *testing.T) {
	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD()

	resObjs := []client.Object{a}
	subresObjs := []client.Object{a}
	runtimeObjs := []runtime.Object{}
	sch := makeTestReconcilerScheme(argoproj.AddToScheme, promoter.AddToScheme, apiregistrationv1.AddToScheme)
	cl := makeTestReconcilerClient(sch, resObjs, subresObjs, runtimeObjs)
	r := makeTestReconciler(cl, sch, testclient.NewSimpleClientset())

	assert.NoError(t, r.reconcileRedisDeployment(a, false))

	deployment := &appsv1.Deployment{}
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-redis", Namespace: a.Namespace}, deployment))
	assert.Equal(t, int32(1), *deployment.Spec.Replicas)

	idle(a)
	assert.NoError(t, r.reconcileRedisDeployment(a, false))
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-redis", Namespace: a.Namespace}, deployment))
	assert.Equal(t, int32(0), *deployment.Spec.Replicas)

	unidle(a)
	assert.NoError(t, r.reconcileRedisDeployment(a, false))
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-redis", Namespace: a.Namespace}, deployment))
	assert.Equal(t, int32(1), *deployment.Spec.Replicas)
}

func TestReconcileArgoCD_Idled_RedisHA(t *testing.T) {
	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD(func(a *argoproj.ArgoCD) {
		a.Spec.HA.Enabled = true
	})

	resObjs := []client.Object{a}
	subresObjs := []client.Object{a}
	runtimeObjs := []runtime.Object{}
	sch := makeTestReconcilerScheme(argoproj.AddToScheme, promoter.AddToScheme, apiregistrationv1.AddToScheme)
	cl := makeTestReconcilerClient(sch, resObjs, subresObjs, runtimeObjs)
	r := makeTestReconciler(cl, sch, testclient.NewSimpleClientset())

	assert.NoError(t, r.reconcileRedisHAProxyDeployment(a))
	assert.NoError(t, r.reconcileRedisStatefulSet(a))

	haproxy := &appsv1.Deployment{}
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-redis-ha-haproxy", Namespace: a.Namespace}, haproxy))
	assert.Equal(t, int32(3), *haproxy.Spec.Replicas)

	ss := &appsv1.StatefulSet{}
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-redis-ha-server", Namespace: a.Namespace}, ss))
	assert.Equal(t, int32(3), *ss.Spec.Replicas)

	idle(a)
	assert.NoError(t, r.reconcileRedisHAProxyDeployment(a))
	assert.NoError(t, r.reconcileRedisStatefulSet(a))

	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-redis-ha-haproxy", Namespace: a.Namespace}, haproxy))
	assert.Equal(t, int32(0), *haproxy.Spec.Replicas)

	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-redis-ha-server", Namespace: a.Namespace}, ss))
	assert.Equal(t, int32(0), *ss.Spec.Replicas)

	unidle(a)
	assert.NoError(t, r.reconcileRedisHAProxyDeployment(a))
	assert.NoError(t, r.reconcileRedisStatefulSet(a))

	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-redis-ha-haproxy", Namespace: a.Namespace}, haproxy))
	assert.Equal(t, int32(3), *haproxy.Spec.Replicas)

	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-redis-ha-server", Namespace: a.Namespace}, ss))
	assert.Equal(t, int32(3), *ss.Spec.Replicas)
}

func TestReconcileArgoCD_Idled_CommitServerDeployment(t *testing.T) {
	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD(func(a *argoproj.ArgoCD) {
		a.Spec.SourceHydrator.Enabled = ptr.To(true)
	})

	resObjs := []client.Object{a}
	subresObjs := []client.Object{a}
	runtimeObjs := []runtime.Object{}
	sch := makeTestReconcilerScheme(argoproj.AddToScheme, promoter.AddToScheme, apiregistrationv1.AddToScheme)
	cl := makeTestReconcilerClient(sch, resObjs, subresObjs, runtimeObjs)
	r := makeTestReconciler(cl, sch, testclient.NewSimpleClientset())

	assert.NoError(t, r.reconcileCommitServerDeployment(a))

	deployment := &appsv1.Deployment{}
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-commit-server", Namespace: a.Namespace}, deployment))
	assert.Equal(t, int32(1), *deployment.Spec.Replicas)

	idle(a)
	assert.NoError(t, r.reconcileCommitServerDeployment(a))
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-commit-server", Namespace: a.Namespace}, deployment))
	assert.Equal(t, int32(0), *deployment.Spec.Replicas)

	unidle(a)
	assert.NoError(t, r.reconcileCommitServerDeployment(a))
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: "argocd-commit-server", Namespace: a.Namespace}, deployment))
	assert.Equal(t, int32(1), *deployment.Spec.Replicas)
}

func TestReconcileArgoCD_Idled_NotificationsDeployment(t *testing.T) {
	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD(func(a *argoproj.ArgoCD) {
		a.Spec.Notifications.Enabled = true
	})

	resObjs := []client.Object{a}
	subresObjs := []client.Object{a}
	runtimeObjs := []runtime.Object{}
	sch := makeTestReconcilerScheme(argoproj.AddToScheme, promoter.AddToScheme, apiregistrationv1.AddToScheme)
	cl := makeTestReconcilerClient(sch, resObjs, subresObjs, runtimeObjs)
	r := makeTestReconciler(cl, sch, testclient.NewSimpleClientset())

	sa := corev1.ServiceAccount{}
	assert.NoError(t, r.reconcileNotificationsDeployment(a, &sa))

	deployment := &appsv1.Deployment{}
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: a.Name + "-notifications-controller", Namespace: a.Namespace}, deployment))
	assert.Equal(t, int32(1), *deployment.Spec.Replicas)

	idle(a)
	assert.NoError(t, r.reconcileNotificationsDeployment(a, &sa))
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: a.Name + "-notifications-controller", Namespace: a.Namespace}, deployment))
	assert.Equal(t, int32(0), *deployment.Spec.Replicas)

	unidle(a)
	assert.NoError(t, r.reconcileNotificationsDeployment(a, &sa))
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: a.Name + "-notifications-controller", Namespace: a.Namespace}, deployment))
	assert.Equal(t, int32(1), *deployment.Spec.Replicas)
}

func TestReconcileArgoCD_Idled_ImageUpdaterDeployment(t *testing.T) {
	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD(func(a *argoproj.ArgoCD) {
		a.Spec.ImageUpdater.Enabled = true
	})

	resObjs := []client.Object{a}
	subresObjs := []client.Object{a}
	runtimeObjs := []runtime.Object{}
	sch := makeTestReconcilerScheme(argoproj.AddToScheme, promoter.AddToScheme, apiregistrationv1.AddToScheme)
	cl := makeTestReconcilerClient(sch, resObjs, subresObjs, runtimeObjs)
	r := makeTestReconciler(cl, sch, testclient.NewSimpleClientset())

	sa := corev1.ServiceAccount{}
	assert.NoError(t, r.reconcileImageUpdaterDeployment(a, &sa, "", nil))

	deployment := &appsv1.Deployment{}
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: generateResourceName(common.ArgoCDImageUpdaterControllerComponent, a), Namespace: a.Namespace}, deployment))
	assert.Equal(t, int32(1), *deployment.Spec.Replicas)

	idle(a)
	assert.NoError(t, r.reconcileImageUpdaterDeployment(a, &sa, "", nil))
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: generateResourceName(common.ArgoCDImageUpdaterControllerComponent, a), Namespace: a.Namespace}, deployment))
	assert.Equal(t, int32(0), *deployment.Spec.Replicas)

	unidle(a)
	assert.NoError(t, r.reconcileImageUpdaterDeployment(a, &sa, "", nil))
	assert.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: generateResourceName(common.ArgoCDImageUpdaterControllerComponent, a), Namespace: a.Namespace}, deployment))
	assert.Equal(t, int32(1), *deployment.Spec.Replicas)
}
