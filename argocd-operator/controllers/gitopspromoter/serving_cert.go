// Copyright 2026 ArgoCD Operator Developers
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gitopspromoter

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	argoproj "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/argocd-operator/controllers/argoutil"
)

const (
	// DefaultAPIServerServingCertSecretName is the Secret used by upstream gitops-promoter's
	// built-in apiserver cert rotation (--apiserver-cert-rotation). The Secret starts empty and
	// is populated by the promoter controller.
	DefaultAPIServerServingCertSecretName = "promoter-apiserver-serving-cert"

	// DefaultAPIServerAPIServiceName is the aggregated APIService for the promoter dashboard.
	DefaultAPIServerAPIServiceName = "v1alpha1.view.promoter.argoproj.io"
)

// usesBuiltInAPIServerCerts reports whether the operator should manage serving certs via the
// promoter controller's built-in cert rotator (no user-provided CertSecretName).
func usesBuiltInAPIServerCerts(cr *argoproj.ArgoCD) bool {
	return getUserAPIServerCertSecretName(cr) == ""
}

// getUserAPIServerCertSecretName returns the user-provided TLS cert Secret name, if any.
func getUserAPIServerCertSecretName(cr *argoproj.ArgoCD) string {
	if cr.Spec.Promoter != nil && cr.Spec.Promoter.APIServer != nil && cr.Spec.Promoter.APIServer.TLS != nil {
		return cr.Spec.Promoter.APIServer.TLS.CertSecretName
	}
	return ""
}

// getAPIServerServingCertSecretName returns the Secret mounted by the apiserver for TLS.
func getAPIServerServingCertSecretName(cr *argoproj.ArgoCD) string {
	if name := getUserAPIServerCertSecretName(cr); name != "" {
		return name
	}
	return DefaultAPIServerServingCertSecretName
}

// isAPIServerEnabled reports whether the promoter API server should be reconciled.
func isAPIServerEnabled(cr *argoproj.ArgoCD) bool {
	return cr.Spec.Promoter == nil || cr.Spec.Promoter.APIServer.IsEnabled()
}

// isBuiltInAPIServerCertRotationEnabled reports whether controller cert-rotation should run.
func isBuiltInAPIServerCertRotationEnabled(cr *argoproj.ArgoCD) bool {
	return cr.Spec.Promoter.IsEnabled() && isAPIServerEnabled(cr) && usesBuiltInAPIServerCerts(cr) &&
		argoutil.IsNamespaceClusterConfigNamespace(cr.Namespace)
}

// ReconcilePromoterAPIServerServingCertSecret reconciles the empty serving-cert Secret used by
// built-in cert rotation. The Secret must exist up front; .data is owned by the promoter
// cert-rotator and must not be cleared by the operator.
func ReconcilePromoterAPIServerServingCertSecret(client client.Client, cr *argoproj.ArgoCD, scheme *runtime.Scheme) (*corev1.Secret, error) {
	secret := buildAPIServerServingCertSecret(cr)
	enabled := isBuiltInAPIServerCertRotationEnabled(cr)

	exists := true
	if err := argoutil.FetchObject(client, secret.Namespace, secret.Name, secret); err != nil {
		if !errors.IsNotFound(err) {
			return nil, err
		}
		exists = false
	}

	if exists {
		// Remove the owner reference when the API Server cert is set to the same name as
		// the default name to prevent the secret from being deleted when the Argo CD CR is.
		if getUserAPIServerCertSecretName(cr) == DefaultAPIServerServingCertSecretName && metav1.IsControlledBy(secret, cr) {
			if err := controllerutil.RemoveControllerReference(cr, secret, scheme); err != nil {
				return nil, fmt.Errorf("failed to remove owner reference from the promoter apiserver serving cert secret %s: %v", secret.Name, err)
			}

			argoutil.LogResourceUpdate(log, secret, "removing owner reference as TLS cert is set to default serving cert name")
			if err := client.Update(context.Background(), secret); err != nil {
				return nil, fmt.Errorf("failed to update api server serving cert secret %s: %v", secret.Name, err)
			}

			return secret, nil
		}

		if !enabled {
			// Only delete secrets this reconciler created. Preserve user-provided or
			// unrelated Secrets with the same name, including when Promoter is disabled
			// or when the user references this Secret via certSecretName.
			if !metav1.IsControlledBy(secret, cr) || getUserAPIServerCertSecretName(cr) == secret.Name {
				return secret, nil
			}
			argoutil.LogResourceDeletion(log, secret, "promoter apiserver serving cert secret is being deleted due to being disabled")
			if err := client.Delete(context.Background(), secret); err != nil {
				return nil, fmt.Errorf("failed to delete promoter apiserver serving cert secret %s: %v", secret.Name, err)
			}
			return secret, nil
		}
		return secret, nil
	}

	if !enabled {
		return secret, nil
	}

	if err := controllerutil.SetControllerReference(cr, secret, scheme); err != nil {
		return nil, fmt.Errorf("failed to set argocd cr %s as owner for secret %s: %v", cr.Name, secret.Name, err)
	}

	argoutil.LogResourceCreation(log, secret)
	if err := client.Create(context.Background(), secret); err != nil {
		return nil, fmt.Errorf("failed to create promoter apiserver serving cert secret %s: %v", secret.Name, err)
	}
	return secret, nil
}

func buildAPIServerServingCertSecret(cr *argoproj.ArgoCD) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultAPIServerServingCertSecretName,
			Namespace: cr.Namespace,
			Labels:    buildLabelsForPromoterResources(string(argoproj.PromoterComponentTypeAPIServer), cr),
		},
		Type: corev1.SecretTypeOpaque,
	}
}
