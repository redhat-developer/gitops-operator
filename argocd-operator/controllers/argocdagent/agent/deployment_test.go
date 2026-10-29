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

package agent

import (
	"context"
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

	argoproj "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/argocd-operator/common"
)

// Helper function to create a test deployment
func makeTestDeployment(cr *argoproj.ArgoCD) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generateAgentResourceName(cr.Name, testAgentCompName),
			Namespace: cr.Namespace,
			Labels:    buildLabelsForAgent(cr.Name, testAgentCompName),
		},
		Spec: buildAgentSpec(testAgentCompName, generateAgentResourceName(cr.Name, testAgentCompName), cr),
	}
}

// Helper function to create a test deployment with custom image
func makeTestDeploymentWithCustomImage(cr *argoproj.ArgoCD, customImage string) *appsv1.Deployment {
	deployment := makeTestDeployment(cr)
	deployment.Spec.Template.Spec.Containers[0].Image = customImage
	return deployment
}

// Helper function to create ArgoCD with custom agent image
func withAgentImage(image string) argoCDOpt {
	return func(a *argoproj.ArgoCD) {
		if a.Spec.ArgoCDAgent == nil {
			a.Spec.ArgoCDAgent = &argoproj.ArgoCDAgentSpec{}
		}
		if a.Spec.ArgoCDAgent.Agent == nil {
			a.Spec.ArgoCDAgent.Agent = &argoproj.AgentSpec{}
		}
		a.Spec.ArgoCDAgent.Agent.Image = image
	}
}

func withResourceRequirements(resources corev1.ResourceRequirements) argoCDOpt {
	return func(a *argoproj.ArgoCD) {
		if a.Spec.ArgoCDAgent == nil {
			a.Spec.ArgoCDAgent = &argoproj.ArgoCDAgentSpec{}
		}
		if a.Spec.ArgoCDAgent.Agent == nil {
			a.Spec.ArgoCDAgent.Agent = &argoproj.AgentSpec{}
		}
		a.Spec.ArgoCDAgent.Agent.Resources = &resources
	}
}

// TestReconcileAgentDeployment tests

func TestReconcileAgentDeployment_DeploymentDoesNotExist_AgentDisabled(t *testing.T) {
	// Test case: Deployment doesn't exist and agent is disabled
	// Expected behavior: Should do nothing (no creation, no error)

	cr := makeTestArgoCD(withAgentEnabled(false))
	saName := generateAgentResourceName(cr.Name, testAgentCompName)

	resObjs := []client.Object{cr}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcileAgentDeployment(cl, testAgentCompName, saName, cr, sch)
	assert.NoError(t, err)

	// Verify Deployment was not created
	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testAgentCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.True(t, errors.IsNotFound(err))
}

func TestReconcileAgentDeployment_DeploymentDoesNotExist_AgentEnabled(t *testing.T) {
	// Test case: Deployment doesn't exist and agent is enabled
	// Expected behavior: Should create the Deployment with expected spec

	cr := makeTestArgoCD(withAgentEnabled(true))
	saName := generateAgentResourceName(cr.Name, testAgentCompName)

	resObjs := []client.Object{cr}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcileAgentDeployment(cl, testAgentCompName, saName, cr, sch)
	assert.NoError(t, err)

	// Verify Deployment was created
	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testAgentCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.NoError(t, err)

	// Verify Deployment has expected metadata
	assert.Equal(t, generateAgentResourceName(cr.Name, testAgentCompName), deployment.Name)
	assert.Equal(t, cr.Namespace, deployment.Namespace)
	assert.Equal(t, buildLabelsForAgent(cr.Name, testAgentCompName), deployment.Labels)

	// Verify Deployment has expected spec
	expectedSpec := buildAgentSpec(testAgentCompName, saName, cr)
	assert.Equal(t, expectedSpec.Selector, deployment.Spec.Selector)
	assert.Equal(t, expectedSpec.Template.Labels, deployment.Spec.Template.Labels)
	assert.Equal(t, expectedSpec.Template.Spec.ServiceAccountName, deployment.Spec.Template.Spec.ServiceAccountName)

	// Verify container configuration
	assert.Len(t, deployment.Spec.Template.Spec.Containers, 1)
	container := deployment.Spec.Template.Spec.Containers[0]
	assert.Equal(t, generateAgentResourceName(cr.Name, testAgentCompName), container.Name)
	assert.Equal(t, buildAgentImage(cr), container.Image)
	assert.Equal(t, corev1.PullAlways, container.ImagePullPolicy)
	assert.Equal(t, buildArgs(testAgentCompName), container.Args)
	assert.Equal(t, buildAgentContainerEnv(cr), container.Env)
	assert.Equal(t, buildSecurityContext(), container.SecurityContext)
	assert.Equal(t, buildPorts(), container.Ports)

	// Verify owner reference is set
	assert.Len(t, deployment.OwnerReferences, 1)
	assert.Equal(t, cr.Name, deployment.OwnerReferences[0].Name)
	assert.Equal(t, "ArgoCD", deployment.OwnerReferences[0].Kind)
}

func TestReconcileAgentDeployment_DeploymentExists_AgentDisabled(t *testing.T) {
	// Test case: Deployment exists and agent is disabled
	// Expected behavior: Should delete the Deployment

	cr := makeTestArgoCD(withAgentEnabled(false))
	saName := generateAgentResourceName(cr.Name, testAgentCompName)

	// Create existing Deployment
	existingDeployment := makeTestDeployment(cr)

	resObjs := []client.Object{cr, existingDeployment}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcileAgentDeployment(cl, testAgentCompName, saName, cr, sch)
	assert.NoError(t, err)

	// Verify Deployment was deleted
	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testAgentCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.True(t, errors.IsNotFound(err))
}

func TestReconcileAgentDeployment_DeploymentExists_AgentEnabled_NoChanges(t *testing.T) {
	// Test case: Deployment exists, agent is enabled, and no changes are needed
	// Expected behavior: Should not update the Deployment

	cr := makeTestArgoCD(withAgentEnabled(true))
	saName := generateAgentResourceName(cr.Name, testAgentCompName)

	// Create existing Deployment with correct spec
	existingDeployment := makeTestDeployment(cr)

	resObjs := []client.Object{cr, existingDeployment}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcileAgentDeployment(cl, testAgentCompName, saName, cr, sch)
	assert.NoError(t, err)

	// Verify Deployment still exists with same spec
	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testAgentCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.NoError(t, err)
	assert.Equal(t, buildAgentImage(cr), deployment.Spec.Template.Spec.Containers[0].Image)
	assert.Equal(t, saName, deployment.Spec.Template.Spec.ServiceAccountName)
}

func TestReconcileAgentDeployment_DeploymentExists_AgentEnabled_ImageChanged(t *testing.T) {
	// Test case: Deployment exists, agent is enabled, but image has changed
	// Expected behavior: Should update the Deployment with new image

	cr := makeTestArgoCD(withAgentEnabled(true), withAgentImage("quay.io/argoproj/argocd-agent:v2"))
	saName := generateAgentResourceName(cr.Name, testAgentCompName)

	// Create existing Deployment with old image
	existingDeployment := makeTestDeploymentWithCustomImage(cr, "quay.io/argoproj/argocd-agent:v1")

	resObjs := []client.Object{cr, existingDeployment}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcileAgentDeployment(cl, testAgentCompName, saName, cr, sch)
	assert.NoError(t, err)

	// Verify Deployment was updated with new image
	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testAgentCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.NoError(t, err)
	assert.Equal(t, "quay.io/argoproj/argocd-agent:v2", deployment.Spec.Template.Spec.Containers[0].Image)
}

func TestReconcileAgentDeployment_DeploymentExists_AgentEnabled_ServiceAccountChanged(t *testing.T) {
	// Test case: Deployment exists, agent is enabled, but service account has changed
	// Expected behavior: Should update the Deployment with new service account

	cr := makeTestArgoCD(withAgentEnabled(true))
	oldSAName := "old-service-account"
	newSAName := "new-service-account"

	// Create existing Deployment with old service account
	existingDeployment := makeTestDeployment(cr)
	existingDeployment.Spec.Template.Spec.ServiceAccountName = oldSAName

	resObjs := []client.Object{cr, existingDeployment}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcileAgentDeployment(cl, testAgentCompName, newSAName, cr, sch)
	assert.NoError(t, err)

	// Verify Deployment was updated with new service account
	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testAgentCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.NoError(t, err)
	assert.Equal(t, newSAName, deployment.Spec.Template.Spec.ServiceAccountName)
}

func TestReconcileAgentDeployment_DeploymentExists_AgentEnabled_VolumesChanged(t *testing.T) {
	// Test case: Deployment exists, agent is enabled, but volumes have changed
	// Expected behavior: Should update the Deployment with expected volumes

	cr := makeTestArgoCD(withAgentEnabled(true))

	// Create existing Deployment with old service account
	existingDeployment := makeTestDeployment(cr)
	// existingDeployment.Spec.Template.Spec.Volumes

	resObjs := []client.Object{cr, existingDeployment}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	require.NoError(t, ReconcileAgentDeployment(cl, testAgentCompName, testAgentCompName, cr, sch))

	deployment := &appsv1.Deployment{}
	deploymentName := types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testAgentCompName),
		Namespace: cr.Namespace,
	}
	require.NoError(t, cl.Get(context.TODO(), deploymentName, deployment))

	deployment.Spec.Template.Spec.Volumes = []corev1.Volume{}
	require.NoError(t, cl.Update(context.TODO(), deployment))

	require.NoError(t, ReconcileAgentDeployment(cl, testAgentCompName, testAgentCompName, cr, sch))
	require.NoError(t, cl.Get(context.TODO(), deploymentName, deployment))

	assert.Len(t, deployment.Spec.Template.Spec.Volumes, 2)
}

func TestReconcileAgentDeployment_DeploymentExists_AgentNotSet(t *testing.T) {
	// Test case: Deployment exists but agent is not set (nil)
	// Expected behavior: Should delete the Deployment

	cr := makeTestArgoCD() // No agent configuration
	saName := generateAgentResourceName(cr.Name, testAgentCompName)

	// Create existing Deployment
	existingDeployment := makeTestDeployment(cr)

	resObjs := []client.Object{cr, existingDeployment}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcileAgentDeployment(cl, testAgentCompName, saName, cr, sch)
	assert.NoError(t, err)

	// Verify Deployment was deleted
	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testAgentCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.True(t, errors.IsNotFound(err))
}

func TestReconcileAgentDeployment_DeploymentDoesNotExist_AgentNotSet(t *testing.T) {
	// Test case: Deployment doesn't exist and ArgoCDAgent is not set (nil)
	// Expected behavior: Should do nothing since agent is effectively disabled

	cr := makeTestArgoCD() // No agent configuration
	saName := generateAgentResourceName(cr.Name, testAgentCompName)

	resObjs := []client.Object{cr}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcileAgentDeployment(cl, testAgentCompName, saName, cr, sch)
	assert.NoError(t, err)

	// Verify Deployment was not created
	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testAgentCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.True(t, errors.IsNotFound(err))
}

func TestReconcileAgentDeployment_VerifyDeploymentSpec(t *testing.T) {
	// Test case: Verify the deployment spec has correct configuration
	// Expected behavior: Should create deployment with correct security context, ports, etc.

	cr := makeTestArgoCD(withAgentEnabled(true))
	saName := generateAgentResourceName(cr.Name, testAgentCompName)

	resObjs := []client.Object{cr}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcileAgentDeployment(cl, testAgentCompName, saName, cr, sch)
	assert.NoError(t, err)

	// Verify Deployment was created
	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testAgentCompName),
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

	// Verify ports configuration (agent only has metrics and healthz)
	assert.Len(t, container.Ports, 2)
	metricsPort := container.Ports[0]
	assert.Equal(t, "metrics", metricsPort.Name)
	assert.Equal(t, int32(8181), metricsPort.ContainerPort)
	assert.Equal(t, corev1.ProtocolTCP, metricsPort.Protocol)
	healthzPort := container.Ports[1]
	assert.Equal(t, "healthz", healthzPort.Name)
	assert.Equal(t, int32(8002), healthzPort.ContainerPort)
	assert.Equal(t, corev1.ProtocolTCP, healthzPort.Protocol)

	// Verify args
	assert.Equal(t, []string{testAgentCompName}, container.Args)

	// Verify environment variables are set
	assert.True(t, len(container.Env) > 0)
	// Verify some expected environment variables are present
	envNames := make(map[string]bool)
	for _, env := range container.Env {
		envNames[env.Name] = true
		// All environment variables should have direct values, not references
		assert.Nil(t, env.ValueFrom, "Environment variable %s should have direct value, not reference", env.Name)
	}
	// Check for some agent-specific environment variables
	assert.True(t, envNames["ARGOCD_AGENT_REMOTE_SERVER"], "ARGOCD_AGENT_REMOTE_SERVER should be set")
	assert.True(t, envNames["ARGOCD_AGENT_LOG_LEVEL"], "ARGOCD_AGENT_LOG_LEVEL should be set")
}

func TestReconcileAgentDeployment_CustomImage(t *testing.T) {
	// Test case: Verify custom image is used when specified
	// Expected behavior: Should create deployment with custom image

	customImage := "custom-registry/argocd-agent:custom-tag"
	cr := makeTestArgoCD(withAgentEnabled(true), withAgentImage(customImage))
	saName := generateAgentResourceName(cr.Name, testAgentCompName)

	resObjs := []client.Object{cr}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcileAgentDeployment(cl, testAgentCompName, saName, cr, sch)
	assert.NoError(t, err)

	// Verify Deployment was created with custom image
	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testAgentCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.NoError(t, err)
	assert.Equal(t, customImage, deployment.Spec.Template.Spec.Containers[0].Image)
}

func TestReconcileAgentDeployment_DefaultImage(t *testing.T) {
	// Test case: Verify default image is used when no custom image is specified
	// Expected behavior: Should create deployment with default image

	cr := makeTestArgoCD(withAgentEnabled(true))
	saName := generateAgentResourceName(cr.Name, testAgentCompName)

	resObjs := []client.Object{cr}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcileAgentDeployment(cl, testAgentCompName, saName, cr, sch)
	assert.NoError(t, err)

	// Verify Deployment was created with default image
	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testAgentCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.NoError(t, err)
	assert.Equal(t, common.ArgoCDAgentAgentDefaultImageName, deployment.Spec.Template.Spec.Containers[0].Image)
}

func TestReconcileAgentDeployment_VolumeMountsAndVolumes(t *testing.T) {
	// Test case: Verify volume mounts and volumes are correctly configured
	// Expected behavior: Should create deployment with JWT and userpass volume mounts and volumes

	cr := makeTestArgoCD(withAgentEnabled(true))
	saName := generateAgentResourceName(cr.Name, testAgentCompName)

	resObjs := []client.Object{cr}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcileAgentDeployment(cl, testAgentCompName, saName, cr, sch)
	assert.NoError(t, err)

	// Verify Deployment was created
	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testAgentCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.NoError(t, err)

	// Verify volume mounts
	container := deployment.Spec.Template.Spec.Containers[0]

	// Verify specific volume mount details
	assert.Len(t, container.VolumeMounts, 2)
	userpassMount := container.VolumeMounts[0]
	assert.Equal(t, "userpass-passwd", userpassMount.Name)
	assert.Equal(t, "/app/config/creds", userpassMount.MountPath)

	assert.Equal(t, "redis-initial-pass", container.VolumeMounts[1].Name)

	// Verify specific volume details
	assert.Len(t, deployment.Spec.Template.Spec.Volumes, 2)
	userpassVolume := deployment.Spec.Template.Spec.Volumes[0]
	assert.Equal(t, "userpass-passwd", userpassVolume.Name)
	assert.Equal(t, "argocd-agent-agent-userpass", userpassVolume.Secret.SecretName)
	assert.Equal(t, new(true), userpassVolume.Secret.Optional)

	assert.Equal(t, "redis-initial-pass", deployment.Spec.Template.Spec.Volumes[1].Name)
}

func TestBuildAgentImage(t *testing.T) {
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
				withAgentEnabled(true),
				withAgentImage("custom-registry/argocd-agent:custom-tag"),
			),
			envImage:      "env-registry/argocd-agent:env-tag",
			expectedImage: "custom-registry/argocd-agent:custom-tag",
			description:   "When CR specifies an image, it should take precedence over environment variable and default",
		},
		{
			name: "Environment variable used when CR image not specified",
			cr: makeTestArgoCD(
				withAgentEnabled(true),
			),
			envImage:      "env-registry/argocd-agent:env-tag",
			expectedImage: "env-registry/argocd-agent:env-tag",
			description:   "When CR doesn't specify an image but environment variable is set, use environment variable",
		},
		{
			name: "Default image used when neither CR nor environment specified",
			cr: makeTestArgoCD(
				withAgentEnabled(true),
			),
			envImage:      "",
			expectedImage: common.ArgoCDAgentAgentDefaultImageName,
			description:   "When neither CR nor environment variable specifies an image, use default",
		},
		{
			name: "Empty CR image should not override environment variable",
			cr: makeTestArgoCD(
				withAgentEnabled(true),
				withAgentImage(""),
			),
			envImage:      "env-registry/argocd-agent:env-tag",
			expectedImage: "env-registry/argocd-agent:env-tag",
			description:   "When CR specifies empty image, environment variable should be used",
		},
		{
			name: "Default image used when CR image is empty and no environment variable",
			cr: makeTestArgoCD(
				withAgentEnabled(true),
				withAgentImage(""),
			),
			envImage:      "",
			expectedImage: common.ArgoCDAgentAgentDefaultImageName,
			description:   "When CR specifies empty image and no environment variable, use default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable if specified
			if tt.envImage != "" {
				t.Setenv("ARGOCD_AGENT_IMAGE", tt.envImage)
			} else {
				// Clear environment variable
				t.Setenv("ARGOCD_AGENT_IMAGE", "")
			}

			result := buildAgentImage(tt.cr)
			assert.Equal(t, tt.expectedImage, result, tt.description)
		})
	}
}

// withAgentSPIFFE configures SPIFFE on the Agent spec
func withAgentSPIFFE(authMethod, csiDriverName string) argoCDOpt {
	return func(a *argoproj.ArgoCD) {
		if a.Spec.ArgoCDAgent == nil {
			a.Spec.ArgoCDAgent = &argoproj.ArgoCDAgentSpec{}
		}
		if a.Spec.ArgoCDAgent.Agent == nil {
			a.Spec.ArgoCDAgent.Agent = &argoproj.AgentSpec{}
		}
		a.Spec.ArgoCDAgent.Agent.SPIFFE = &argoproj.SPIFFESpec{
			Enabled:       true,
			AuthMethod:    authMethod,
			CSIDriverName: csiDriverName,
		}
	}
}

// withAgentTLS configures legacy TLS on the Agent spec
func withAgentTLS(secretName, rootCASecretName string) argoCDOpt {
	return func(a *argoproj.ArgoCD) {
		if a.Spec.ArgoCDAgent == nil {
			a.Spec.ArgoCDAgent = &argoproj.ArgoCDAgentSpec{}
		}
		if a.Spec.ArgoCDAgent.Agent == nil {
			a.Spec.ArgoCDAgent.Agent = &argoproj.AgentSpec{}
		}
		a.Spec.ArgoCDAgent.Agent.TLS = &argoproj.AgentTLSSpec{
			SecretName:       secretName,
			RootCASecretName: rootCASecretName,
		}
	}
}

func TestHasAgentSPIFFE(t *testing.T) {
	tests := []struct {
		name     string
		cr       *argoproj.ArgoCD
		expected bool
	}{
		{
			name:     "agent not configured",
			cr:       makeTestArgoCD(),
			expected: false,
		},
		{
			name:     "agent enabled without SPIFFE",
			cr:       makeTestArgoCD(withAgentEnabled(true)),
			expected: false,
		},
		{
			name: "SPIFFE block present but enabled=false",
			cr: makeTestArgoCD(withAgentEnabled(true), func(a *argoproj.ArgoCD) {
				a.Spec.ArgoCDAgent.Agent.SPIFFE = &argoproj.SPIFFESpec{Enabled: false}
			}),
			expected: false,
		},
		{
			name:     "SPIFFE configured with enabled=true",
			cr:       makeTestArgoCD(withAgentEnabled(true), withAgentSPIFFE("", "")),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, hasAgentSPIFFE(tt.cr))
		})
	}
}

func TestGetAgentSpireAuthMethod(t *testing.T) {
	tests := []struct {
		name     string
		cr       *argoproj.ArgoCD
		expected string
	}{
		{
			name:     "SPIFFE not configured defaults to jwt",
			cr:       makeTestArgoCD(withAgentEnabled(true)),
			expected: common.SpiffeAuthMethodJWT,
		},
		{
			name:     "SPIFFE configured without authMethod defaults to jwt",
			cr:       makeTestArgoCD(withAgentEnabled(true), withAgentSPIFFE("", "")),
			expected: common.SpiffeAuthMethodJWT,
		},
		{
			name:     "SPIFFE configured with mtls authMethod",
			cr:       makeTestArgoCD(withAgentEnabled(true), withAgentSPIFFE(common.SpiffeAuthMethodMTLS, "")),
			expected: common.SpiffeAuthMethodMTLS,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, getAgentSpireAuthMethod(tt.cr))
		})
	}
}

func TestGetAgentSpiffeCSIDriver(t *testing.T) {
	tests := []struct {
		name     string
		cr       *argoproj.ArgoCD
		expected string
	}{
		{
			name:     "SPIFFE not configured defaults to csi.spiffe.io",
			cr:       makeTestArgoCD(withAgentEnabled(true)),
			expected: "csi.spiffe.io",
		},
		{
			name:     "SPIFFE configured without csiDriverName defaults to csi.spiffe.io",
			cr:       makeTestArgoCD(withAgentEnabled(true), withAgentSPIFFE(common.SpiffeAuthMethodJWT, "")),
			expected: "csi.spiffe.io",
		},
		{
			name:     "SPIFFE configured with custom csiDriverName",
			cr:       makeTestArgoCD(withAgentEnabled(true), withAgentSPIFFE(common.SpiffeAuthMethodJWT, "custom.csi.driver.io")),
			expected: "custom.csi.driver.io",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, getAgentSpiffeCSIDriver(tt.cr))
		})
	}
}

func TestBuildSpiffeVolumeMount_Agent(t *testing.T) {
	mount := buildSpiffeVolumeMount()
	assert.Equal(t, "spiffe-workload-api", mount.Name)
	assert.Equal(t, "/run/spire/sockets", mount.MountPath)
	assert.True(t, mount.ReadOnly)
}

func TestBuildSpiffeVolume_Agent(t *testing.T) {
	volume := buildSpiffeVolume("custom.csi.driver.io")
	assert.Equal(t, "spiffe-workload-api", volume.Name)
	assert.NotNil(t, volume.CSI)
	assert.Equal(t, "custom.csi.driver.io", volume.CSI.Driver)
	assert.NotNil(t, volume.CSI.ReadOnly)
	assert.True(t, *volume.CSI.ReadOnly)
}

func TestBuildAgentContainerEnv_SPIFFE(t *testing.T) {
	tests := []struct {
		name               string
		cr                 *argoproj.ArgoCD
		expectSocketSet    bool
		expectedSocket     string
		expectedAuthMethod string
	}{
		{
			name:            "SPIFFE not configured, env vars absent",
			cr:              makeTestArgoCD(withAgentEnabled(true)),
			expectSocketSet: false,
		},
		{
			name:               "SPIFFE configured with jwt auth method",
			cr:                 makeTestArgoCD(withAgentEnabled(true), withAgentSPIFFE(common.SpiffeAuthMethodJWT, "")),
			expectSocketSet:    true,
			expectedSocket:     "unix:///run/spire/sockets/spire-agent.sock",
			expectedAuthMethod: common.SpiffeAuthMethodJWT,
		},
		{
			name:               "SPIFFE configured with mtls auth method",
			cr:                 makeTestArgoCD(withAgentEnabled(true), withAgentSPIFFE(common.SpiffeAuthMethodMTLS, "")),
			expectSocketSet:    true,
			expectedSocket:     "unix:///run/spire/sockets/spire-agent.sock",
			expectedAuthMethod: common.SpiffeAuthMethodMTLS,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envVars := buildAgentContainerEnv(tt.cr)
			envMap := make(map[string]string)
			envNames := make(map[string]bool)
			for _, e := range envVars {
				envMap[e.Name] = e.Value
				envNames[e.Name] = true
			}

			if !tt.expectSocketSet {
				assert.False(t, envNames[EnvArgoCDAgentSpireAgentSocket], "ARGOCD_AGENT_SPIRE_AGENT_SOCKET should not be set")
				assert.False(t, envNames[EnvArgoCDAgentSpireAuthMethod], "ARGOCD_AGENT_SPIRE_AUTH_METHOD should not be set")
				return
			}

			assert.Equal(t, tt.expectedSocket, envMap[EnvArgoCDAgentSpireAgentSocket],
				"ARGOCD_AGENT_SPIRE_AGENT_SOCKET mismatch")
			assert.Equal(t, tt.expectedAuthMethod, envMap[EnvArgoCDAgentSpireAuthMethod],
				"ARGOCD_AGENT_SPIRE_AUTH_METHOD mismatch")
		})
	}
}

func TestBuildAgentContainerEnv_SPIFFE_TLSPrecedence(t *testing.T) {
	// Test case: When both SPIFFE and legacy TLS are configured, SPIFFE env vars
	// should still be set (SPIFFE takes precedence) and legacy TLS env vars should
	// retain their configured values (the agent binary ignores them, operator doesn't need to clear them).
	cr := makeTestArgoCD(
		withAgentEnabled(true),
		withAgentSPIFFE(common.SpiffeAuthMethodJWT, ""),
		withAgentTLS("legacy-tls-secret", "legacy-ca-secret"),
	)

	envVars := buildAgentContainerEnv(cr)
	envMap := make(map[string]string)
	for _, e := range envVars {
		envMap[e.Name] = e.Value
	}

	assert.Equal(t, "unix:///run/spire/sockets/spire-agent.sock", envMap[EnvArgoCDAgentSpireAgentSocket])
	assert.Equal(t, common.SpiffeAuthMethodJWT, envMap[EnvArgoCDAgentSpireAuthMethod])
	assert.Equal(t, "legacy-tls-secret", envMap[EnvArgoCDAgentTLSSecretName])
	assert.Equal(t, "legacy-ca-secret", envMap[EnvArgoCDAgentTLSRootCASecretName])
}

func TestReconcileAgentDeployment_SPIFFE_VolumeMountsAndVolumes(t *testing.T) {
	// Test case: Verify SPIFFE CSI volume and mount are added when SPIFFE is configured
	cr := makeTestArgoCD(withAgentEnabled(true), withAgentSPIFFE(common.SpiffeAuthMethodJWT, "custom.csi.driver.io"))
	saName := generateAgentResourceName(cr.Name, testAgentCompName)

	resObjs := []client.Object{cr}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcileAgentDeployment(cl, testAgentCompName, saName, cr, sch)
	assert.NoError(t, err)

	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testAgentCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.NoError(t, err)

	container := deployment.Spec.Template.Spec.Containers[0]

	// userpass-passwd, redis-initial-pass, spiffe-workload-api
	assert.Len(t, container.VolumeMounts, 3)
	spiffeMount := container.VolumeMounts[2]
	assert.Equal(t, "spiffe-workload-api", spiffeMount.Name)
	assert.Equal(t, "/run/spire/sockets", spiffeMount.MountPath)
	assert.True(t, spiffeMount.ReadOnly)

	assert.Len(t, deployment.Spec.Template.Spec.Volumes, 3)
	spiffeVolume := deployment.Spec.Template.Spec.Volumes[2]
	assert.Equal(t, "spiffe-workload-api", spiffeVolume.Name)
	assert.NotNil(t, spiffeVolume.CSI)
	assert.Equal(t, "custom.csi.driver.io", spiffeVolume.CSI.Driver)
}

func TestReconcileAgentDeployment_SPIFFE_AddedOnUpdate(t *testing.T) {
	// Test case: SPIFFE is added to an existing CR after the deployment was created without it
	// Expected behavior: Deployment should be updated to add the SPIFFE volume and mount
	cr := makeTestArgoCD(withAgentEnabled(true))
	saName := generateAgentResourceName(cr.Name, testAgentCompName)

	existingDeployment := makeTestDeployment(cr)

	resObjs := []client.Object{cr, existingDeployment}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	// Sanity check: no SPIFFE volume mount before SPIFFE is configured
	assert.Len(t, existingDeployment.Spec.Template.Spec.Containers[0].VolumeMounts, 2)

	cr.Spec.ArgoCDAgent.Agent.SPIFFE = &argoproj.SPIFFESpec{Enabled: true, AuthMethod: common.SpiffeAuthMethodJWT}

	err := ReconcileAgentDeployment(cl, testAgentCompName, saName, cr, sch)
	assert.NoError(t, err)

	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, testAgentCompName),
		Namespace: cr.Namespace,
	}, deployment)
	assert.NoError(t, err)

	container := deployment.Spec.Template.Spec.Containers[0]
	assert.Len(t, container.VolumeMounts, 3)
	assert.Equal(t, "spiffe-workload-api", container.VolumeMounts[2].Name)
	assert.Len(t, deployment.Spec.Template.Spec.Volumes, 3)
	assert.Equal(t, "spiffe-workload-api", deployment.Spec.Template.Spec.Volumes[2].Name)

	// Verify env vars were updated too
	envNames := make(map[string]bool)
	for _, e := range container.Env {
		envNames[e.Name] = true
	}
	assert.True(t, envNames[EnvArgoCDAgentSpireAgentSocket], "ARGOCD_AGENT_SPIRE_AGENT_SOCKET should be set")
	assert.True(t, envNames[EnvArgoCDAgentSpireAuthMethod], "ARGOCD_AGENT_SPIRE_AUTH_METHOD should be set")
}

// withAgentAllowedNamespaces configures AllowedNamespaces on the Agent spec
func withAgentAllowedNamespaces(namespaces []string) argoCDOpt {
	return func(a *argoproj.ArgoCD) {
		if a.Spec.ArgoCDAgent == nil {
			a.Spec.ArgoCDAgent = &argoproj.ArgoCDAgentSpec{}
		}
		if a.Spec.ArgoCDAgent.Agent == nil {
			a.Spec.ArgoCDAgent.Agent = &argoproj.AgentSpec{}
		}
		a.Spec.ArgoCDAgent.Agent.AllowedNamespaces = namespaces
	}
}

func TestGetAgentDestinationBasedMapping(t *testing.T) {
	tests := []struct {
		name     string
		cr       *argoproj.ArgoCD
		expected string
	}{
		{
			name:     "agent not configured",
			cr:       makeTestArgoCD(),
			expected: "false",
		},
		{
			name:     "agent enabled without DBM",
			cr:       makeTestArgoCD(withAgentEnabled(true)),
			expected: "false",
		},
		{
			name:     "DBM explicitly disabled",
			cr:       makeTestArgoCD(withAgentEnabled(true), withAgentDestinationMapping(false, false)),
			expected: "false",
		},
		{
			name:     "DBM enabled",
			cr:       makeTestArgoCD(withAgentEnabled(true), withAgentDestinationMapping(true, false)),
			expected: "true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, getAgentDestinationBasedMapping(tt.cr))
		})
	}
}

func TestGetAgentCreateNamespace(t *testing.T) {
	tests := []struct {
		name     string
		cr       *argoproj.ArgoCD
		expected string
	}{
		{
			name:     "agent not configured",
			cr:       makeTestArgoCD(),
			expected: "false",
		},
		{
			name:     "agent enabled without DBM",
			cr:       makeTestArgoCD(withAgentEnabled(true)),
			expected: "false",
		},
		{
			name:     "DBM enabled without createNamespace",
			cr:       makeTestArgoCD(withAgentEnabled(true), withAgentDestinationMapping(true, false)),
			expected: "false",
		},
		{
			name:     "DBM disabled with createNamespace true",
			cr:       makeTestArgoCD(withAgentEnabled(true), withAgentDestinationMapping(false, true)),
			expected: "false",
		},
		{
			name:     "DBM enabled with createNamespace",
			cr:       makeTestArgoCD(withAgentEnabled(true), withAgentDestinationMapping(true, true)),
			expected: "true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, getAgentCreateNamespace(tt.cr))
		})
	}
}

func TestGetAgentAllowedNamespaces(t *testing.T) {
	tests := []struct {
		name     string
		cr       *argoproj.ArgoCD
		expected string
	}{
		{
			name:     "agent not configured",
			cr:       makeTestArgoCD(),
			expected: "",
		},
		{
			name:     "agent enabled without allowed namespaces",
			cr:       makeTestArgoCD(withAgentEnabled(true)),
			expected: "",
		},
		{
			name:     "single namespace",
			cr:       makeTestArgoCD(withAgentEnabled(true), withAgentAllowedNamespaces([]string{"ns1"})),
			expected: "ns1",
		},
		{
			name:     "multiple namespaces",
			cr:       makeTestArgoCD(withAgentEnabled(true), withAgentAllowedNamespaces([]string{"ns1", "ns2", "ns3"})),
			expected: "ns1,ns2,ns3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, getAgentAllowedNamespaces(tt.cr))
		})
	}
}

func TestBuildAgentContainerEnv_DestinationBasedMappingVars(t *testing.T) {
	tests := []struct {
		name              string
		cr                *argoproj.ArgoCD
		wantDBMValue      string
		wantCreateNSValue string
		wantAllowedNS     string
	}{
		{
			name:              "defaults without DBM",
			cr:                makeTestArgoCD(withAgentEnabled(true)),
			wantDBMValue:      "false",
			wantCreateNSValue: "false",
			wantAllowedNS:     "",
		},
		{
			name:              "DBM enabled",
			cr:                makeTestArgoCD(withAgentEnabled(true), withAgentDestinationMapping(true, false)),
			wantDBMValue:      "true",
			wantCreateNSValue: "false",
			wantAllowedNS:     "",
		},
		{
			name: "DBM enabled with createNamespace and allowedNamespaces",
			cr: makeTestArgoCD(
				withAgentEnabled(true),
				withAgentDestinationMapping(true, true),
				withAgentAllowedNamespaces([]string{"ns1", "ns2"}),
			),
			wantDBMValue:      "true",
			wantCreateNSValue: "true",
			wantAllowedNS:     "ns1,ns2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envVars := buildAgentContainerEnv(tt.cr)
			envMap := make(map[string]string)
			for _, e := range envVars {
				envMap[e.Name] = e.Value
			}

			assert.Equal(t, tt.wantDBMValue, envMap[EnvArgoCDAgentDestinationBasedMap],
				"ARGOCD_AGENT_DESTINATION_BASED_MAPPING mismatch")
			assert.Equal(t, tt.wantCreateNSValue, envMap[EnvArgoCDAgentCreateNamespace],
				"ARGOCD_AGENT_CREATE_NAMESPACE mismatch")
			assert.Equal(t, tt.wantAllowedNS, envMap[EnvArgoCDAgentAllowedNamespaces],
				"ARGOCD_AGENT_ALLOWED_NAMESPACES mismatch")
		})
	}
}

func TestReconcileAgentDeployment_ResourceRequirements(t *testing.T) {
	cr := makeTestArgoCD(withAgentEnabled(true), withResourceRequirements(corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resourcev1.MustParse("500m"),
			corev1.ResourceMemory: resourcev1.MustParse("512Mi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resourcev1.MustParse("250m"),
			corev1.ResourceMemory: resourcev1.MustParse("256Mi"),
		},
	}))
	saName := generateAgentResourceName(cr.Name, "agent")

	resObjs := []client.Object{cr}
	sch := makeTestReconcilerScheme()
	cl := makeTestReconcilerClient(sch, resObjs)

	err := ReconcileAgentDeployment(cl, "agent", saName, cr, sch)
	assert.NoError(t, err)

	deployment := &appsv1.Deployment{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, "agent"),
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
	cr.Spec.ArgoCDAgent.Agent.Resources = &updatedResources

	err = ReconcileAgentDeployment(cl, "agent", saName, cr, sch)
	assert.NoError(t, err)

	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      generateAgentResourceName(cr.Name, "agent"),
		Namespace: cr.Namespace,
	}, deployment)
	assert.NoError(t, err)

	assert.Equal(t, updatedResources, deployment.Spec.Template.Spec.Containers[0].Resources)
}

func withAgentLabelSelector(selector string) argoCDOpt {
	return func(a *argoproj.ArgoCD) {
		if a.Spec.ArgoCDAgent == nil {
			a.Spec.ArgoCDAgent = &argoproj.ArgoCDAgentSpec{}
		}
		if a.Spec.ArgoCDAgent.Agent == nil {
			a.Spec.ArgoCDAgent.Agent = &argoproj.AgentSpec{}
		}
		a.Spec.ArgoCDAgent.Agent.LabelSelector = selector
	}
}

func TestGetAgentLabelSelector(t *testing.T) {
	tests := []struct {
		name     string
		cr       *argoproj.ArgoCD
		expected string
	}{
		{
			name:     "agent not configured",
			cr:       makeTestArgoCD(),
			expected: "",
		},
		{
			name:     "agent enabled without label selector",
			cr:       makeTestArgoCD(withAgentEnabled(true)),
			expected: "",
		},
		{
			name:     "label selector set",
			cr:       makeTestArgoCD(withAgentEnabled(true), withAgentLabelSelector("argocd-agent=true")),
			expected: "argocd-agent=true",
		},
		{
			name:     "empty label selector",
			cr:       makeTestArgoCD(withAgentEnabled(true), withAgentLabelSelector("")),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, getAgentLabelSelector(tt.cr))
		})
	}
}

func TestBuildAgentContainerEnv_LabelSelector(t *testing.T) {
	tests := []struct {
		name     string
		cr       *argoproj.ArgoCD
		expected string
	}{
		{
			name:     "default empty label selector",
			cr:       makeTestArgoCD(withAgentEnabled(true)),
			expected: "",
		},
		{
			name:     "custom label selector",
			cr:       makeTestArgoCD(withAgentEnabled(true), withAgentLabelSelector("argocd-agent=true")),
			expected: "argocd-agent=true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envVars := buildAgentContainerEnv(tt.cr)
			envMap := make(map[string]string)
			for _, e := range envVars {
				envMap[e.Name] = e.Value
			}
			assert.Equal(t, tt.expected, envMap[EnvArgoCDAgentLabelSelector],
				"ARGOCD_AGENT_LABEL_SELECTOR mismatch")
		})
	}
}
