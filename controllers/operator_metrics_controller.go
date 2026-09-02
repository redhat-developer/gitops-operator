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
	"reflect"
	"strings"
	"time"

	"github.com/go-logr/logr"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// Metrics resources owned by this reconciler. The metrics Service and CA
	// bundle ConfigMap are still shipped in the operator bundle; only the
	// ServiceMonitor and bearer token Secret are managed here so the Secret
	// always exists before Prometheus starts scraping.
	operatorMetricsServiceName           = "openshift-gitops-operator-metrics-service"
	operatorMetricsMonitorName           = "openshift-gitops-operator-metrics-monitor"
	operatorMetricsCABundleConfigMapName = operatorMetricsMonitorName + "-ca-bundle"
	operatorMetricsBearerTokenSecretName = operatorMetricsMonitorName + "-bearer-token"
	operatorControllerSAName             = "openshift-gitops-operator-controller-manager"
	operatorMetricsTokenExpirySecs       = int64(3600)
	operatorMetricsTokenExpiry           = time.Duration(operatorMetricsTokenExpirySecs) * time.Second
	operatorMetricsBearerTokenKey        = "token"
	operatorMetricsBearerTokenExpiryKey  = "expiry"
	operatorMetricsTokenRenewalPercent   = 20
	operatorMetricsControlPlaneLabel     = "control-plane"
	operatorMetricsControlPlaneLabelVal  = "gitops-operator"
	// bearerTokenMinRequeueAfter is the minimum RequeueAfter after minting when
	// the API server grants a lifetime shorter than bearerTokenRenewalLead().
	bearerTokenMinRequeueAfter = time.Minute
)

type serviceAccountTokenRequester interface {
	RequestToken(ctx context.Context, namespace, serviceAccountName string, expirationSeconds int64) (token string, expiry time.Time, err error)
}

type clientServiceAccountTokenRequester struct {
	client client.Client
}

func (r *clientServiceAccountTokenRequester) RequestToken(ctx context.Context, namespace, serviceAccountName string, expirationSeconds int64) (string, time.Time, error) {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceAccountName,
			Namespace: namespace,
		},
	}
	tr := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			ExpirationSeconds: ptr.To(expirationSeconds),
		},
	}
	if err := r.client.SubResource("token").Create(ctx, sa, tr); err != nil {
		return "", time.Time{}, err
	}
	return tr.Status.Token, tr.Status.ExpirationTimestamp.Time, nil
}

// OperatorMetricsTokenReconciler manages the short-lived bearer token Secret and
// the ServiceMonitor that references it for operator Prometheus metrics scraping.
//
// The ServiceMonitor is intentionally created by this controller (not the bundle)
// so the bearer token Secret is minted first. That avoids a window where the
// ServiceMonitor exists but the referenced Secret does not yet.
type OperatorMetricsTokenReconciler struct {
	Client         client.Client
	Scheme         *runtime.Scheme
	TokenRequester serviceAccountTokenRequester
}

var _ reconcile.Reconciler = &OperatorMetricsTokenReconciler{}

func (r *OperatorMetricsTokenReconciler) tokenRequester() serviceAccountTokenRequester {
	if r.TokenRequester != nil {
		return r.TokenRequester
	}
	return &clientServiceAccountTokenRequester{client: r.Client}
}

// SetupWithManager wires watches for the metrics Service (always present from the
// bundle), the bearer token Secret, and the ServiceMonitor. All events reconcile
// the same operator-metrics resources in the operator namespace.
func (r *OperatorMetricsTokenReconciler) SetupWithManager(mgr ctrl.Manager) error {
	metricsServicePredicate := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		return obj.GetName() == operatorMetricsServiceName
	})
	serviceMonitorPredicate := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		return obj.GetName() == operatorMetricsMonitorName
	})
	bearerTokenSecretPredicate := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		return obj.GetName() == operatorMetricsBearerTokenSecretName
	})

	return ctrl.NewControllerManagedBy(mgr).
		Named("operator-metrics-token").
		// The metrics Service is installed with the operator and provides a stable
		// initial reconcile trigger even before the ServiceMonitor exists.
		For(&corev1.Service{}, builder.WithPredicates(metricsServicePredicate)).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(mapOperatorMetricsResourceToReconcile),
			builder.WithPredicates(bearerTokenSecretPredicate),
		).
		// Watch ServiceMonitor updates (for example legacy auth migration) and
		// token renewals that only touch the Secret.
		Watches(
			&monitoringv1.ServiceMonitor{},
			handler.EnqueueRequestsFromMapFunc(mapOperatorMetricsResourceToReconcile),
			builder.WithPredicates(serviceMonitorPredicate),
		).
		Complete(r)
}

// mapOperatorMetricsResourceToReconcile maps Secret and ServiceMonitor events to
// the fixed reconcile key used for operator metrics resources.
func mapOperatorMetricsResourceToReconcile(_ context.Context, obj client.Object) []reconcile.Request {
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Namespace: obj.GetNamespace(),
			Name:      operatorMetricsMonitorName,
		},
	}}
}

//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;create;update;patch

func (r *OperatorMetricsTokenReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	reqLogger := logf.Log.WithName("controller_operator_metrics_token").
		WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)

	operatorNS, err := getOperatorNamespace()
	if err != nil {
		if os.IsNotExist(err) {
			reqLogger.Info(fmt.Sprintf("Unable to retrieve the operator's running namespace via '%s': you should only see this message when running within unit tests, otherwise it is an error.", operatorPodNamespacePath))
			return reconcile.Result{}, nil
		}
		reqLogger.Error(err, "Error retrieving operator's running namespace")
		return reconcile.Result{}, err
	}

	if request.Namespace != operatorNS {
		return reconcile.Result{}, nil
	}
	if request.Name != operatorMetricsMonitorName && request.Name != operatorMetricsServiceName {
		return reconcile.Result{}, nil
	}

	// Ensure the bearer token Secret exists and is current. The
	// ServiceMonitor must not be created until this succeeds.
	requeueAfter, secretReady, err := r.reconcileBearerTokenSecret(ctx, operatorNS, reqLogger)
	if err != nil {
		return reconcile.Result{}, err
	}
	if !secretReady {
		if requeueAfter > 0 {
			reqLogger.Info("Waiting for metrics bearer token Secret before creating ServiceMonitor", "after", requeueAfter.String())
		}
		return reconcile.Result{RequeueAfter: requeueAfter}, nil
	}

	// Create or update the ServiceMonitor now that scrape auth exists.
	if err := r.reconcileServiceMonitor(ctx, operatorNS, reqLogger); err != nil {
		return reconcile.Result{}, err
	}

	if requeueAfter > 0 {
		reqLogger.Info("Scheduling bearer token renewal", "after", requeueAfter.String())
	}
	return reconcile.Result{RequeueAfter: requeueAfter}, nil
}

func getOperatorNamespace() (string, error) {
	data, err := os.ReadFile(operatorPodNamespacePath)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// desiredOperatorMetricsServiceMonitor returns the ServiceMonitor spec written by
// this controller. Keep this aligned with the historical bundle manifest.
func desiredOperatorMetricsServiceMonitor(operatorNS string) *monitoringv1.ServiceMonitor {
	serverName := operatorMetricsServiceName + "." + operatorNS + ".svc"
	return &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorMetricsMonitorName,
			Namespace: operatorNS,
			Labels: map[string]string{
				operatorMetricsControlPlaneLabel: operatorMetricsControlPlaneLabelVal,
			},
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					operatorMetricsControlPlaneLabel: operatorMetricsControlPlaneLabelVal,
				},
			},
			Endpoints: []monitoringv1.Endpoint{
				{
					Authorization: &monitoringv1.SafeAuthorization{
						Type: "Bearer",
						Credentials: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: operatorMetricsBearerTokenSecretName,
							},
							Key: operatorMetricsBearerTokenKey,
						},
					},
					Interval: monitoringv1.Duration("30s"),
					Path:     "/metrics",
					Port:     "metrics",
					Scheme:   "https",
					TLSConfig: &monitoringv1.TLSConfig{
						SafeTLSConfig: monitoringv1.SafeTLSConfig{
							CA: monitoringv1.SecretOrConfigMap{
								ConfigMap: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: operatorMetricsCABundleConfigMapName,
									},
									Key: "service-ca.crt",
								},
							},
							ServerName: &serverName,
						},
					},
				},
			},
		},
	}
}

// reconcileServiceMonitor creates or updates the operator metrics ServiceMonitor.
// It is only called after reconcileBearerTokenSecret reports the bearer token
// Secret is ready, so Prometheus never observes a missing credentials Secret.
func (r *OperatorMetricsTokenReconciler) reconcileServiceMonitor(ctx context.Context, operatorNS string, reqLogger logr.Logger) error {
	desired := desiredOperatorMetricsServiceMonitor(operatorNS)

	serviceMonitor := &monitoringv1.ServiceMonitor{}
	err := r.Client.Get(ctx, types.NamespacedName{
		Name:      operatorMetricsMonitorName,
		Namespace: operatorNS,
	}, serviceMonitor)
	if errors.IsNotFound(err) {
		reqLogger.Info("Creating operator metrics ServiceMonitor",
			"Namespace", operatorNS, "Name", operatorMetricsMonitorName)
		return r.Client.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	updated := false
	if serviceMonitor.Labels == nil {
		serviceMonitor.Labels = map[string]string{}
	}
	if serviceMonitor.Labels[operatorMetricsControlPlaneLabel] != operatorMetricsControlPlaneLabelVal {
		serviceMonitor.Labels[operatorMetricsControlPlaneLabel] = operatorMetricsControlPlaneLabelVal
		updated = true
	}

	if len(serviceMonitor.Spec.Endpoints) == 0 {
		serviceMonitor.Spec.Endpoints = desired.Spec.Endpoints
		updated = true
	} else {
		endpoint := &serviceMonitor.Spec.Endpoints[0]
		desiredEndpoint := desired.Spec.Endpoints[0]

		if endpoint.BearerTokenSecret != nil { //nolint:staticcheck // SA1019: migrate deprecated bearerTokenSecret to authorization
			// Upgrades may still carry the deprecated field from older bundle installs.
			endpoint.BearerTokenSecret = nil //nolint:staticcheck // SA1019: migrate deprecated bearerTokenSecret to authorization
			updated = true
		}
		if !reflect.DeepEqual(endpoint.Authorization, desiredEndpoint.Authorization) {
			endpoint.Authorization = desiredEndpoint.Authorization
			updated = true
		}
		if endpoint.Interval != desiredEndpoint.Interval ||
			endpoint.Path != desiredEndpoint.Path ||
			endpoint.Port != desiredEndpoint.Port ||
			endpoint.Scheme != desiredEndpoint.Scheme {
			endpoint.Interval = desiredEndpoint.Interval
			endpoint.Path = desiredEndpoint.Path
			endpoint.Port = desiredEndpoint.Port
			endpoint.Scheme = desiredEndpoint.Scheme
			updated = true
		}

		if endpoint.TLSConfig == nil {
			endpoint.TLSConfig = &monitoringv1.TLSConfig{}
		}
		if !reflect.DeepEqual(endpoint.TLSConfig.SafeTLSConfig, desiredEndpoint.TLSConfig.SafeTLSConfig) {
			endpoint.TLSConfig.SafeTLSConfig = desiredEndpoint.TLSConfig.SafeTLSConfig
			updated = true
		}

		if !reflect.DeepEqual(endpoint.TLSConfig.CA, desiredEndpoint.TLSConfig.CA) {
			endpoint.TLSConfig.CA = desiredEndpoint.TLSConfig.CA
			updated = true
		}
	}

	if !reflect.DeepEqual(serviceMonitor.Spec.Selector.MatchLabels, desired.Spec.Selector.MatchLabels) {
		serviceMonitor.Spec.Selector = desired.Spec.Selector
		updated = true
	}

	if !updated {
		return nil
	}

	reqLogger.Info("Updating operator metrics ServiceMonitor",
		"Namespace", serviceMonitor.Namespace, "Name", serviceMonitor.Name)
	return r.Client.Update(ctx, serviceMonitor)
}

// isLegacyMetricsBearerTokenSecret reports whether the Secret is a deprecated
// kubernetes.io/service-account-token entry that must be replaced via delete/create.
func isLegacyMetricsBearerTokenSecret(secret *corev1.Secret) bool {
	return secret.Type == corev1.SecretTypeServiceAccountToken
}

// metricsBearerTokenSecretMustReplace reports whether the Secret cannot be
// updated in place. Secret type is immutable, so any non-Opaque Secret (legacy
// or otherwise) must be deleted and recreated.
func metricsBearerTokenSecretMustReplace(existing *corev1.Secret) bool {
	return existing.Type != corev1.SecretTypeOpaque
}

func (r *OperatorMetricsTokenReconciler) applyMetricsBearerTokenSecret(
	ctx context.Context,
	namespace string,
	desired *corev1.Secret,
	existing *corev1.Secret,
	reqLogger logr.Logger,
) error {
	if existing == nil {
		reqLogger.Info("Creating metrics monitor bearer token Secret",
			"Namespace", namespace, "Name", operatorMetricsBearerTokenSecretName)
		return r.Client.Create(ctx, desired)
	}

	if metricsBearerTokenSecretMustReplace(existing) {
		if isLegacyMetricsBearerTokenSecret(existing) {
			reqLogger.Info("Replacing legacy non-expiring service account token Secret",
				"Namespace", namespace, "Name", operatorMetricsBearerTokenSecretName)
		} else {
			reqLogger.Info("Replacing metrics bearer token Secret with incompatible type",
				"Namespace", namespace, "Name", operatorMetricsBearerTokenSecretName, "Type", existing.Type)
		}
		if err := r.Client.Delete(ctx, existing); err != nil && !errors.IsNotFound(err) {
			return err
		}
		return r.Client.Create(ctx, desired)
	}

	if reflect.DeepEqual(existing.Data, desired.Data) {
		return nil
	}
	existing.Data = desired.Data
	reqLogger.Info("Updating metrics monitor bearer token Secret",
		"Namespace", namespace, "Name", operatorMetricsBearerTokenSecretName)
	return r.Client.Update(ctx, existing)
}

// reconcileBearerTokenSecret mints or refreshes the short-lived bearer token
// Secret used for metrics scraping.
//
// The returned ready flag is true when an Opaque Secret with a usable token is
// present. Reconcile must not create the ServiceMonitor until ready is true.
func (r *OperatorMetricsTokenReconciler) reconcileBearerTokenSecret(ctx context.Context, namespace string, reqLogger logr.Logger) (time.Duration, bool, error) {
	existing := &corev1.Secret{}
	err := r.Client.Get(ctx, types.NamespacedName{
		Name:      operatorMetricsBearerTokenSecretName,
		Namespace: namespace,
	}, existing)

	needsRefresh := false
	if errors.IsNotFound(err) {
		existing = nil
		needsRefresh = true
	} else if err != nil {
		return 0, false, err
	} else if metricsBearerTokenSecretMustReplace(existing) {
		// Keep legacy Secrets until TokenRequest succeeds so scrape auth is not
		// interrupted if minting fails. All non-Opaque Secrets are replaced via
		// delete/create because Secret type is immutable in the API.
		needsRefresh = true
	} else if len(existing.Data[operatorMetricsBearerTokenKey]) == 0 {
		needsRefresh = true
	} else {
		expiry, parseErr := parseBearerTokenTimestamp(existing.Data[operatorMetricsBearerTokenExpiryKey])
		if parseErr != nil {
			reqLogger.Error(parseErr, "bearer token secret has unparseable expiry, renewal needed",
				"Namespace", namespace, "Name", operatorMetricsBearerTokenSecretName,
				"expiry", string(existing.Data[operatorMetricsBearerTokenExpiryKey]))
			needsRefresh = true
		} else if requeueAfter, refresh := evaluateBearerTokenRenewal(expiry, time.Now()); refresh {
			needsRefresh = true
		} else {
			return requeueAfter, true, nil
		}
	}

	if !needsRefresh {
		return 0, true, nil
	}

	token, expiry, err := r.tokenRequester().RequestToken(ctx, namespace, operatorControllerSAName, operatorMetricsTokenExpirySecs)
	if err != nil {
		reqLogger.Error(err, "Failed to request service account token")
		return 0, false, err
	}

	desiredSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorMetricsBearerTokenSecretName,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			operatorMetricsBearerTokenKey:       []byte(token),
			operatorMetricsBearerTokenExpiryKey: []byte(expiry.UTC().Format(time.RFC3339)),
		},
	}
	requeueAfter := bearerTokenRequeueAfterMint(expiry, time.Now(), reqLogger)

	if err := r.applyMetricsBearerTokenSecret(ctx, namespace, desiredSecret, existing, reqLogger); err != nil {
		return 0, false, err
	}

	return requeueAfter, true, nil
}

func parseBearerTokenTimestamp(value []byte) (time.Time, error) {
	return time.Parse(time.RFC3339, string(value))
}

// bearerTokenRenewalLead is how long before expiry renewal should happen. It
// matches 20% of the requested token lifetime.
func bearerTokenRenewalLead() time.Duration {
	return operatorMetricsTokenExpiry * operatorMetricsTokenRenewalPercent / 100
}

// bearerTokenRequeueAfterMint returns when to requeue after a successful
// TokenRequest. Normal lifetimes use evaluateBearerTokenRenewal unchanged; when
// the granted lifetime is at or below the renewal lead, requeue on a positive
// minimum interval instead of zero so renewal is scheduled.
func bearerTokenRequeueAfterMint(expiry, now time.Time, reqLogger logr.Logger) time.Duration {
	requeueAfter, refreshAgain := evaluateBearerTokenRenewal(expiry, now)
	if !refreshAgain {
		return requeueAfter
	}
	remaining := expiry.Sub(now)
	if remaining > bearerTokenRenewalLead() {
		return requeueAfter
	}
	reqLogger.Info("Granted token lifetime is shorter than the renewal lead",
		"remaining", remaining.String(),
		"renewalLead", bearerTokenRenewalLead().String(),
		"expiry", expiry.UTC().Format(time.RFC3339))
	requeueAfter = bearerTokenMinRequeueAfter
	if remaining > 0 && requeueAfter >= remaining {
		requeueAfter = remaining - time.Second
	}
	if requeueAfter <= 0 {
		return bearerTokenMinRequeueAfter
	}
	return requeueAfter
}

// evaluateBearerTokenRenewal decides whether to refresh now and, if not, when
// to requeue. Only expiry is stored in the Secret; the renewal boundary is
// expiry minus a fixed lead derived from the requested TTL.
func evaluateBearerTokenRenewal(expiry, now time.Time) (time.Duration, bool) {
	remaining := expiry.Sub(now)
	if remaining <= 0 {
		return 0, true
	}
	lead := bearerTokenRenewalLead()
	if remaining <= lead {
		return 0, true
	}
	return remaining - lead, false
}
