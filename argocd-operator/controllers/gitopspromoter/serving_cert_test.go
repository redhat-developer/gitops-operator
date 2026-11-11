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
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tlsProfile "github.com/redhat-developer/gitops-operator/argocd-operator/pkg/tlsprofile"
)

func TestReconcilePromoterAPIServerServingCertSecret_BuiltIn_CreatesEmptySecret(t *testing.T) {
	cr := makeTestArgoCD(withPromoterEnabled(true))
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr})

	secret, err := ReconcilePromoterAPIServerServingCertSecret(cl, cr, sch)
	assert.NoError(t, err)
	assert.NotNil(t, secret)

	retrieved := &corev1.Secret{}
	err = cl.Get(context.Background(), types.NamespacedName{
		Name:      DefaultAPIServerServingCertSecretName,
		Namespace: cr.Namespace,
	}, retrieved)
	assert.NoError(t, err)
	assert.Equal(t, corev1.SecretTypeOpaque, retrieved.Type)
	assert.Empty(t, retrieved.Data)
}

func TestReconcilePromoterAPIServerServingCertSecret_BuiltIn_DoesNotClearData(t *testing.T) {
	cr := makeTestArgoCD(withPromoterEnabled(true))
	existing := buildAPIServerServingCertSecret(cr)
	existing.Data = map[string][]byte{
		"tls.crt": []byte("cert"),
		"tls.key": []byte("key"),
		"ca.crt":  []byte("ca"),
	}

	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr, existing})

	_, err := ReconcilePromoterAPIServerServingCertSecret(cl, cr, sch)
	assert.NoError(t, err)

	retrieved := &corev1.Secret{}
	err = cl.Get(context.Background(), types.NamespacedName{
		Name:      DefaultAPIServerServingCertSecretName,
		Namespace: cr.Namespace,
	}, retrieved)
	assert.NoError(t, err)
	assert.Equal(t, []byte("cert"), retrieved.Data["tls.crt"])
	assert.Equal(t, []byte("key"), retrieved.Data["tls.key"])
	assert.Equal(t, []byte("ca"), retrieved.Data["ca.crt"])
}

func TestReconcilePromoterAPIServerServingCertSecret_BYO_DoesNotCreate(t *testing.T) {
	cr := makeTestArgoCD(withPromoterEnabled(true), withPromoterAPIServerTLS(""))
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr})

	_, err := ReconcilePromoterAPIServerServingCertSecret(cl, cr, sch)
	assert.NoError(t, err)

	retrieved := &corev1.Secret{}
	err = cl.Get(context.Background(), types.NamespacedName{
		Name:      DefaultAPIServerServingCertSecretName,
		Namespace: cr.Namespace,
	}, retrieved)
	assert.True(t, errors.IsNotFound(err))
}

func TestReconcilePromoterAPIServerServingCertSecret_Disabled_DeletesOwnedSecret(t *testing.T) {
	cr := makeTestArgoCD(withPromoterEnabled(false))
	existing := buildAPIServerServingCertSecret(cr)
	sch := makeTestReconcilerScheme()
	require.NoError(t, controllerutil.SetControllerReference(cr, existing, sch))

	cl := makeTestReconcilerClient(sch, []client.Object{cr, existing})

	_, err := ReconcilePromoterAPIServerServingCertSecret(cl, cr, sch)
	assert.NoError(t, err)

	retrieved := &corev1.Secret{}
	err = cl.Get(context.Background(), types.NamespacedName{
		Name:      DefaultAPIServerServingCertSecretName,
		Namespace: cr.Namespace,
	}, retrieved)
	assert.True(t, errors.IsNotFound(err))
}

func TestReconcilePromoterAPIServerServingCertSecret_Disabled_PreservesUnownedSecret(t *testing.T) {
	cr := makeTestArgoCD(withPromoterEnabled(false))
	existing := buildAPIServerServingCertSecret(cr)
	existing.Data = map[string][]byte{"tls.crt": []byte("user-cert")}

	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr, existing})

	_, err := ReconcilePromoterAPIServerServingCertSecret(cl, cr, sch)
	assert.NoError(t, err)

	retrieved := &corev1.Secret{}
	err = cl.Get(context.Background(), types.NamespacedName{
		Name:      DefaultAPIServerServingCertSecretName,
		Namespace: cr.Namespace,
	}, retrieved)
	assert.NoError(t, err)
	assert.Equal(t, []byte("user-cert"), retrieved.Data["tls.crt"])
}

func TestReconcilePromoterAPIServerServingCertSecret_BYO_PreservesReferencedSecret(t *testing.T) {
	// User brings their own cert using the default secret name; must not be deleted.
	cr := makeTestArgoCD(withPromoterEnabled(true), withPromoterAPIServerTLS(""))
	cr.Spec.Promoter.APIServer.TLS.CertSecretName = DefaultAPIServerServingCertSecretName

	existing := buildAPIServerServingCertSecret(cr)
	existing.Data = map[string][]byte{"tls.crt": []byte("byo")}
	sch := makeTestReconcilerScheme()
	require.NoError(t, controllerutil.SetControllerReference(cr, existing, sch))

	cl := makeTestReconcilerClient(sch, []client.Object{cr, existing})

	_, err := ReconcilePromoterAPIServerServingCertSecret(cl, cr, sch)
	assert.NoError(t, err)

	retrieved := &corev1.Secret{}
	err = cl.Get(context.Background(), types.NamespacedName{
		Name:      DefaultAPIServerServingCertSecretName,
		Namespace: cr.Namespace,
	}, retrieved)
	assert.NoError(t, err)
	assert.Equal(t, []byte("byo"), retrieved.Data["tls.crt"])
}

func TestReconcilePromoterControllerDeployment_BYO_SkipsCertRotationArgs(t *testing.T) {
	cr := makeTestArgoCD(withPromoterEnabled(true), withPromoterAPIServerTLS(""))
	sa := makeExistingServiceAccount(cr)
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr})

	deployment, err := ReconcilePromoterControllerDeployment(cl, testCompName, sa, cr, sch, tlsProfile.TLSConfigProfile{})
	assert.NoError(t, err)
	assert.NotNil(t, deployment)

	retrieved := &appsv1.Deployment{}
	err = cl.Get(context.Background(), types.NamespacedName{
		Name:      deployment.Name,
		Namespace: cr.Namespace,
	}, retrieved)
	assert.NoError(t, err)
	assert.NotContains(t, retrieved.Spec.Template.Spec.Containers[0].Args, "--apiserver-cert-rotation")
}

func TestReconcilePromoterAPIServerDeployment_BYO_MountsUserSecret(t *testing.T) {
	cr := makeTestArgoCD(withPromoterEnabled(true), withPromoterAPIServerTLS(""))
	sa := makeExistingServiceAccount(cr)
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, []client.Object{cr})

	deployment, err := ReconcilePromoterAPIServerDeployment(cl, testCompName, sa, cr, sch)
	assert.NoError(t, err)

	retrieved := &appsv1.Deployment{}
	err = cl.Get(context.Background(), types.NamespacedName{
		Name:      deployment.Name,
		Namespace: cr.Namespace,
	}, retrieved)
	assert.NoError(t, err)
	assert.Equal(t, testCertName, retrieved.Spec.Template.Spec.Volumes[0].Secret.SecretName)
	assert.Nil(t, retrieved.Spec.Template.Spec.Volumes[0].Secret.Optional)
	assert.Nil(t, retrieved.Spec.Template.Spec.Containers[0].StartupProbe)
}
