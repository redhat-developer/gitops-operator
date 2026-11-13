// Copyright 2025 ArgoCD Operator Developers
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

package argocdagent

import (
	"context"
	"reflect"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	resourcev1 "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/openshift/api/config/v1"

	argoproj "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/argocd-operator/common"
	"github.com/redhat-developer/gitops-operator/argocd-operator/pkg/tlsprofile"
)

// Helper function to create a test deployment
func makeTestDeployment(cr *argoproj.ArgoCD) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generateAgentResourceName(cr.Name, testCompName),
			Namespace: cr.Namespace,
			Labels:    buildLabelsForAgentPrincipal(cr.Name, testCompName),
		},
		Spec: mustBuildPrincipalSpec(testCompName, generateAgentResourceName(cr.Name, testCompName), cr, tlsprofile.TLSConfigProfile{}),
	}
}

func mustBuildPrincipalSpec(compName, saName string, cr *argoproj.ArgoCD, profile tlsprofile.TLSConfigProfile) appsv1.DeploymentSpec {
	spec, err := buildPrincipalSpec(compName, saName, cr, profile)
	if err != nil {
		panic(err)
	}
	return spec
}

// Helper function to create a test deployment with custom image
func makeTestDeploymentWithCustomImage(cr *argoproj.ArgoCD, customImage string) *appsv1.Deployment {
	deployment := makeTestDeployment(cr)
	deployment.Spec.Template.Spec.Containers[0].Image = customImage
	return deployment
}

// Helper function to create ArgoCD with custom principal image
func withPrincipalImage(image string) argoCDOpt {
	return func(a *argoproj.ArgoCD) {
		if a.Spec.ArgoCDAgent == nil {
			a.Spec.ArgoCDAgent = &argoproj.ArgoCDAgentSpec{}
		}
		if a.Spec.ArgoCDAgent.Principal == nil {
			a.Spec.ArgoCDAgent.Principal = &argoproj.PrincipalSpec{}
		}
		a.Spec.ArgoCDAgent.Principal.Image = image
	}
}

func withResourceRequirements(resources corev1.ResourceRequirements) argoCDOpt {
	return func(a *argoproj.ArgoCD) {
		if a.Spec.ArgoCDAgent == nil {
			a.Spec.ArgoCDAgent = &argoproj.ArgoCDAgentSpec{}
		}
		if a.Spec.ArgoCDAgent.Principal == nil {
			a.Spec.ArgoCDAgent.Principal = &argoproj.PrincipalSpec{}
		}
		a.Spec.ArgoCDAgent.Principal.Resources = &resources
	}
}

// TestReconcilePrincipalDeployment tests

func TestReconcilePrincipalDeployment_DeploymentDoesNotExist_PrincipalDisabled(t *testing.T) {
	// Test case: Deployment doesn't exist and principal is disabled
	// Expected behavior: Should do nothing (no creation, no error)

	cr := makeTestArgoCD(withPrincipalEnabled(false))
	saName := generateAgentResourceName(cr.Name, testCompName)

	resObjs := []client.Object{cr}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcilePrincipalDeployment(cl, testCompName, saName, cr, sch, tlsprofile.TLSConfigProfile{})
	assert.NoError(t, err)

	// Verify Deployment was not created
	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.True(t, errors.IsNotFound(err))
}

func TestReconcilePrincipalDeployment_DeploymentDoesNotExist_PrincipalEnabled(t *testing.T) {
	// Test case: Deployment doesn't exist and principal is enabled
	// Expected behavior: Should create the Deployment with expected spec

	cr := makeTestArgoCD(withPrincipalEnabled(true))
	saName := generateAgentResourceName(cr.Name, testCompName)

	resObjs := []client.Object{cr}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcilePrincipalDeployment(cl, testCompName, saName, cr, sch, tlsprofile.TLSConfigProfile{})
	assert.NoError(t, err)

	// Verify Deployment was created
	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.NoError(t, err)

	// Verify Deployment has expected metadata
	assert.Equal(t, generateAgentResourceName(cr.Name, testCompName), deployment.Name)
	assert.Equal(t, cr.Namespace, deployment.Namespace)
	assert.Equal(t, buildLabelsForAgentPrincipal(cr.Name, testCompName), deployment.Labels)

	// Verify Deployment has expected spec
	expectedSpec := mustBuildPrincipalSpec(testCompName, saName, cr, tlsprofile.TLSConfigProfile{})
	assert.Equal(t, expectedSpec.Selector, deployment.Spec.Selector)
	assert.Equal(t, expectedSpec.Template.Labels, deployment.Spec.Template.Labels)
	assert.Equal(t, expectedSpec.Template.Spec.ServiceAccountName, deployment.Spec.Template.Spec.ServiceAccountName)

	// Verify container configuration
	assert.Len(t, deployment.Spec.Template.Spec.Containers, 1)
	container := deployment.Spec.Template.Spec.Containers[0]
	assert.Equal(t, generateAgentResourceName(cr.Name, testCompName), container.Name)
	assert.Equal(t, buildPrincipalImage(cr), container.Image)
	assert.Equal(t, buildArgs(testCompName), container.Args)
	env, err := buildPrincipalContainerEnv(cr, tlsprofile.TLSConfigProfile{})
	assert.NoError(t, err)
	assert.Equal(t, env, container.Env)
	assert.Equal(t, buildSecurityContext(), container.SecurityContext)
	assert.Equal(t, buildPorts(testCompName, cr), container.Ports)

	// Verify owner reference is set
	assert.Len(t, deployment.OwnerReferences, 1)
	assert.Equal(t, cr.Name, deployment.OwnerReferences[0].Name)
	assert.Equal(t, "ArgoCD", deployment.OwnerReferences[0].Kind)
}

func TestReconcilePrincipalDeployment_DeploymentExists_PrincipalDisabled(t *testing.T) {
	// Test case: Deployment exists and principal is disabled
	// Expected behavior: Should delete the Deployment

	cr := makeTestArgoCD(withPrincipalEnabled(false))
	saName := generateAgentResourceName(cr.Name, testCompName)

	// Create existing Deployment
	existingDeployment := makeTestDeployment(cr)

	resObjs := []client.Object{cr, existingDeployment}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcilePrincipalDeployment(cl, testCompName, saName, cr, sch, tlsprofile.TLSConfigProfile{})
	assert.NoError(t, err)

	// Verify Deployment was deleted
	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.True(t, errors.IsNotFound(err))
}

func TestReconcilePrincipalDeployment_DeploymentExists_PrincipalEnabled_NoChanges(t *testing.T) {
	// Test case: Deployment exists, principal is enabled, and no changes are needed
	// Expected behavior: Should not update the Deployment

	cr := makeTestArgoCD(withPrincipalEnabled(true))
	saName := generateAgentResourceName(cr.Name, testCompName)

	// Create existing Deployment with correct spec
	existingDeployment := makeTestDeployment(cr)

	resObjs := []client.Object{cr, existingDeployment}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcilePrincipalDeployment(cl, testCompName, saName, cr, sch, tlsprofile.TLSConfigProfile{})
	assert.NoError(t, err)

	// Verify Deployment still exists with same spec
	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.NoError(t, err)
	assert.Equal(t, buildPrincipalImage(cr), deployment.Spec.Template.Spec.Containers[0].Image)
	assert.Equal(t, saName, deployment.Spec.Template.Spec.ServiceAccountName)
}

func TestReconcilePrincipalDeployment_DeploymentExists_PrincipalEnabled_ImageChanged(t *testing.T) {
	// Test case: Deployment exists, principal is enabled, but image has changed
	// Expected behavior: Should update the Deployment with new image

	cr := makeTestArgoCD(withPrincipalEnabled(true), withPrincipalImage("quay.io/argoproj/argocd-agent:v2"))
	saName := generateAgentResourceName(cr.Name, testCompName)

	// Create existing Deployment with old image
	existingDeployment := makeTestDeploymentWithCustomImage(cr, "quay.io/argoproj/argocd-agent:v1")

	resObjs := []client.Object{cr, existingDeployment}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcilePrincipalDeployment(cl, testCompName, saName, cr, sch, tlsprofile.TLSConfigProfile{})
	assert.NoError(t, err)

	// Verify Deployment was updated with new image
	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.NoError(t, err)
	assert.Equal(t, "quay.io/argoproj/argocd-agent:v2", deployment.Spec.Template.Spec.Containers[0].Image)
}

func TestReconcilePrincipalDeployment_DeploymentExists_PrincipalEnabled_ServiceAccountChanged(t *testing.T) {
	// Test case: Deployment exists, principal is enabled, but service account has changed
	// Expected behavior: Should update the Deployment with new service account

	cr := makeTestArgoCD(withPrincipalEnabled(true))
	oldSAName := "old-service-account"
	newSAName := "new-service-account"

	// Create existing Deployment with old service account
	existingDeployment := makeTestDeployment(cr)
	existingDeployment.Spec.Template.Spec.ServiceAccountName = oldSAName

	resObjs := []client.Object{cr, existingDeployment}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcilePrincipalDeployment(cl, testCompName, newSAName, cr, sch, tlsprofile.TLSConfigProfile{})
	assert.NoError(t, err)

	// Verify Deployment was updated with new service account
	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.NoError(t, err)
	assert.Equal(t, newSAName, deployment.Spec.Template.Spec.ServiceAccountName)
}

func TestReconcilePrincipalDeployment_DeploymentExists_PrincipalNotSet(t *testing.T) {
	// Test case: Deployment exists but principal is not set (nil)
	// Expected behavior: Should delete the Deployment

	cr := makeTestArgoCD() // No principal configuration
	saName := generateAgentResourceName(cr.Name, testCompName)

	// Create existing Deployment
	existingDeployment := makeTestDeployment(cr)

	resObjs := []client.Object{cr, existingDeployment}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcilePrincipalDeployment(cl, testCompName, saName, cr, sch, tlsprofile.TLSConfigProfile{})
	assert.NoError(t, err)

	// Verify Deployment was deleted
	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.True(t, errors.IsNotFound(err))
}

func TestReconcilePrincipalDeployment_DeploymentDoesNotExist_AgentNotSet(t *testing.T) {
	// Test case: Deployment doesn't exist and ArgoCDAgent is not set (nil)
	// Expected behavior: Should do nothing since principal is effectively disabled

	cr := makeTestArgoCD() // No agent configuration
	saName := generateAgentResourceName(cr.Name, testCompName)

	resObjs := []client.Object{cr}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcilePrincipalDeployment(cl, testCompName, saName, cr, sch, tlsprofile.TLSConfigProfile{})
	assert.NoError(t, err)

	// Verify Deployment was not created
	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.True(t, errors.IsNotFound(err))
}

func TestReconcilePrincipalDeployment_DeploymentExists_AgentNotSet(t *testing.T) {
	// Test case: Deployment exists but ArgoCDAgent is not set (nil)
	// Expected behavior: Should delete the Deployment

	cr := makeTestArgoCD() // No agent configuration
	saName := generateAgentResourceName(cr.Name, testCompName)

	// Create existing Deployment
	existingDeployment := makeTestDeployment(cr)

	resObjs := []client.Object{cr, existingDeployment}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcilePrincipalDeployment(cl, testCompName, saName, cr, sch, tlsprofile.TLSConfigProfile{})
	assert.NoError(t, err)

	// Verify Deployment was deleted
	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.True(t, errors.IsNotFound(err))
}

func TestReconcilePrincipalDeployment_VerifyDeploymentSpec(t *testing.T) {
	// Test case: Verify the deployment spec has correct configuration
	// Expected behavior: Should create deployment with correct security context, ports, etc.

	cr := makeTestArgoCD(withPrincipalEnabled(true))
	saName := generateAgentResourceName(cr.Name, testCompName)

	resObjs := []client.Object{cr}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcilePrincipalDeployment(cl, testCompName, saName, cr, sch, tlsprofile.TLSConfigProfile{})
	assert.NoError(t, err)

	// Verify Deployment was created
	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.NoError(t, err)

	// Verify security context
	container := deployment.Spec.Template.Spec.Containers[0]
	assert.Equal(t, new(false), container.SecurityContext.AllowPrivilegeEscalation)
	assert.Equal(t, new(true), container.SecurityContext.ReadOnlyRootFilesystem)
	assert.Equal(t, new(true), container.SecurityContext.RunAsNonRoot)
	assert.Equal(t, []corev1.Capability{"ALL"}, container.SecurityContext.Capabilities.Drop)
	assert.Equal(t, corev1.SeccompProfileType("RuntimeDefault"), container.SecurityContext.SeccompProfile.Type)

	// Verify ports configuration
	assert.Len(t, container.Ports, 5)
	principalPort := container.Ports[0]
	assert.Equal(t, testCompName, principalPort.Name)
	assert.Equal(t, int32(8443), principalPort.ContainerPort)
	assert.Equal(t, corev1.ProtocolTCP, principalPort.Protocol)
	metricsPort := container.Ports[1]
	assert.Equal(t, "metrics", metricsPort.Name)
	assert.Equal(t, int32(8000), metricsPort.ContainerPort)
	assert.Equal(t, corev1.ProtocolTCP, metricsPort.Protocol)
	redisPort := container.Ports[2]
	assert.Equal(t, "redis", redisPort.Name)
	assert.Equal(t, int32(6379), redisPort.ContainerPort)
	assert.Equal(t, corev1.ProtocolTCP, redisPort.Protocol)
	resourceProxyPort := container.Ports[3]
	assert.Equal(t, "resource-proxy", resourceProxyPort.Name)
	assert.Equal(t, int32(9090), resourceProxyPort.ContainerPort)
	assert.Equal(t, corev1.ProtocolTCP, resourceProxyPort.Protocol)
	healthzPort := container.Ports[4]
	assert.Equal(t, "healthz", healthzPort.Name)
	assert.Equal(t, int32(8003), healthzPort.ContainerPort)
	assert.Equal(t, corev1.ProtocolTCP, healthzPort.Protocol)

	// Verify args
	assert.Equal(t, []string{testCompName}, container.Args)

	// Verify environment variables are set
	assert.True(t, len(container.Env) > 0)
	// Verify some expected environment variables are present
	// The runtime-switchable settings are sourced from the params ConfigMap rather than set to
	// a literal, so that changing one rewrites the ConfigMap without touching the pod spec.
	runtimeSwitchable := map[string]string{
		EnvArgoCDPrincipalLogLevel:     common.ArgoCDAgentPrincipalLogLevelKey,
		EnvArgoCDPrincipalFullDetail:   common.ArgoCDAgentPrincipalFullDetailKey,
		EnvArgoCDPrincipalPProfEnabled: common.ArgoCDAgentPrincipalPProfEnabledKey,
	}
	envNames := make(map[string]bool)
	for _, env := range container.Env {
		envNames[env.Name] = true
		if key, ok := runtimeSwitchable[env.Name]; ok {
			require.NotNil(t, env.ValueFrom, "%s should be sourced from the params ConfigMap", env.Name)
			require.NotNil(t, env.ValueFrom.ConfigMapKeyRef, "%s should use a ConfigMapKeyRef", env.Name)
			assert.Equal(t, common.ArgoCDAgentParamsConfigMapName, env.ValueFrom.ConfigMapKeyRef.Name)
			assert.Equal(t, key, env.ValueFrom.ConfigMapKeyRef.Key)
			require.NotNil(t, env.ValueFrom.ConfigMapKeyRef.Optional)
			assert.True(t, *env.ValueFrom.ConfigMapKeyRef.Optional, "a pod must be able to start before the ConfigMap exists")
			assert.Empty(t, env.Value, "%s must not carry a literal value as well", env.Name)
			continue
		}
		// Every other environment variable should have a direct value, not a reference
		assert.Nil(t, env.ValueFrom, "Environment variable %s should have direct value, not reference", env.Name)
	}
	// Check for some environment variables
	assert.True(t, envNames["ARGOCD_PRINCIPAL_NAMESPACE"], "ARGOCD_PRINCIPAL_NAMESPACE should be set")
	for name := range runtimeSwitchable {
		assert.True(t, envNames[name], "%s should be set", name)
	}
	assert.False(t, envNames["REDIS_PASSWORD"], "REDIS_PASSWORD should not be set")

	// Verify volume mounts
	assert.Len(t, container.VolumeMounts, 3)
	jwtVolumeMount := container.VolumeMounts[0]
	assert.Equal(t, "jwt-secret", jwtVolumeMount.Name)
	assert.Equal(t, "/app/config/jwt", jwtVolumeMount.MountPath)
	userpassVolumeMount := container.VolumeMounts[1]
	assert.Equal(t, "userpass-passwd", userpassVolumeMount.Name)
	assert.Equal(t, "/app/config/userpass", userpassVolumeMount.MountPath)
	redisAuthVolumeMount := container.VolumeMounts[2]
	assert.Equal(t, "redis-initial-pass", redisAuthVolumeMount.Name)
	assert.Equal(t, "/app/config/redis-auth/", redisAuthVolumeMount.MountPath)

	// Verify pod volumes
	assert.Len(t, deployment.Spec.Template.Spec.Volumes, 3)
	jwtVolume := deployment.Spec.Template.Spec.Volumes[0]
	assert.Equal(t, "jwt-secret", jwtVolume.Name)
	assert.NotNil(t, jwtVolume.Secret)
	assert.Equal(t, "argocd-agent-jwt", jwtVolume.Secret.SecretName)
	assert.Equal(t, new(true), jwtVolume.Secret.Optional)
	assert.Len(t, jwtVolume.Secret.Items, 1)
	assert.Equal(t, "jwt.key", jwtVolume.VolumeSource.Secret.Items[0].Key)
	assert.Equal(t, "jwt.key", jwtVolume.VolumeSource.Secret.Items[0].Path)

	userpassVolume := deployment.Spec.Template.Spec.Volumes[1]
	assert.Equal(t, "userpass-passwd", userpassVolume.Name)
	assert.NotNil(t, userpassVolume.Secret)
	assert.Equal(t, "argocd-agent-principal-userpass", userpassVolume.Secret.SecretName)
	assert.Equal(t, new(true), userpassVolume.Secret.Optional)
	assert.Len(t, userpassVolume.Secret.Items, 1)
	assert.Equal(t, "passwd", userpassVolume.VolumeSource.Secret.Items[0].Key)
	assert.Equal(t, "passwd", userpassVolume.VolumeSource.Secret.Items[0].Path)

	redisAuthVolume := deployment.Spec.Template.Spec.Volumes[2]
	assert.Equal(t, "redis-initial-pass", redisAuthVolume.Name)
	assert.NotNil(t, redisAuthVolume.Secret)
	assert.Equal(t, "argocd-redis-initial-password", redisAuthVolume.Secret.SecretName)
	assert.NotEqual(t, new(true), redisAuthVolume.Secret.Optional)
	assert.Len(t, redisAuthVolume.Secret.Items, 2)
}

func TestReconcilePrincipalDeployment_CustomImage(t *testing.T) {
	// Test case: Verify custom image is used when specified
	// Expected behavior: Should create deployment with custom image

	customImage := "custom-registry/argocd-agent:custom-tag"
	cr := makeTestArgoCD(withPrincipalEnabled(true), withPrincipalImage(customImage))
	saName := generateAgentResourceName(cr.Name, testCompName)

	resObjs := []client.Object{cr}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcilePrincipalDeployment(cl, testCompName, saName, cr, sch, tlsprofile.TLSConfigProfile{})
	assert.NoError(t, err)

	// Verify Deployment was created with custom image
	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.NoError(t, err)
	assert.Equal(t, customImage, deployment.Spec.Template.Spec.Containers[0].Image)
}

func TestReconcilePrincipalDeployment_DefaultImage(t *testing.T) {
	// Test case: Verify default image is used when no custom image is specified
	// Expected behavior: Should create deployment with default image

	cr := makeTestArgoCD(withPrincipalEnabled(true))
	saName := generateAgentResourceName(cr.Name, testCompName)

	resObjs := []client.Object{cr}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcilePrincipalDeployment(cl, testCompName, saName, cr, sch, tlsprofile.TLSConfigProfile{})
	assert.NoError(t, err)

	// Verify Deployment was created with default image
	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.NoError(t, err)
	assert.Equal(t, common.ArgoCDAgentPrincipalDefaultImageName, deployment.Spec.Template.Spec.Containers[0].Image)
}

func TestReconcilePrincipalDeployment_VolumeMountsAndVolumes(t *testing.T) {
	// Test case: Verify volume mounts and volumes are correctly configured
	// Expected behavior: Should create deployment with JWT and userpass volume mounts and volumes

	cr := makeTestArgoCD(withPrincipalEnabled(true))
	saName := generateAgentResourceName(cr.Name, testCompName)

	resObjs := []client.Object{cr}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcilePrincipalDeployment(cl, testCompName, saName, cr, sch, tlsprofile.TLSConfigProfile{})
	assert.NoError(t, err)

	// Verify Deployment was created
	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.NoError(t, err)

	// Verify volume mounts
	container := deployment.Spec.Template.Spec.Containers[0]

	// Verify specific volume mount details
	assert.Len(t, container.VolumeMounts, 3)
	jwtMount := container.VolumeMounts[0]
	assert.Equal(t, "jwt-secret", jwtMount.Name)
	assert.Equal(t, "/app/config/jwt", jwtMount.MountPath)

	userpassMount := container.VolumeMounts[1]
	assert.Equal(t, "userpass-passwd", userpassMount.Name)
	assert.Equal(t, "/app/config/userpass", userpassMount.MountPath)

	redisAuthMount := container.VolumeMounts[2]
	assert.Equal(t, "redis-initial-pass", redisAuthMount.Name)

	// Verify specific volume details
	assert.Len(t, deployment.Spec.Template.Spec.Volumes, 3)
	jwtVolume := deployment.Spec.Template.Spec.Volumes[0]
	assert.Equal(t, "jwt-secret", jwtVolume.Name)
	assert.Equal(t, "argocd-agent-jwt", jwtVolume.Secret.SecretName)
	assert.Equal(t, new(true), jwtVolume.Secret.Optional)

	userpassVolume := deployment.Spec.Template.Spec.Volumes[1]
	assert.Equal(t, "userpass-passwd", userpassVolume.Name)
	assert.Equal(t, "argocd-agent-principal-userpass", userpassVolume.Secret.SecretName)
	assert.Equal(t, new(true), userpassVolume.Secret.Optional)

	redisAuthVolume := deployment.Spec.Template.Spec.Volumes[2]
	assert.Equal(t, "redis-initial-pass", redisAuthVolume.Name)
}

func TestBuildPrincipalImage(t *testing.T) {
	tests := []struct {
		name          string
		cr            *argoproj.ArgoCD
		envImage      string
		expectedImage string
		description   string
	}{
		{
			name: "CR specification takes precedence",
			cr: makeTestArgoCD(
				withPrincipalEnabled(true),
				withPrincipalImage("custom-registry/argocd-agent:custom-tag"),
			),
			envImage:      "env-registry/argocd-agent:env-tag",
			expectedImage: "custom-registry/argocd-agent:custom-tag",
			description:   "When CR specifies an image, it should take precedence over environment variable and default",
		},
		{
			name: "Environment variable used when CR image not specified",
			cr: makeTestArgoCD(
				withPrincipalEnabled(true),
			),
			envImage:      "env-registry/argocd-agent:env-tag",
			expectedImage: "env-registry/argocd-agent:env-tag",
			description:   "When CR doesn't specify an image but environment variable is set, use environment variable",
		},
		{
			name: "Default image used when neither CR nor environment specified",
			cr: makeTestArgoCD(
				withPrincipalEnabled(true),
			),
			envImage:      "",
			expectedImage: common.ArgoCDAgentPrincipalDefaultImageName,
			description:   "When neither CR nor environment variable specifies an image, use default",
		},
		{
			name: "Empty CR image should not override environment variable",
			cr: makeTestArgoCD(
				withPrincipalEnabled(true),
				withPrincipalImage(""),
			),
			envImage:      "env-registry/argocd-agent:env-tag",
			expectedImage: "env-registry/argocd-agent:env-tag",
			description:   "When CR specifies empty image, environment variable should be used",
		},
		{
			name: "Default image used when CR image is empty and no environment variable",
			cr: makeTestArgoCD(
				withPrincipalEnabled(true),
				withPrincipalImage(""),
			),
			envImage:      "",
			expectedImage: common.ArgoCDAgentPrincipalDefaultImageName,
			description:   "When CR specifies empty image and no environment variable, use default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable if specified
			if tt.envImage != "" {
				t.Setenv("ARGOCD_PRINCIPAL_IMAGE", tt.envImage)
			} else {
				// Clear environment variable
				t.Setenv("ARGOCD_PRINCIPAL_IMAGE", "")
			}

			result := buildPrincipalImage(tt.cr)
			assert.Equal(t, tt.expectedImage, result, tt.description)
		})
	}
}

func TestReconcilePrincipalDeployment_ResourceRequirements(t *testing.T) {
	cr := makeTestArgoCD(withPrincipalEnabled(true), withResourceRequirements(corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resourcev1.MustParse("500m"),
			corev1.ResourceMemory: resourcev1.MustParse("512Mi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resourcev1.MustParse("250m"),
			corev1.ResourceMemory: resourcev1.MustParse("256Mi"),
		},
	}))
	saName := generateAgentResourceName(cr.Name, testCompName)

	resObjs := []client.Object{cr}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcilePrincipalDeployment(cl, testCompName, saName, cr, sch, tlsprofile.TLSConfigProfile{})
	assert.NoError(t, err)

	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.NoError(t, err)

	testResources := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resourcev1.MustParse("500m"),
			corev1.ResourceMemory: resourcev1.MustParse("512Mi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resourcev1.MustParse("250m"),
			corev1.ResourceMemory: resourcev1.MustParse("256Mi"),
		},
	}
	assert.Equal(t, testResources, deployment.Spec.Template.Spec.Containers[0].Resources)

	// Ensure that the requirements can be updated
	updatedResources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resourcev1.MustParse("250m"),
			corev1.ResourceMemory: resourcev1.MustParse("256Mi"),
		},
	}
	cr.Spec.ArgoCDAgent.Principal.Resources = &updatedResources

	err = ReconcilePrincipalDeployment(cl, testCompName, saName, cr, sch, tlsprofile.TLSConfigProfile{})
	assert.NoError(t, err)

	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.NoError(t, err)

	assert.Equal(t, updatedResources, deployment.Spec.Template.Spec.Containers[0].Resources)
}

func TestGetPrincipalTlsConfig(t *testing.T) {

	tests := []struct {
		name       string
		tlsProfile tlsprofile.TLSConfigProfile
		expected   map[string]string
		wantErr    bool
	}{
		{
			name: "disableClusterTLSProfile for argocd agent",
			tlsProfile: tlsprofile.TLSConfigProfile{
				DisableClusterTLSProfile: true,
			},
			expected: nil,
			wantErr:  false,
		},
		{
			name: "central tls profile with tls 1.2",
			tlsProfile: tlsprofile.TLSConfigProfile{
				MinVersion: configv1.VersionTLS12,
				Ciphers: []string{
					"ECDHE-RSA-AES128-GCM-SHA256",
					"TLS_AES_128_GCM_SHA256",
				},
			},
			expected: map[string]string{
				"--tlsminversion": "tls1.2",
				"--tlsciphers":    "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
			},
			wantErr: false,
		},
		{
			name: "central tls profile with tls 1.3",
			tlsProfile: tlsprofile.TLSConfigProfile{
				MinVersion: configv1.VersionTLS13,
				Ciphers: []string{
					"TLS_AES_128_GCM_SHA256",
					"ECDHE-RSA-AES128-GCM-SHA256",
				},
			},
			expected: map[string]string{
				"--tlsminversion": "tls1.3",
				"--tlsciphers":    "TLS_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
			},
			wantErr: false,
		},
		{
			name: "central tls with only tls13 ciphers filtered",
			tlsProfile: tlsprofile.TLSConfigProfile{
				MinVersion: configv1.VersionTLS12,
				Ciphers: []string{
					"TLS_AES_128_GCM_SHA256",
					"TLS_AES_256_GCM_SHA384",
				},
			},
			expected: map[string]string{
				"--tlsminversion": "tls1.2",
			},
			wantErr: false,
		},
		{
			name: "central tls with mapped non tls13 ciphers",
			tlsProfile: tlsprofile.TLSConfigProfile{
				MinVersion: configv1.VersionTLS12,
				Ciphers: []string{
					"ECDHE-RSA-AES256-GCM-SHA384",
				},
			},
			expected: map[string]string{
				"--tlsminversion": "tls1.2",
				"--tlsciphers":    "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
			},
			wantErr: false,
		},
		{
			name: "central tls with empty cipher mapping",
			tlsProfile: tlsprofile.TLSConfigProfile{
				MinVersion: configv1.VersionTLS12,
				Ciphers: []string{
					"INVALID",
				},
			},
			expected: map[string]string{
				"--tlsminversion": "tls1.2",
			},
			wantErr: false,
		},
		{
			name: "curve preferences mapped to comma-separated Go curve names",
			tlsProfile: tlsprofile.TLSConfigProfile{
				MinVersion: configv1.VersionTLS12,
				CurvePreferences: []configv1.TLSGroup{
					configv1.TLSGroupX25519,
					configv1.TLSGroupSecP256r1,
					configv1.TLSGroupSecP384r1,
				},
			},
			expected: map[string]string{
				"--tlsminversion":       "tls1.2",
				"--tlsCurvePreferences": "X25519,CurveP256,CurveP384",
			},
			wantErr: false,
		},
		{
			name: "unsupported curve groups skipped when others remain",
			tlsProfile: tlsprofile.TLSConfigProfile{
				CurvePreferences: []configv1.TLSGroup{
					configv1.TLSGroupSecP256r1MLKEM768,
					configv1.TLSGroupX25519,
				},
			},
			expected: map[string]string{
				"--tlsCurvePreferences": "X25519",
			},
			wantErr: false,
		},
		{
			name: "all unsupported curve groups returns error",
			tlsProfile: tlsprofile.TLSConfigProfile{
				CurvePreferences: []configv1.TLSGroup{
					configv1.TLSGroupSecP256r1MLKEM768,
				},
			},
			expected: nil,
			wantErr:  true,
		},
		{
			name: "empty curve preferences omit env value",
			tlsProfile: tlsprofile.TLSConfigProfile{
				MinVersion:       configv1.VersionTLS12,
				CurvePreferences: nil,
			},
			expected: map[string]string{
				"--tlsminversion": "tls1.2",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getPrincipalTlsConfig(tt.tlsProfile)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Fatalf("expected %#v got %#v", tt.expected, got)
			}
		})
	}
}

func TestBuildPrincipalContainerEnv_TLSCurvePreferences(t *testing.T) {
	cr := makeTestArgoCD(withPrincipalEnabled(true))
	envVars, err := buildPrincipalContainerEnv(cr, tlsprofile.TLSConfigProfile{
		CurvePreferences: []configv1.TLSGroup{
			configv1.TLSGroupX25519,
			configv1.TLSGroupSecP256r1,
		},
	})
	assert.NoError(t, err)

	envMap := make(map[string]string)
	for _, e := range envVars {
		envMap[e.Name] = e.Value
	}
	assert.Equal(t, "X25519,CurveP256", envMap[EnvArgoCDPrincipalTLSCurvePreferences])

	_, err = buildPrincipalContainerEnv(cr, tlsprofile.TLSConfigProfile{
		CurvePreferences: []configv1.TLSGroup{
			configv1.TLSGroupSecP256r1MLKEM768,
		},
	})
	assert.Error(t, err)
}

func withPrincipalLabelSelector(selector string) argoCDOpt {
	return func(a *argoproj.ArgoCD) {
		if a.Spec.ArgoCDAgent == nil {
			a.Spec.ArgoCDAgent = &argoproj.ArgoCDAgentSpec{}
		}
		if a.Spec.ArgoCDAgent.Principal == nil {
			a.Spec.ArgoCDAgent.Principal = &argoproj.PrincipalSpec{}
		}
		a.Spec.ArgoCDAgent.Principal.LabelSelector = selector
	}
}

func TestGetPrincipalLabelSelector(t *testing.T) {
	tests := []struct {
		name     string
		cr       *argoproj.ArgoCD
		expected string
	}{
		{
			name:     "principal not configured",
			cr:       makeTestArgoCD(),
			expected: "",
		},
		{
			name:     "principal enabled without label selector",
			cr:       makeTestArgoCD(withPrincipalEnabled(true)),
			expected: "",
		},
		{
			name:     "label selector set",
			cr:       makeTestArgoCD(withPrincipalEnabled(true), withPrincipalLabelSelector("argocd-agent=true")),
			expected: "argocd-agent=true",
		},
		{
			name:     "empty label selector",
			cr:       makeTestArgoCD(withPrincipalEnabled(true), withPrincipalLabelSelector("")),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, getPrincipalLabelSelector(tt.cr))
		})
	}
}

func TestBuildPrincipalContainerEnv_LabelSelector(t *testing.T) {
	tests := []struct {
		name     string
		cr       *argoproj.ArgoCD
		expected string
	}{
		{
			name:     "default empty label selector",
			cr:       makeTestArgoCD(withPrincipalEnabled(true)),
			expected: "",
		},
		{
			name:     "custom label selector",
			cr:       makeTestArgoCD(withPrincipalEnabled(true), withPrincipalLabelSelector("argocd-agent=true")),
			expected: "argocd-agent=true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envVars, err := buildPrincipalContainerEnv(tt.cr, tlsprofile.TLSConfigProfile{})
			assert.NoError(t, err)
			envMap := make(map[string]string)
			for _, e := range envVars {
				envMap[e.Name] = e.Value
			}
			assert.Equal(t, tt.expected, envMap[EnvArgoCDPrincipalLabelSelector],
				"ARGOCD_PRINCIPAL_LABEL_SELECTOR mismatch")
		})
	}
}

func withSelfRegistration(enabled bool, clientCertSecretName string) argoCDOpt {
	return func(a *argoproj.ArgoCD) {
		if a.Spec.ArgoCDAgent == nil {
			a.Spec.ArgoCDAgent = &argoproj.ArgoCDAgentSpec{}
		}
		if a.Spec.ArgoCDAgent.Principal == nil {
			a.Spec.ArgoCDAgent.Principal = &argoproj.PrincipalSpec{}
		}
		a.Spec.ArgoCDAgent.Principal.SelfRegistration = &argoproj.PrincipalSelfRegistrationSpec{
			Enabled:              &enabled,
			ClientCertSecretName: clientCertSecretName,
		}
	}
}

func withSelfRegistrationClusterSecretLabels(labels map[string]string) argoCDOpt {
	return func(a *argoproj.ArgoCD) {
		if a.Spec.ArgoCDAgent == nil {
			a.Spec.ArgoCDAgent = &argoproj.ArgoCDAgentSpec{}
		}
		if a.Spec.ArgoCDAgent.Principal == nil {
			a.Spec.ArgoCDAgent.Principal = &argoproj.PrincipalSpec{}
		}
		if a.Spec.ArgoCDAgent.Principal.SelfRegistration == nil {
			a.Spec.ArgoCDAgent.Principal.SelfRegistration = &argoproj.PrincipalSelfRegistrationSpec{}
		}
		a.Spec.ArgoCDAgent.Principal.SelfRegistration.ClusterSecretLabels = labels
	}
}

func TestGetPrincipalEnableSelfClusterRegistration(t *testing.T) {
	tests := []struct {
		name     string
		cr       *argoproj.ArgoCD
		expected string
	}{
		{
			name:     "principal not configured",
			cr:       makeTestArgoCD(),
			expected: "false",
		},
		{
			name:     "self-registration not configured",
			cr:       makeTestArgoCD(withPrincipalEnabled(true)),
			expected: "false",
		},
		{
			name:     "self-registration enabled",
			cr:       makeTestArgoCD(withPrincipalEnabled(true), withSelfRegistration(true, "")),
			expected: "true",
		},
		{
			name:     "self-registration disabled",
			cr:       makeTestArgoCD(withPrincipalEnabled(true), withSelfRegistration(false, "")),
			expected: "false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, getPrincipalEnableSelfClusterRegistration(tt.cr))
		})
	}
}

func TestGetPrincipalSelfRegistrationClientCertSecret(t *testing.T) {
	tests := []struct {
		name     string
		cr       *argoproj.ArgoCD
		expected string
	}{
		{
			name:     "principal not configured",
			cr:       makeTestArgoCD(),
			expected: "",
		},
		{
			name:     "self-registration not configured",
			cr:       makeTestArgoCD(withPrincipalEnabled(true)),
			expected: "",
		},
		{
			name:     "client cert secret set",
			cr:       makeTestArgoCD(withPrincipalEnabled(true), withSelfRegistration(true, "argocd-agent-shared-client-cert")),
			expected: "argocd-agent-shared-client-cert",
		},
		{
			name:     "client cert secret empty",
			cr:       makeTestArgoCD(withPrincipalEnabled(true), withSelfRegistration(true, "")),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, getPrincipalSelfRegistrationClientCertSecret(tt.cr))
		})
	}
}

func TestBuildPrincipalContainerEnv_SelfRegistration(t *testing.T) {
	tests := []struct {
		name                      string
		cr                        *argoproj.ArgoCD
		expectedEnabled           string
		expectedClientCertSecret  string
		expectedSecretLabels      string
		expectedResourceProxyAddr string
	}{
		{
			name:                      "defaults when self-registration not configured",
			cr:                        makeTestArgoCD(withPrincipalEnabled(true)),
			expectedEnabled:           "false",
			expectedClientCertSecret:  "",
			expectedSecretLabels:      "",
			expectedResourceProxyAddr: generateAgentResourceName(testArgoCDName, testCompName+"-resource-proxy") + ":9090",
		},
		{
			name:                      "self-registration enabled with client cert",
			cr:                        makeTestArgoCD(withPrincipalEnabled(true), withSelfRegistration(true, "argocd-agent-shared-client-cert")),
			expectedEnabled:           "true",
			expectedClientCertSecret:  "argocd-agent-shared-client-cert",
			expectedSecretLabels:      "",
			expectedResourceProxyAddr: generateAgentResourceName(testArgoCDName, testCompName+"-resource-proxy") + ":9090",
		},
		{
			name: "self-registration enabled with cluster secret labels",
			cr: makeTestArgoCD(withPrincipalEnabled(true),
				withSelfRegistration(true, "argocd-agent-shared-client-cert"),
				withSelfRegistrationClusterSecretLabels(map[string]string{"foo": "bar"})),
			expectedEnabled:           "true",
			expectedClientCertSecret:  "argocd-agent-shared-client-cert",
			expectedSecretLabels:      "foo=bar",
			expectedResourceProxyAddr: generateAgentResourceName(testArgoCDName, testCompName+"-resource-proxy") + ":9090",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envVars, err := buildPrincipalContainerEnv(tt.cr, tlsprofile.TLSConfigProfile{})
			assert.NoError(t, err)
			envMap := make(map[string]string)
			for _, e := range envVars {
				envMap[e.Name] = e.Value
			}
			assert.Equal(t, tt.expectedEnabled, envMap[EnvArgoCDPrincipalEnableSelfClusterRegistration],
				"ARGOCD_PRINCIPAL_ENABLE_SELF_CLUSTER_REGISTRATION mismatch")
			assert.Equal(t, tt.expectedClientCertSecret, envMap[EnvArgoCDPrincipalSelfRegistrationClientCertSecret],
				"ARGOCD_PRINCIPAL_SELF_REGISTRATION_CLIENT_CERT_SECRET mismatch")
			assert.Equal(t, tt.expectedSecretLabels, envMap[EnvArgoCDPrincipalSelfRegistrationSecretLabels],
				"ARGOCD_PRINCIPAL_SELF_REGISTRATION_SECRET_LABELS mismatch")
			assert.Equal(t, tt.expectedResourceProxyAddr, envMap[EnvArgoCDPrincipalResourceProxyAddress],
				"ARGOCD_PRINCIPAL_RESOURCE_PROXY_ADDRESS mismatch")
		})
	}
}

func TestGetPrincipalSelfRegistrationSecretLabels(t *testing.T) {
	tests := []struct {
		name     string
		cr       *argoproj.ArgoCD
		expected string
	}{
		{
			name:     "no self-registration configured",
			cr:       makeTestArgoCD(withPrincipalEnabled(true)),
			expected: "",
		},
		{
			name:     "self-registration set but no cluster secret labels",
			cr:       makeTestArgoCD(withPrincipalEnabled(true), withSelfRegistration(true, "")),
			expected: "",
		},
		{
			name: "single label",
			cr: makeTestArgoCD(withPrincipalEnabled(true),
				withSelfRegistrationClusterSecretLabels(map[string]string{"foo": "bar"})),
			expected: "foo=bar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, getPrincipalSelfRegistrationSecretLabels(tt.cr))
		})
	}
}

func TestGetPrincipalSelfRegistrationSecretLabels_MultipleLabels(t *testing.T) {
	cr := makeTestArgoCD(withPrincipalEnabled(true),
		withSelfRegistrationClusterSecretLabels(map[string]string{
			"env":  "production",
			"app":  "argocd",
			"team": "platform",
		}))
	result := getPrincipalSelfRegistrationSecretLabels(cr)
	// Map iteration order is non-deterministic; verify each pair is present individually.
	assert.Contains(t, result, "env=production")
	assert.Contains(t, result, "app=argocd")
	assert.Contains(t, result, "team=platform")
}

func withPrincipalSPIFFE(authMethod argoproj.SpiffeAuthMethod, csiDriverName string) argoCDOpt {
	return func(a *argoproj.ArgoCD) {
		if a.Spec.ArgoCDAgent == nil {
			a.Spec.ArgoCDAgent = &argoproj.ArgoCDAgentSpec{}
		}
		if a.Spec.ArgoCDAgent.Principal == nil {
			a.Spec.ArgoCDAgent.Principal = &argoproj.PrincipalSpec{}
		}
		a.Spec.ArgoCDAgent.Principal.SPIFFE = &argoproj.SPIFFESpec{
			Enabled:       true,
			AuthMethod:    authMethod,
			CSIDriverName: csiDriverName,
		}
	}
}

func TestHasPrincipalSPIFFE(t *testing.T) {
	tests := []struct {
		name     string
		cr       *argoproj.ArgoCD
		expected bool
	}{
		{
			name:     "principal not configured",
			cr:       makeTestArgoCD(),
			expected: false,
		},
		{
			name:     "principal enabled without SPIFFE",
			cr:       makeTestArgoCD(withPrincipalEnabled(true)),
			expected: false,
		},
		{
			name: "SPIFFE block present but enabled=false",
			cr: makeTestArgoCD(withPrincipalEnabled(true), func(a *argoproj.ArgoCD) {
				a.Spec.ArgoCDAgent.Principal.SPIFFE = &argoproj.SPIFFESpec{Enabled: false}
			}),
			expected: false,
		},
		{
			name:     "SPIFFE configured with enabled=true",
			cr:       makeTestArgoCD(withPrincipalEnabled(true), withPrincipalSPIFFE("", "")),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, hasPrincipalSPIFFE(tt.cr))
		})
	}
}

func TestGetPrincipalSpireAuthMethod(t *testing.T) {
	tests := []struct {
		name     string
		cr       *argoproj.ArgoCD
		expected string
	}{
		{
			name:     "SPIFFE not configured defaults to jwt",
			cr:       makeTestArgoCD(withPrincipalEnabled(true)),
			expected: string(argoproj.SpiffeAuthMethodJWT),
		},
		{
			name:     "SPIFFE configured without authMethod defaults to jwt",
			cr:       makeTestArgoCD(withPrincipalEnabled(true), withPrincipalSPIFFE("", "")),
			expected: string(argoproj.SpiffeAuthMethodJWT),
		},
		{
			name:     "SPIFFE configured with mtls authMethod",
			cr:       makeTestArgoCD(withPrincipalEnabled(true), withPrincipalSPIFFE(argoproj.SpiffeAuthMethodMTLS, "")),
			expected: string(argoproj.SpiffeAuthMethodMTLS),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, getPrincipalSpireAuthMethod(tt.cr))
		})
	}
}

func TestGetPrincipalSpiffeCSIDriver(t *testing.T) {
	tests := []struct {
		name     string
		cr       *argoproj.ArgoCD
		expected string
	}{
		{
			name:     "SPIFFE not configured defaults to csi.spiffe.io",
			cr:       makeTestArgoCD(withPrincipalEnabled(true)),
			expected: "csi.spiffe.io",
		},
		{
			name:     "SPIFFE configured without csiDriverName defaults to csi.spiffe.io",
			cr:       makeTestArgoCD(withPrincipalEnabled(true), withPrincipalSPIFFE(argoproj.SpiffeAuthMethodJWT, "")),
			expected: "csi.spiffe.io",
		},
		{
			name:     "SPIFFE configured with custom csiDriverName",
			cr:       makeTestArgoCD(withPrincipalEnabled(true), withPrincipalSPIFFE(argoproj.SpiffeAuthMethodJWT, "custom.csi.driver.io")),
			expected: "custom.csi.driver.io",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, getPrincipalSpiffeCSIDriver(tt.cr))
		})
	}
}

func TestGetEffectivePrincipalAuth(t *testing.T) {
	tests := []struct {
		name     string
		cr       *argoproj.ArgoCD
		expected string
	}{
		{
			name:     "principal not configured",
			cr:       makeTestArgoCD(),
			expected: "",
		},
		{
			name: "explicit auth takes precedence over SPIFFE",
			cr: makeTestArgoCD(withPrincipalEnabled(true), withPrincipalSPIFFE(argoproj.SpiffeAuthMethodJWT, ""), func(a *argoproj.ArgoCD) {
				a.Spec.ArgoCDAgent.Principal.Auth = "mtls:CN=([^,]+)"
			}),
			expected: "mtls:CN=([^,]+)",
		},
		{
			name:     "SPIFFE configured without explicit auth infers jwt pattern",
			cr:       makeTestArgoCD(withPrincipalEnabled(true), withPrincipalSPIFFE(argoproj.SpiffeAuthMethodJWT, "")),
			expected: defaultPrincipalSPIFFEJWTAuth,
		},
		{
			name:     "SPIFFE configured with default (empty) authMethod infers jwt pattern",
			cr:       makeTestArgoCD(withPrincipalEnabled(true), withPrincipalSPIFFE("", "")),
			expected: defaultPrincipalSPIFFEJWTAuth,
		},
		{
			name:     "SPIFFE configured with mtls authMethod infers mtls:uri pattern",
			cr:       makeTestArgoCD(withPrincipalEnabled(true), withPrincipalSPIFFE(argoproj.SpiffeAuthMethodMTLS, "")),
			expected: defaultPrincipalSPIFFEMTLSAuth,
		},
		{
			name:     "neither auth nor SPIFFE configured",
			cr:       makeTestArgoCD(withPrincipalEnabled(true)),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, getEffectivePrincipalAuth(tt.cr))
		})
	}
}

func TestGetPrincipalAuth(t *testing.T) {
	tests := []struct {
		name     string
		cr       *argoproj.ArgoCD
		expected string
	}{
		{
			name:     "principal not configured falls back to default mtls pattern",
			cr:       makeTestArgoCD(),
			expected: "mtls:CN=([^,]+)",
		},
		{
			name: "explicit auth is used as-is",
			cr: makeTestArgoCD(withPrincipalEnabled(true), func(a *argoproj.ArgoCD) {
				a.Spec.ArgoCDAgent.Principal.Auth = "mtls:CN=([^,]+)"
			}),
			expected: "mtls:CN=([^,]+)",
		},
		{
			name:     "SPIFFE jwt auth method infers spiffe-jwt pattern",
			cr:       makeTestArgoCD(withPrincipalEnabled(true), withPrincipalSPIFFE(argoproj.SpiffeAuthMethodJWT, "")),
			expected: defaultPrincipalSPIFFEJWTAuth,
		},
		{
			name:     "SPIFFE mtls auth method infers mtls:uri spiffe pattern",
			cr:       makeTestArgoCD(withPrincipalEnabled(true), withPrincipalSPIFFE(argoproj.SpiffeAuthMethodMTLS, "")),
			expected: defaultPrincipalSPIFFEMTLSAuth,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, getPrincipalAuth(tt.cr))
		})
	}
}

func TestBuildSpiffeVolumeMount_Principal(t *testing.T) {
	mount := buildSpiffeVolumeMount()
	assert.Equal(t, "spiffe-workload-api", mount.Name)
	assert.Equal(t, "/run/spire/sockets", mount.MountPath)
	assert.True(t, mount.ReadOnly)
}

func TestBuildSpiffeVolume_Principal(t *testing.T) {
	volume := buildSpiffeVolume("custom.csi.driver.io")
	assert.Equal(t, "spiffe-workload-api", volume.Name)
	assert.NotNil(t, volume.CSI)
	assert.Equal(t, "custom.csi.driver.io", volume.CSI.Driver)
	assert.NotNil(t, volume.CSI.ReadOnly)
	assert.True(t, *volume.CSI.ReadOnly)
}

func TestBuildPrincipalContainerEnv_SPIFFE(t *testing.T) {
	tests := []struct {
		name               string
		cr                 *argoproj.ArgoCD
		expectSocketSet    bool
		expectedSocket     string
		expectedAuthMethod string
	}{
		{
			name:            "SPIFFE not configured, env vars absent",
			cr:              makeTestArgoCD(withPrincipalEnabled(true)),
			expectSocketSet: false,
		},
		{
			name:               "SPIFFE configured with jwt auth method",
			cr:                 makeTestArgoCD(withPrincipalEnabled(true), withPrincipalSPIFFE(argoproj.SpiffeAuthMethodJWT, "")),
			expectSocketSet:    true,
			expectedSocket:     "unix:///run/spire/sockets/spire-agent.sock",
			expectedAuthMethod: string(argoproj.SpiffeAuthMethodJWT),
		},
		{
			name:               "SPIFFE configured with mtls auth method",
			cr:                 makeTestArgoCD(withPrincipalEnabled(true), withPrincipalSPIFFE(argoproj.SpiffeAuthMethodMTLS, "")),
			expectSocketSet:    true,
			expectedSocket:     "unix:///run/spire/sockets/spire-agent.sock",
			expectedAuthMethod: string(argoproj.SpiffeAuthMethodMTLS),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envVars, err := buildPrincipalContainerEnv(tt.cr, tlsprofile.TLSConfigProfile{})
			assert.NoError(t, err)
			envMap := make(map[string]string)
			envNames := make(map[string]bool)
			for _, e := range envVars {
				envMap[e.Name] = e.Value
				envNames[e.Name] = true
			}

			if !tt.expectSocketSet {
				assert.False(t, envNames[EnvArgoCDPrincipalSpireAgentSocket], "ARGOCD_PRINCIPAL_SPIRE_AGENT_SOCKET should not be set")
				assert.False(t, envNames[EnvArgoCDPrincipalSpireAuthMethod], "ARGOCD_PRINCIPAL_SPIRE_AUTH_METHOD should not be set")
				return
			}

			assert.Equal(t, tt.expectedSocket, envMap[EnvArgoCDPrincipalSpireAgentSocket],
				"ARGOCD_PRINCIPAL_SPIRE_AGENT_SOCKET mismatch")
			assert.Equal(t, tt.expectedAuthMethod, envMap[EnvArgoCDPrincipalSpireAuthMethod],
				"ARGOCD_PRINCIPAL_SPIRE_AUTH_METHOD mismatch")
		})
	}
}

func TestReconcilePrincipalDeployment_SPIFFE_VolumeMountsAndVolumes(t *testing.T) {
	// Test case: Verify SPIFFE CSI volume and mount are added when SPIFFE is configured
	cr := makeTestArgoCD(withPrincipalEnabled(true), withPrincipalSPIFFE(argoproj.SpiffeAuthMethodJWT, "custom.csi.driver.io"))
	saName := generateAgentResourceName(cr.Name, testCompName)

	resObjs := []client.Object{cr}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcilePrincipalDeployment(cl, testCompName, saName, cr, sch, tlsprofile.TLSConfigProfile{})
	assert.NoError(t, err)

	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.NoError(t, err)

	container := deployment.Spec.Template.Spec.Containers[0]

	// jwt-secret, userpass-passwd, redis-initial-pass, spiffe-workload-api
	assert.Len(t, container.VolumeMounts, 4)
	spiffeMount := container.VolumeMounts[3]
	assert.Equal(t, "spiffe-workload-api", spiffeMount.Name)
	assert.Equal(t, "/run/spire/sockets", spiffeMount.MountPath)
	assert.True(t, spiffeMount.ReadOnly)

	assert.Len(t, deployment.Spec.Template.Spec.Volumes, 4)
	spiffeVolume := deployment.Spec.Template.Spec.Volumes[3]
	assert.Equal(t, "spiffe-workload-api", spiffeVolume.Name)
	assert.NotNil(t, spiffeVolume.CSI)
	assert.Equal(t, "custom.csi.driver.io", spiffeVolume.CSI.Driver)
}

func TestReconcilePrincipalDeployment_SPIFFE_AddedOnUpdate(t *testing.T) {
	// Test case: SPIFFE is added to an existing CR after the deployment was created without it
	// Expected behavior: Deployment should be updated to add the SPIFFE volume and mount
	cr := makeTestArgoCD(withPrincipalEnabled(true))
	saName := generateAgentResourceName(cr.Name, testCompName)

	existingDeployment := makeTestDeployment(cr)

	resObjs := []client.Object{cr, existingDeployment}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	// Sanity check: no SPIFFE volume mount before SPIFFE is configured
	assert.Len(t, existingDeployment.Spec.Template.Spec.Containers[0].VolumeMounts, 3)

	cr.Spec.ArgoCDAgent.Principal.SPIFFE = &argoproj.SPIFFESpec{Enabled: true, AuthMethod: argoproj.SpiffeAuthMethodJWT}

	err := ReconcilePrincipalDeployment(cl, testCompName, saName, cr, sch, tlsprofile.TLSConfigProfile{})
	assert.NoError(t, err)

	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.NoError(t, err)

	container := deployment.Spec.Template.Spec.Containers[0]
	assert.Len(t, container.VolumeMounts, 4)
	assert.Equal(t, "spiffe-workload-api", container.VolumeMounts[3].Name)
	assert.Len(t, deployment.Spec.Template.Spec.Volumes, 4)
	assert.Equal(t, "spiffe-workload-api", deployment.Spec.Template.Spec.Volumes[3].Name)

	// Verify env vars were updated too
	envNames := make(map[string]bool)
	for _, e := range container.Env {
		envNames[e.Name] = true
	}
	assert.True(t, envNames[EnvArgoCDPrincipalSpireAgentSocket], "ARGOCD_PRINCIPAL_SPIRE_AGENT_SOCKET should be set")
	assert.True(t, envNames[EnvArgoCDPrincipalSpireAuthMethod], "ARGOCD_PRINCIPAL_SPIRE_AUTH_METHOD should be set")
}

func withPrincipalHA(enabled bool, preferredRole, peerAddress string, allowedClients []string, adminPort *int32, adminAuth, replicationTimeout string) argoCDOpt {
	return withPrincipalHAFull(enabled, preferredRole, peerAddress, "", allowedClients, adminPort, adminAuth, replicationTimeout)
}

func withPrincipalHAFull(enabled bool, preferredRole, peerAddress, peerTLSSecretName string, allowedClients []string, adminPort *int32, adminAuth, replicationTimeout string) argoCDOpt {
	return func(a *argoproj.ArgoCD) {
		if a.Spec.ArgoCDAgent == nil {
			a.Spec.ArgoCDAgent = &argoproj.ArgoCDAgentSpec{}
		}
		if a.Spec.ArgoCDAgent.Principal == nil {
			a.Spec.ArgoCDAgent.Principal = &argoproj.PrincipalSpec{}
		}
		a.Spec.ArgoCDAgent.Principal.HA = &argoproj.PrincipalHASpec{
			Enabled:                      &enabled,
			PreferredRole:                preferredRole,
			PeerAddress:                  peerAddress,
			PeerTLSSecretName:            peerTLSSecretName,
			AllowedReplicationClients:    allowedClients,
			AdminPort:                    adminPort,
			AdminAuth:                    adminAuth,
			ReplicationInitialAckTimeout: replicationTimeout,
		}
	}
}

func TestBuildPrincipalContainerEnv_HA_Enabled(t *testing.T) {
	adminPort := int32(PrincipalHAAdminPort)
	cr := makeTestArgoCD(withPrincipalEnabled(true), withPrincipalHA(
		true,
		"replica",
		"principal-primary.example.com:443",
		[]string{"primary"},
		&adminPort,
		"mtls:subject:CN=ha-admin",
		"10m",
	))

	env, err := buildPrincipalContainerEnv(cr, tlsprofile.TLSConfigProfile{DisableClusterTLSProfile: true})
	assert.NoError(t, err)

	envMap := make(map[string]string)
	for _, e := range env {
		envMap[e.Name] = e.Value
	}

	assert.Equal(t, "true", envMap[EnvArgoCDPrincipalHAEnabled])
	assert.Equal(t, "replica", envMap[EnvArgoCDPrincipalHAPreferredRole])
	assert.Equal(t, "principal-primary.example.com:443", envMap[EnvArgoCDPrincipalHAPeerAddress])
	assert.Equal(t, "primary", envMap[EnvArgoCDPrincipalHAAllowedReplicationClients])
	assert.Equal(t, strconv.Itoa(PrincipalHAAdminPort), envMap[EnvArgoCDPrincipalHAAdminPort])
	assert.Equal(t, "mtls:subject:CN=ha-admin", envMap[EnvArgoCDPrincipalHAAdminAuth])
	assert.Equal(t, "10m", envMap[EnvArgoCDPrincipalHAReplicationInitialAckTimeout])
	assert.Equal(t, "/app/config/ha-peer-tls/tls.crt", envMap[EnvArgoCDPrincipalHAPeerTLSCert])
	assert.Equal(t, "/app/config/ha-peer-tls/tls.key", envMap[EnvArgoCDPrincipalHAPeerTLSKey])
}

func TestReconcilePrincipalDeployment_HA_Enabled(t *testing.T) {
	adminPort := int32(PrincipalHAAdminPort)
	cr := makeTestArgoCD(withPrincipalEnabled(true), withPrincipalHA(
		true,
		"primary",
		"principal-replica.example.com:443",
		[]string{"replica"},
		&adminPort,
		"mtls:subject:CN=ha-admin",
		"5m",
	))

	resObjs := []client.Object{cr}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcilePrincipalDeployment(cl, testCompName, testCompName, cr, sch, tlsprofile.TLSConfigProfile{DisableClusterTLSProfile: true})
	assert.NoError(t, err)

	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.NoError(t, err)

	container := deployment.Spec.Template.Spec.Containers[0]
	envMap := make(map[string]string)
	for _, e := range container.Env {
		envMap[e.Name] = e.Value
	}
	assert.Equal(t, "true", envMap[EnvArgoCDPrincipalHAEnabled])
	assert.Equal(t, "primary", envMap[EnvArgoCDPrincipalHAPreferredRole])
	assert.Equal(t, "mtls:subject:CN=ha-admin", envMap[EnvArgoCDPrincipalHAAdminAuth])
	assert.Equal(t, "/app/config/ha-peer-tls/tls.crt", envMap[EnvArgoCDPrincipalHAPeerTLSCert])
	assert.Equal(t, "/app/config/ha-peer-tls/tls.key", envMap[EnvArgoCDPrincipalHAPeerTLSKey])

	portNames := make(map[string]int32)
	for _, p := range container.Ports {
		portNames[p.Name] = p.ContainerPort
	}
	assert.Equal(t, int32(PrincipalHAAdminPort), portNames["ha-admin"])

	// Dedicated replication client cert volume/mount
	foundMount := false
	for _, m := range container.VolumeMounts {
		if m.Name == "ha-peer-tls" {
			foundMount = true
			assert.Equal(t, "/app/config/ha-peer-tls", m.MountPath)
		}
	}
	assert.True(t, foundMount, "ha-peer-tls volume mount should be present when HA is enabled")

	foundVolume := false
	for _, v := range deployment.Spec.Template.Spec.Volumes {
		if v.Name == "ha-peer-tls" {
			foundVolume = true
			assert.NotNil(t, v.Secret)
			assert.Equal(t, "argocd-agent-ha-peer-tls", v.Secret.SecretName)
			assert.Equal(t, new(true), v.Secret.Optional)
		}
	}
	assert.True(t, foundVolume, "ha-peer-tls volume should be present when HA is enabled")
}

func TestReconcilePrincipalDeployment_HA_CustomPeerTLSSecret(t *testing.T) {
	adminPort := int32(PrincipalHAAdminPort)
	cr := makeTestArgoCD(withPrincipalEnabled(true), withPrincipalHAFull(
		true,
		"replica",
		"principal-primary.example.com:443",
		"custom-ha-peer-tls",
		[]string{"argocd-agent-principal-peer"},
		&adminPort,
		"mtls:subject:CN=ha-admin",
		"5m",
	))

	resObjs := []client.Object{cr}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcilePrincipalDeployment(cl, testCompName, testCompName, cr, sch, tlsprofile.TLSConfigProfile{DisableClusterTLSProfile: true})
	assert.NoError(t, err)

	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.NoError(t, err)

	foundVolume := false
	for _, v := range deployment.Spec.Template.Spec.Volumes {
		if v.Name == "ha-peer-tls" {
			foundVolume = true
			assert.Equal(t, "custom-ha-peer-tls", v.Secret.SecretName)
		}
	}
	assert.True(t, foundVolume)
}

func TestBuildPrincipalContainerEnv_HA_Disabled(t *testing.T) {
	cr := makeTestArgoCD(withPrincipalEnabled(true)) // no HA spec set

	env, err := buildPrincipalContainerEnv(cr, tlsprofile.TLSConfigProfile{DisableClusterTLSProfile: true})
	assert.NoError(t, err)

	envMap := make(map[string]string)
	for _, e := range env {
		envMap[e.Name] = e.Value
	}

	_, haPresent := envMap[EnvArgoCDPrincipalHAEnabled]
	assert.False(t, haPresent, "HA env vars should not be present when HA is not configured")
}

func TestBuildPorts_HA_Enabled(t *testing.T) {
	enabled := true
	cr := makeTestArgoCD(withPrincipalEnabled(true), func(a *argoproj.ArgoCD) {
		if a.Spec.ArgoCDAgent == nil {
			a.Spec.ArgoCDAgent = &argoproj.ArgoCDAgentSpec{}
		}
		if a.Spec.ArgoCDAgent.Principal == nil {
			a.Spec.ArgoCDAgent.Principal = &argoproj.PrincipalSpec{}
		}
		a.Spec.ArgoCDAgent.Principal.HA = &argoproj.PrincipalHASpec{
			Enabled: &enabled,
		}
	})

	ports := buildPorts(testCompName, cr)

	portNames := make(map[string]int32)
	for _, p := range ports {
		portNames[p.Name] = p.ContainerPort
	}

	assert.Equal(t, int32(PrincipalHAAdminPort), portNames["ha-admin"])
}

func TestBuildPorts_HA_Disabled(t *testing.T) {
	cr := makeTestArgoCD(withPrincipalEnabled(true))

	ports := buildPorts(testCompName, cr)

	for _, p := range ports {
		assert.NotEqual(t, "ha-admin", p.Name, "ha-admin port should not be present when HA is disabled")
	}
}

func TestBuildPorts_HA_CustomAdminPort(t *testing.T) {
	enabled := true
	customPort := int32(9405)
	cr := makeTestArgoCD(withPrincipalEnabled(true), func(a *argoproj.ArgoCD) {
		if a.Spec.ArgoCDAgent == nil {
			a.Spec.ArgoCDAgent = &argoproj.ArgoCDAgentSpec{}
		}
		if a.Spec.ArgoCDAgent.Principal == nil {
			a.Spec.ArgoCDAgent.Principal = &argoproj.PrincipalSpec{}
		}
		a.Spec.ArgoCDAgent.Principal.HA = &argoproj.PrincipalHASpec{
			Enabled:   &enabled,
			AdminPort: &customPort,
		}
	})

	ports := buildPorts(testCompName, cr)

	portNames := make(map[string]int32)
	for _, p := range ports {
		portNames[p.Name] = p.ContainerPort
	}

	assert.Equal(t, int32(9405), portNames["ha-admin"])
}

func TestGetPrincipalHAHelperFunctions(t *testing.T) {
	t.Run("hasPrincipalHA", func(t *testing.T) {
		// No HA configured
		cr := makeTestArgoCD(withPrincipalEnabled(true))
		assert.False(t, hasPrincipalHA(cr))

		// HA configured
		enabled := true
		cr = makeTestArgoCD(withPrincipalEnabled(true), func(a *argoproj.ArgoCD) {
			a.Spec.ArgoCDAgent.Principal.HA = &argoproj.PrincipalHASpec{
				Enabled: &enabled,
			}
		})
		assert.True(t, hasPrincipalHA(cr))
	})

	t.Run("isPrincipalHAEnabled", func(t *testing.T) {
		// No HA configured
		cr := makeTestArgoCD(withPrincipalEnabled(true))
		assert.False(t, isPrincipalHAEnabled(cr))

		// HA configured but disabled
		disabled := false
		cr = makeTestArgoCD(withPrincipalEnabled(true), func(a *argoproj.ArgoCD) {
			a.Spec.ArgoCDAgent.Principal.HA = &argoproj.PrincipalHASpec{
				Enabled: &disabled,
			}
		})
		assert.False(t, isPrincipalHAEnabled(cr))

		// HA configured and enabled
		enabled := true
		cr = makeTestArgoCD(withPrincipalEnabled(true), func(a *argoproj.ArgoCD) {
			a.Spec.ArgoCDAgent.Principal.HA = &argoproj.PrincipalHASpec{
				Enabled: &enabled,
			}
		})
		assert.True(t, isPrincipalHAEnabled(cr))
	})

	t.Run("getPrincipalHAPreferredRole", func(t *testing.T) {
		// Default value
		cr := makeTestArgoCD(withPrincipalEnabled(true))
		assert.Equal(t, "primary", getPrincipalHAPreferredRole(cr))

		// Custom value
		enabled := true
		cr = makeTestArgoCD(withPrincipalEnabled(true), func(a *argoproj.ArgoCD) {
			a.Spec.ArgoCDAgent.Principal.HA = &argoproj.PrincipalHASpec{
				Enabled:       &enabled,
				PreferredRole: "replica",
			}
		})
		assert.Equal(t, "replica", getPrincipalHAPreferredRole(cr))
	})

	t.Run("getPrincipalHAPeerAddress", func(t *testing.T) {
		// No HA configured
		cr := makeTestArgoCD(withPrincipalEnabled(true))
		assert.Equal(t, "", getPrincipalHAPeerAddress(cr))

		// Custom value
		enabled := true
		cr = makeTestArgoCD(withPrincipalEnabled(true), func(a *argoproj.ArgoCD) {
			a.Spec.ArgoCDAgent.Principal.HA = &argoproj.PrincipalHASpec{
				Enabled:     &enabled,
				PeerAddress: "peer.example.com:8443",
			}
		})
		assert.Equal(t, "peer.example.com:8443", getPrincipalHAPeerAddress(cr))
	})

	t.Run("getPrincipalHAPeerTLSSecretName", func(t *testing.T) {
		// Default value when HA is not configured
		cr := makeTestArgoCD(withPrincipalEnabled(true))
		assert.Equal(t, "argocd-agent-ha-peer-tls", getPrincipalHAPeerTLSSecretName(cr))

		// Default when HA enabled but secret name unset
		enabled := true
		cr = makeTestArgoCD(withPrincipalEnabled(true), func(a *argoproj.ArgoCD) {
			a.Spec.ArgoCDAgent.Principal.HA = &argoproj.PrincipalHASpec{
				Enabled: &enabled,
			}
		})
		assert.Equal(t, "argocd-agent-ha-peer-tls", getPrincipalHAPeerTLSSecretName(cr))

		// Custom value
		cr = makeTestArgoCD(withPrincipalEnabled(true), func(a *argoproj.ArgoCD) {
			a.Spec.ArgoCDAgent.Principal.HA = &argoproj.PrincipalHASpec{
				Enabled:           &enabled,
				PeerTLSSecretName: "custom-ha-peer-tls",
			}
		})
		assert.Equal(t, "custom-ha-peer-tls", getPrincipalHAPeerTLSSecretName(cr))
	})

	t.Run("getPrincipalHAAllowedReplicationClients", func(t *testing.T) {
		// No clients configured
		cr := makeTestArgoCD(withPrincipalEnabled(true))
		assert.Equal(t, "", getPrincipalHAAllowedReplicationClients(cr))

		// Single client
		enabled := true
		cr = makeTestArgoCD(withPrincipalEnabled(true), func(a *argoproj.ArgoCD) {
			a.Spec.ArgoCDAgent.Principal.HA = &argoproj.PrincipalHASpec{
				Enabled:                   &enabled,
				AllowedReplicationClients: []string{"primary"},
			}
		})
		assert.Equal(t, "primary", getPrincipalHAAllowedReplicationClients(cr))

		// Multiple clients
		cr = makeTestArgoCD(withPrincipalEnabled(true), func(a *argoproj.ArgoCD) {
			a.Spec.ArgoCDAgent.Principal.HA = &argoproj.PrincipalHASpec{
				Enabled:                   &enabled,
				AllowedReplicationClients: []string{"primary", "replica"},
			}
		})
		assert.Equal(t, "primary,replica", getPrincipalHAAllowedReplicationClients(cr))
	})

	t.Run("getPrincipalHAAdminPort", func(t *testing.T) {
		// Default value
		cr := makeTestArgoCD(withPrincipalEnabled(true))
		assert.Equal(t, strconv.Itoa(PrincipalHAAdminPort), getPrincipalHAAdminPort(cr))

		// Custom value
		enabled := true
		customPort := int32(9405)
		cr = makeTestArgoCD(withPrincipalEnabled(true), func(a *argoproj.ArgoCD) {
			a.Spec.ArgoCDAgent.Principal.HA = &argoproj.PrincipalHASpec{
				Enabled:   &enabled,
				AdminPort: &customPort,
			}
		})
		assert.Equal(t, "9405", getPrincipalHAAdminPort(cr))
	})

	t.Run("getPrincipalHAAdminAuth", func(t *testing.T) {
		// Default empty value
		cr := makeTestArgoCD(withPrincipalEnabled(true))
		assert.Equal(t, "", getPrincipalHAAdminAuth(cr))

		// Custom value
		enabled := true
		cr = makeTestArgoCD(withPrincipalEnabled(true), func(a *argoproj.ArgoCD) {
			a.Spec.ArgoCDAgent.Principal.HA = &argoproj.PrincipalHASpec{
				Enabled:   &enabled,
				AdminAuth: "mtls:subject:CN=ha-admin",
			}
		})
		assert.Equal(t, "mtls:subject:CN=ha-admin", getPrincipalHAAdminAuth(cr))
	})

	t.Run("getPrincipalHAReplicationInitialAckTimeout", func(t *testing.T) {
		// Default value
		cr := makeTestArgoCD(withPrincipalEnabled(true))
		assert.Equal(t, "5m", getPrincipalHAReplicationInitialAckTimeout(cr))

		// Custom value
		enabled := true
		cr = makeTestArgoCD(withPrincipalEnabled(true), func(a *argoproj.ArgoCD) {
			a.Spec.ArgoCDAgent.Principal.HA = &argoproj.PrincipalHASpec{
				Enabled:                      &enabled,
				ReplicationInitialAckTimeout: "10m",
			}
		})
		assert.Equal(t, "10m", getPrincipalHAReplicationInitialAckTimeout(cr))
	})
}
