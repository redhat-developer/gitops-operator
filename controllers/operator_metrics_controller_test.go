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
	"path/filepath"
	"testing"
	"time"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"gotest.tools/assert"
	is "gotest.tools/assert/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const testOperatorNamespace = "openshift-gitops-operator"

type fakeTokenRequester struct {
	token  string
	expiry time.Time
	err    error
}

func (f *fakeTokenRequester) RequestToken(_ context.Context, _, _ string, _ int64) (string, time.Time, error) {
	if f.err != nil {
		return "", time.Time{}, f.err
	}
	return f.token, f.expiry, nil
}

func writeOperatorNamespaceFile(t *testing.T, namespace string) {
	t.Helper()
	dir := t.TempDir()
	namespaceFile := filepath.Join(dir, "namespace")
	if err := os.WriteFile(namespaceFile, []byte(namespace), 0o644); err != nil {
		t.Fatal(err)
	}
	oldPath := operatorPodNamespacePath
	operatorPodNamespacePath = namespaceFile
	t.Cleanup(func() {
		operatorPodNamespacePath = oldPath
	})
}

func newOperatorMetricsTokenScheme() *runtime.Scheme {
	s := scheme.Scheme
	s.AddKnownTypes(monitoringv1.SchemeGroupVersion, &monitoringv1.ServiceMonitor{})
	return s
}

func newOperatorMetricsClientBuilder(s *runtime.Scheme) *fake.ClientBuilder {
	return fake.NewClientBuilder().WithScheme(s).WithInterceptorFuncs(interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			secret, ok := obj.(*corev1.Secret)
			if !ok {
				return c.Update(ctx, obj, opts...)
			}
			existing := &corev1.Secret{}
			if err := c.Get(ctx, client.ObjectKeyFromObject(secret), existing); err != nil {
				return c.Update(ctx, obj, opts...)
			}
			if existing.Type != secret.Type {
				return fmt.Errorf("secret type is immutable")
			}
			return c.Update(ctx, obj, opts...)
		},
	})
}

func newOperatorMetricsServiceMonitor(namespace string, useLegacyAuth bool) *monitoringv1.ServiceMonitor {
	sm := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorMetricsMonitorName,
			Namespace: namespace,
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			Endpoints: []monitoringv1.Endpoint{
				{
					Interval: monitoringv1.Duration("30s"),
					Path:     "/metrics",
					Port:     "metrics",
					Scheme:   "https",
					TLSConfig: &monitoringv1.TLSConfig{
						SafeTLSConfig: monitoringv1.SafeTLSConfig{
							ServerName: ptr.To("old-server-name"),
						},
					},
				},
			},
		},
	}
	if useLegacyAuth {
		sm.Spec.Endpoints[0].BearerTokenSecret = &corev1.SecretKeySelector{ //nolint:staticcheck // SA1019: migrate deprecated bearerTokenSecret to authorization
			LocalObjectReference: corev1.LocalObjectReference{
				Name: operatorMetricsBearerTokenSecretName,
			},
			Key: operatorMetricsBearerTokenKey,
		}
	}
	return sm
}

func TestGetOperatorNamespace_trimsNewline(t *testing.T) {
	dir := t.TempDir()
	namespaceFile := filepath.Join(dir, "namespace")
	if err := os.WriteFile(namespaceFile, []byte("openshift-gitops-operator\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldPath := operatorPodNamespacePath
	operatorPodNamespacePath = namespaceFile
	t.Cleanup(func() {
		operatorPodNamespacePath = oldPath
	})

	ns, err := getOperatorNamespace()
	assert.NilError(t, err)
	assert.Equal(t, ns, "openshift-gitops-operator")
}

func TestBearerTokenRenewalLead(t *testing.T) {
	lead := bearerTokenRenewalLead()
	assert.Assert(t, lead > 11*time.Minute && lead <= 12*time.Minute)
}

func TestIsLegacyMetricsBearerTokenSecret(t *testing.T) {
	assert.Assert(t, isLegacyMetricsBearerTokenSecret(&corev1.Secret{
		Type: corev1.SecretTypeServiceAccountToken,
	}))
	assert.Assert(t, !isLegacyMetricsBearerTokenSecret(&corev1.Secret{
		Type: corev1.SecretTypeOpaque,
	}))
}

func TestMetricsBearerTokenSecretMustReplace(t *testing.T) {
	legacy := &corev1.Secret{Type: corev1.SecretTypeServiceAccountToken}
	assert.Assert(t, metricsBearerTokenSecretMustReplace(legacy))

	opaque := &corev1.Secret{Type: corev1.SecretTypeOpaque}
	assert.Assert(t, !metricsBearerTokenSecretMustReplace(opaque))

	other := &corev1.Secret{Type: corev1.SecretTypeDockercfg}
	assert.Assert(t, metricsBearerTokenSecretMustReplace(other))
}

func TestOperatorMetricsTokenReconciler_replacesIncompatibleSecretType(t *testing.T) {
	writeOperatorNamespaceFile(t, testOperatorNamespace)

	s := newOperatorMetricsTokenScheme()
	incompatibleSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorMetricsBearerTokenSecretName,
			Namespace: testOperatorNamespace,
		},
		Type: corev1.SecretTypeDockercfg,
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(incompatibleSecret).Build()

	expiry := time.Now().Add(operatorMetricsTokenExpiry)
	r := &OperatorMetricsTokenReconciler{
		Client: c,
		Scheme: s,
		TokenRequester: &fakeTokenRequester{
			token:  "replaced-token",
			expiry: expiry,
		},
	}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      operatorMetricsMonitorName,
			Namespace: testOperatorNamespace,
		},
	})
	assert.NilError(t, err)

	secret := &corev1.Secret{}
	err = c.Get(context.Background(), types.NamespacedName{
		Name:      operatorMetricsBearerTokenSecretName,
		Namespace: testOperatorNamespace,
	}, secret)
	assert.NilError(t, err)
	assert.Equal(t, secret.Type, corev1.SecretTypeOpaque)
	assert.Equal(t, string(secret.Data[operatorMetricsBearerTokenKey]), "replaced-token")
}

func TestEvaluateBearerTokenRenewal(t *testing.T) {
	now := time.Now()
	expiry := now.Add(operatorMetricsTokenExpiry)

	requeueAfter, needsRefresh := evaluateBearerTokenRenewal(expiry, now)
	assert.Assert(t, !needsRefresh)
	assert.Assert(t, requeueAfter > 47*time.Minute && requeueAfter <= 48*time.Minute)

	lead := bearerTokenRenewalLead()
	_, needsRefresh = evaluateBearerTokenRenewal(now.Add(lead), now)
	assert.Assert(t, needsRefresh)

	_, needsRefresh = evaluateBearerTokenRenewal(now.Add(-time.Minute), now)
	assert.Assert(t, needsRefresh)
}

func TestOperatorMetricsTokenReconciler_createsServiceMonitorAfterSecret(t *testing.T) {
	writeOperatorNamespaceFile(t, testOperatorNamespace)

	s := newOperatorMetricsTokenScheme()
	c := fake.NewClientBuilder().WithScheme(s).Build()

	r := &OperatorMetricsTokenReconciler{
		Client: c,
		Scheme: s,
		TokenRequester: &fakeTokenRequester{
			token:  "test-token",
			expiry: time.Now().Add(operatorMetricsTokenExpiry),
		},
	}

	result, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      operatorMetricsMonitorName,
			Namespace: testOperatorNamespace,
		},
	})
	assert.NilError(t, err)
	assert.Assert(t, result.RequeueAfter > 0)

	secret := &corev1.Secret{}
	err = c.Get(context.Background(), types.NamespacedName{
		Name:      operatorMetricsBearerTokenSecretName,
		Namespace: testOperatorNamespace,
	}, secret)
	assert.NilError(t, err)
	assert.Equal(t, secret.Type, corev1.SecretTypeOpaque)
	assert.Equal(t, string(secret.Data[operatorMetricsBearerTokenKey]), "test-token")

	serviceMonitor := &monitoringv1.ServiceMonitor{}
	err = c.Get(context.Background(), types.NamespacedName{
		Name:      operatorMetricsMonitorName,
		Namespace: testOperatorNamespace,
	}, serviceMonitor)
	assert.NilError(t, err)
	assert.Assert(t, serviceMonitor.Spec.Endpoints[0].Authorization != nil)
	assert.Equal(t, serviceMonitor.Spec.Endpoints[0].Authorization.Credentials.Name, operatorMetricsBearerTokenSecretName)
	assert.Equal(t, *serviceMonitor.Spec.Endpoints[0].TLSConfig.ServerName, operatorMetricsServiceName+"."+testOperatorNamespace+".svc")
}

func TestOperatorMetricsTokenReconciler_migratesServiceMonitorAuth(t *testing.T) {
	writeOperatorNamespaceFile(t, testOperatorNamespace)

	s := newOperatorMetricsTokenScheme()
	serviceMonitor := newOperatorMetricsServiceMonitor(testOperatorNamespace, true)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(serviceMonitor).Build()

	r := &OperatorMetricsTokenReconciler{
		Client: c,
		Scheme: s,
		TokenRequester: &fakeTokenRequester{
			token:  "test-token",
			expiry: time.Now().Add(operatorMetricsTokenExpiry),
		},
	}

	result, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      operatorMetricsMonitorName,
			Namespace: testOperatorNamespace,
		},
	})
	assert.NilError(t, err)
	assert.Assert(t, result.RequeueAfter > 0)

	updatedSM := &monitoringv1.ServiceMonitor{}
	err = c.Get(context.Background(), types.NamespacedName{
		Name:      operatorMetricsMonitorName,
		Namespace: testOperatorNamespace,
	}, updatedSM)
	assert.NilError(t, err)
	assert.Assert(t, is.Nil(updatedSM.Spec.Endpoints[0].BearerTokenSecret)) //nolint:staticcheck // SA1019: migrate deprecated bearerTokenSecret to authorization
	assert.Assert(t, updatedSM.Spec.Endpoints[0].Authorization != nil)
	assert.Equal(t, updatedSM.Spec.Endpoints[0].Authorization.Type, "Bearer")
	assert.Equal(t, updatedSM.Spec.Endpoints[0].Authorization.Credentials.Name, operatorMetricsBearerTokenSecretName)
	assert.Equal(t, *updatedSM.Spec.Endpoints[0].TLSConfig.ServerName, operatorMetricsServiceName+"."+testOperatorNamespace+".svc")
}

func TestOperatorMetricsTokenReconciler_replacesLegacySecret(t *testing.T) {
	writeOperatorNamespaceFile(t, testOperatorNamespace)

	s := newOperatorMetricsTokenScheme()
	serviceMonitor := newOperatorMetricsServiceMonitor(testOperatorNamespace, false)
	serviceMonitor.Spec.Endpoints[0].Authorization = &monitoringv1.SafeAuthorization{
		Type: "Bearer",
		Credentials: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: operatorMetricsBearerTokenSecretName,
			},
			Key: operatorMetricsBearerTokenKey,
		},
	}
	legacySecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorMetricsBearerTokenSecretName,
			Namespace: testOperatorNamespace,
		},
		Type: corev1.SecretTypeServiceAccountToken,
	}
	c := newOperatorMetricsClientBuilder(s).WithObjects(serviceMonitor, legacySecret).Build()

	expiry := time.Now().Add(operatorMetricsTokenExpiry)
	r := &OperatorMetricsTokenReconciler{
		Client: c,
		Scheme: s,
		TokenRequester: &fakeTokenRequester{
			token:  "new-token",
			expiry: expiry,
		},
	}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      operatorMetricsMonitorName,
			Namespace: testOperatorNamespace,
		},
	})
	assert.NilError(t, err)

	secret := &corev1.Secret{}
	err = c.Get(context.Background(), types.NamespacedName{
		Name:      operatorMetricsBearerTokenSecretName,
		Namespace: testOperatorNamespace,
	}, secret)
	assert.NilError(t, err)
	assert.Equal(t, secret.Type, corev1.SecretTypeOpaque)
	assert.Equal(t, string(secret.Data[operatorMetricsBearerTokenKey]), "new-token")
	parsedExpiry, err := parseBearerTokenTimestamp(secret.Data[operatorMetricsBearerTokenExpiryKey])
	assert.NilError(t, err)
	assert.Equal(t, parsedExpiry.UTC().Format(time.RFC3339), expiry.UTC().Format(time.RFC3339))
	_, hasIssuedAt := secret.Data["issuedAt"]
	assert.Assert(t, !hasIssuedAt)
}

func TestOperatorMetricsTokenReconciler_keepsLegacySecretIfTokenRequestFails(t *testing.T) {
	writeOperatorNamespaceFile(t, testOperatorNamespace)

	s := newOperatorMetricsTokenScheme()
	legacySecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorMetricsBearerTokenSecretName,
			Namespace: testOperatorNamespace,
		},
		Type: corev1.SecretTypeServiceAccountToken,
		Data: map[string][]byte{
			operatorMetricsBearerTokenKey: []byte("legacy-token"),
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(legacySecret).Build()

	r := &OperatorMetricsTokenReconciler{
		Client: c,
		Scheme: s,
		TokenRequester: &fakeTokenRequester{
			err: fmt.Errorf("token request failed"),
		},
	}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      operatorMetricsMonitorName,
			Namespace: testOperatorNamespace,
		},
	})
	assert.ErrorContains(t, err, "token request failed")

	secret := &corev1.Secret{}
	err = c.Get(context.Background(), types.NamespacedName{
		Name:      operatorMetricsBearerTokenSecretName,
		Namespace: testOperatorNamespace,
	}, secret)
	assert.NilError(t, err)
	assert.Equal(t, secret.Type, corev1.SecretTypeServiceAccountToken)
	assert.Equal(t, string(secret.Data[operatorMetricsBearerTokenKey]), "legacy-token")

	serviceMonitor := &monitoringv1.ServiceMonitor{}
	err = c.Get(context.Background(), types.NamespacedName{
		Name:      operatorMetricsMonitorName,
		Namespace: testOperatorNamespace,
	}, serviceMonitor)
	assert.ErrorContains(t, err, "not found")
}

func TestOperatorMetricsTokenReconciler_refreshesExpiredToken(t *testing.T) {
	writeOperatorNamespaceFile(t, testOperatorNamespace)

	s := newOperatorMetricsTokenScheme()
	serviceMonitor := newOperatorMetricsServiceMonitor(testOperatorNamespace, false)
	serviceMonitor.Spec.Endpoints[0].Authorization = &monitoringv1.SafeAuthorization{
		Type: "Bearer",
		Credentials: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: operatorMetricsBearerTokenSecretName,
			},
			Key: operatorMetricsBearerTokenKey,
		},
	}
	expiredSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorMetricsBearerTokenSecretName,
			Namespace: testOperatorNamespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			operatorMetricsBearerTokenKey:       []byte("old-token"),
			operatorMetricsBearerTokenExpiryKey: []byte(time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)),
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(serviceMonitor, expiredSecret).Build()

	newExpiry := time.Now().Add(operatorMetricsTokenExpiry)
	r := &OperatorMetricsTokenReconciler{
		Client: c,
		Scheme: s,
		TokenRequester: &fakeTokenRequester{
			token:  "refreshed-token",
			expiry: newExpiry,
		},
	}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      operatorMetricsMonitorName,
			Namespace: testOperatorNamespace,
		},
	})
	assert.NilError(t, err)

	secret := &corev1.Secret{}
	err = c.Get(context.Background(), types.NamespacedName{
		Name:      operatorMetricsBearerTokenSecretName,
		Namespace: testOperatorNamespace,
	}, secret)
	assert.NilError(t, err)
	assert.Equal(t, string(secret.Data[operatorMetricsBearerTokenKey]), "refreshed-token")
}

func TestOperatorMetricsTokenReconciler_refreshesSecretWithMissingToken(t *testing.T) {
	writeOperatorNamespaceFile(t, testOperatorNamespace)

	s := newOperatorMetricsTokenScheme()
	serviceMonitor := newOperatorMetricsServiceMonitor(testOperatorNamespace, false)
	invalidSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorMetricsBearerTokenSecretName,
			Namespace: testOperatorNamespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			operatorMetricsBearerTokenExpiryKey: []byte(time.Now().Add(operatorMetricsTokenExpiry).UTC().Format(time.RFC3339)),
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(serviceMonitor, invalidSecret).Build()

	newExpiry := time.Now().Add(operatorMetricsTokenExpiry)
	r := &OperatorMetricsTokenReconciler{
		Client: c,
		Scheme: s,
		TokenRequester: &fakeTokenRequester{
			token:  "minted-token",
			expiry: newExpiry,
		},
	}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      operatorMetricsMonitorName,
			Namespace: testOperatorNamespace,
		},
	})
	assert.NilError(t, err)

	secret := &corev1.Secret{}
	err = c.Get(context.Background(), types.NamespacedName{
		Name:      operatorMetricsBearerTokenSecretName,
		Namespace: testOperatorNamespace,
	}, secret)
	assert.NilError(t, err)
	assert.Equal(t, string(secret.Data[operatorMetricsBearerTokenKey]), "minted-token")
}

func TestOperatorMetricsTokenReconciler_schedulesRenewalFromExpiry(t *testing.T) {
	writeOperatorNamespaceFile(t, testOperatorNamespace)

	s := newOperatorMetricsTokenScheme()
	serviceMonitor := newOperatorMetricsServiceMonitor(testOperatorNamespace, false)
	expiry := time.Now().Add(operatorMetricsTokenExpiry)
	validSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorMetricsBearerTokenSecretName,
			Namespace: testOperatorNamespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			operatorMetricsBearerTokenKey:       []byte("current-token"),
			operatorMetricsBearerTokenExpiryKey: []byte(expiry.UTC().Format(time.RFC3339)),
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(serviceMonitor, validSecret).Build()

	r := &OperatorMetricsTokenReconciler{
		Client: c,
		Scheme: s,
		TokenRequester: &fakeTokenRequester{
			token:  "unexpected-token",
			expiry: time.Now().Add(operatorMetricsTokenExpiry),
		},
	}

	result, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      operatorMetricsMonitorName,
			Namespace: testOperatorNamespace,
		},
	})
	assert.NilError(t, err)
	assert.Assert(t, result.RequeueAfter > 47*time.Minute && result.RequeueAfter <= 48*time.Minute)

	secret := &corev1.Secret{}
	err = c.Get(context.Background(), types.NamespacedName{
		Name:      operatorMetricsBearerTokenSecretName,
		Namespace: testOperatorNamespace,
	}, secret)
	assert.NilError(t, err)
	assert.Equal(t, string(secret.Data[operatorMetricsBearerTokenKey]), "current-token")

	result, err = r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      operatorMetricsMonitorName,
			Namespace: testOperatorNamespace,
		},
	})
	assert.NilError(t, err)
	assert.Assert(t, result.RequeueAfter > 47*time.Minute && result.RequeueAfter <= 48*time.Minute)
}

func TestOperatorMetricsTokenReconciler_refreshesTokenAtRenewalDeadline(t *testing.T) {
	writeOperatorNamespaceFile(t, testOperatorNamespace)

	s := newOperatorMetricsTokenScheme()
	serviceMonitor := newOperatorMetricsServiceMonitor(testOperatorNamespace, false)
	serviceMonitor.Spec.Endpoints[0].Authorization = &monitoringv1.SafeAuthorization{
		Type: "Bearer",
		Credentials: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: operatorMetricsBearerTokenSecretName,
			},
			Key: operatorMetricsBearerTokenKey,
		},
	}
	now := time.Now()
	dueSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorMetricsBearerTokenSecretName,
			Namespace: testOperatorNamespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			operatorMetricsBearerTokenKey:       []byte("old-token"),
			operatorMetricsBearerTokenExpiryKey: []byte(now.Add(bearerTokenRenewalLead()).UTC().Format(time.RFC3339)),
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(serviceMonitor, dueSecret).Build()

	newExpiry := time.Now().Add(operatorMetricsTokenExpiry)
	r := &OperatorMetricsTokenReconciler{
		Client: c,
		Scheme: s,
		TokenRequester: &fakeTokenRequester{
			token:  "renewed-token",
			expiry: newExpiry,
		},
	}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      operatorMetricsMonitorName,
			Namespace: testOperatorNamespace,
		},
	})
	assert.NilError(t, err)

	secret := &corev1.Secret{}
	err = c.Get(context.Background(), types.NamespacedName{
		Name:      operatorMetricsBearerTokenSecretName,
		Namespace: testOperatorNamespace,
	}, secret)
	assert.NilError(t, err)
	assert.Equal(t, string(secret.Data[operatorMetricsBearerTokenKey]), "renewed-token")
	_, hasIssuedAt := secret.Data["issuedAt"]
	assert.Assert(t, !hasIssuedAt)
	_, hasRenewAt := secret.Data["renewAt"]
	assert.Assert(t, !hasRenewAt)
}

func TestOperatorMetricsTokenReconciler_skipsOtherNamespaces(t *testing.T) {
	writeOperatorNamespaceFile(t, testOperatorNamespace)

	s := newOperatorMetricsTokenScheme()
	c := fake.NewClientBuilder().WithScheme(s).Build()
	r := &OperatorMetricsTokenReconciler{
		Client: c,
		Scheme: s,
		TokenRequester: &fakeTokenRequester{
			token:  "test-token",
			expiry: time.Now().Add(operatorMetricsTokenExpiry),
		},
	}

	result, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      operatorMetricsMonitorName,
			Namespace: "other-namespace",
		},
	})
	assert.NilError(t, err)
	assert.Equal(t, result.RequeueAfter, time.Duration(0))

	var secrets corev1.SecretList
	err = c.List(context.Background(), &secrets, client.InNamespace("other-namespace"))
	assert.NilError(t, err)
	assert.Equal(t, len(secrets.Items), 0)
}
