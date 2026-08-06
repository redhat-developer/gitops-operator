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
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	operatorMetricsServiceName             = "openshift-gitops-operator-metrics-service"
	operatorMetricsMonitorName             = "openshift-gitops-operator-metrics-monitor"
	operatorMetricsBearerTokenSecretName   = "openshift-gitops-operator-metrics-monitor-bearer-token"
	operatorControllerSAName             = "openshift-gitops-operator-controller-manager"
	operatorMetricsTokenExpirySecs       = int64(3600)
	operatorMetricsTokenExpiry           = time.Duration(operatorMetricsTokenExpirySecs) * time.Second
	operatorMetricsBearerTokenKey        = "token"
	operatorMetricsBearerTokenExpiryKey  = "expiry"
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

// OperatorMetricsTokenReconciler manages the short-lived bearer token Secret used by the
// operator's ServiceMonitor for Prometheus metrics scraping.
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

// SetupWithManager sets up the controller with the Manager.
func (r *OperatorMetricsTokenReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("operator-metrics-token").
		For(&monitoringv1.ServiceMonitor{}).
		WithEventFilter(predicate.NewPredicateFuncs(func(obj client.Object) bool {
			return obj.GetName() == operatorMetricsMonitorName
		})).
		Complete(r)
}

//+kubebuilder:rbac:groups="",resources=serviceaccounts/token,resourceNames=openshift-gitops-operator-controller-manager,verbs=create
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;update;patch

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

	if request.Namespace != operatorNS || request.Name != operatorMetricsMonitorName {
		return reconcile.Result{}, nil
	}

	serviceMonitor := &monitoringv1.ServiceMonitor{}
	if err := r.Client.Get(ctx, request.NamespacedName, serviceMonitor); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		reqLogger.Error(err, "Error querying for ServiceMonitor")
		return reconcile.Result{}, err
	}

	if err := r.reconcileServiceMonitor(ctx, serviceMonitor, operatorNS, reqLogger); err != nil {
		return reconcile.Result{}, err
	}

	requeueAfter, err := r.reconcileBearerTokenSecret(ctx, operatorNS, reqLogger)
	if err != nil {
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

func (r *OperatorMetricsTokenReconciler) reconcileServiceMonitor(ctx context.Context, serviceMonitor *monitoringv1.ServiceMonitor, operatorNS string, reqLogger logr.Logger) error {
	if len(serviceMonitor.Spec.Endpoints) == 0 {
		return fmt.Errorf("ServiceMonitor %s has no endpoints", serviceMonitor.Name)
	}

	desiredMetricsServerName := operatorMetricsServiceName + "." + operatorNS + ".svc"
	endpoint := &serviceMonitor.Spec.Endpoints[0]

	updated := false
	if endpoint.BearerTokenSecret != nil {
		endpoint.BearerTokenSecret = nil
		endpoint.Authorization = &monitoringv1.SafeAuthorization{
			Type: "Bearer",
			Credentials: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: operatorMetricsBearerTokenSecretName,
				},
				Key: operatorMetricsBearerTokenKey,
			},
		}
		updated = true
	} else if endpoint.Authorization == nil ||
		endpoint.Authorization.Credentials == nil ||
		endpoint.Authorization.Credentials.Name != operatorMetricsBearerTokenSecretName ||
		endpoint.Authorization.Credentials.Key != operatorMetricsBearerTokenKey {
		endpoint.Authorization = &monitoringv1.SafeAuthorization{
			Type: "Bearer",
			Credentials: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: operatorMetricsBearerTokenSecretName,
				},
				Key: operatorMetricsBearerTokenKey,
			},
		}
		updated = true
	}

	if endpoint.TLSConfig != nil && (endpoint.TLSConfig.ServerName == nil || *endpoint.TLSConfig.ServerName != desiredMetricsServerName) {
		endpoint.TLSConfig.ServerName = &desiredMetricsServerName
		updated = true
	}

	if !updated {
		return nil
	}

	reqLogger.Info("Updating operator metrics ServiceMonitor",
		"Namespace", serviceMonitor.Namespace, "Name", serviceMonitor.Name)
	return r.Client.Update(ctx, serviceMonitor)
}

func (r *OperatorMetricsTokenReconciler) reconcileBearerTokenSecret(ctx context.Context, namespace string, reqLogger logr.Logger) (time.Duration, error) {
	secret := &corev1.Secret{}
	err := r.Client.Get(ctx, types.NamespacedName{
		Name:      operatorMetricsBearerTokenSecretName,
		Namespace: namespace,
	}, secret)

	needsRefresh := false
	legacySAToken := false
	if errors.IsNotFound(err) {
		needsRefresh = true
	} else if err != nil {
		return 0, err
	} else if secret.Type == corev1.SecretTypeServiceAccountToken {
		// Keep the legacy Secret until TokenRequest succeeds so scrape auth
		// is not interrupted if minting fails.
		legacySAToken = true
		needsRefresh = true
	} else {
		expiry, parseErr := parseBearerTokenExpiry(secret.Data[operatorMetricsBearerTokenExpiryKey])
		if parseErr != nil || !time.Now().Before(expiry) {
			needsRefresh = true
		} else {
			requeueAfter := bearerTokenRequeueDuration(expiry)
			if requeueAfter <= 0 {
				needsRefresh = true
			} else {
				return requeueAfter, nil
			}
		}
	}

	if !needsRefresh {
		return 0, nil
	}

	token, expiry, err := r.tokenRequester().RequestToken(ctx, namespace, operatorControllerSAName, operatorMetricsTokenExpirySecs)
	if err != nil {
		reqLogger.Error(err, "Failed to request service account token")
		return 0, err
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

	if legacySAToken {
		reqLogger.Info("Replacing legacy non-expiring service account token Secret",
			"Namespace", namespace, "Name", operatorMetricsBearerTokenSecretName)
		if err := r.Client.Delete(ctx, secret); err != nil && !errors.IsNotFound(err) {
			return 0, err
		}
		if err := r.Client.Create(ctx, desiredSecret); err != nil {
			return 0, err
		}
		return bearerTokenRequeueDuration(expiry), nil
	}

	existingSecret := &corev1.Secret{}
	getErr := r.Client.Get(ctx, types.NamespacedName{
		Name:      operatorMetricsBearerTokenSecretName,
		Namespace: namespace,
	}, existingSecret)
	if getErr != nil {
		if errors.IsNotFound(getErr) {
			reqLogger.Info("Creating metrics monitor bearer token Secret",
				"Namespace", namespace, "Name", operatorMetricsBearerTokenSecretName)
			if err := r.Client.Create(ctx, desiredSecret); err != nil {
				return 0, err
			}
			return bearerTokenRequeueDuration(expiry), nil
		}
		return 0, getErr
	}

	existingSecret.Type = corev1.SecretTypeOpaque
	existingSecret.Data = desiredSecret.Data
	reqLogger.Info("Updating metrics monitor bearer token Secret",
		"Namespace", namespace, "Name", operatorMetricsBearerTokenSecretName)
	if err := r.Client.Update(ctx, existingSecret); err != nil {
		return 0, err
	}

	return bearerTokenRequeueDuration(expiry), nil
}

func parseBearerTokenExpiry(expiryData []byte) (time.Time, error) {
	return time.Parse(time.RFC3339, string(expiryData))
}

// bearerTokenRequeueDuration returns how long to wait before renewing the token.
// Renewal is scheduled after one third of the *actual* remaining lifetime has
// elapsed, so cluster-capped TokenRequest lifetimes still get a renewal.
func bearerTokenRequeueDuration(expiry time.Time) time.Duration {
	remaining := time.Until(expiry)
	if remaining <= 0 {
		return 0
	}
	return remaining / 3
}
