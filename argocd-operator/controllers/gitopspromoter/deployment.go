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

// Package gitopspromoter handles reconcilation related to the GitOps Promoter
package gitopspromoter

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	configv1 "github.com/openshift/api/config/v1"

	argoproj "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/argocd-operator/common"
	"github.com/redhat-developer/gitops-operator/argocd-operator/controllers/argoutil"
	tlsProfile "github.com/redhat-developer/gitops-operator/argocd-operator/pkg/tlsprofile"
)

// Useful constants for the Promoter's Deployment
const (
	// EnvGitOpsPromoterImage is the environment variable that controls the default image used for the GitOps Promoter if not
	// specified in the CR
	EnvGitOpsPromoterImage = "GITOPS_PROMOTER_IMAGE"
	// binaryAPIServerCmd is the command for the controller in the GitOps Promoter's binary
	binaryControllerCmd = "controller"
	// binaryAPIServerCmd is the command for the API Server in the GitOps Promoter's binary
	binaryAPIServerCmd     = "apiserver"
	apiServerTLSVolumeName = "serving-cert"
)

// deploymentReconciler represents the functions to fill in spots in the deployment spec that differ between the components
type deploymentConfig struct {
	command         []string
	args            []string
	securityContext *corev1.SecurityContext
	ports           []corev1.ContainerPort
	livenessProbe   *corev1.Probe
	readinessProbe  *corev1.Probe
	startupProbe    *corev1.Probe
	volumes         []corev1.Volume
	volumeMounts    []corev1.VolumeMount
}

// createControllerConfig creates the deploymentConfig that is to be used with the Controller
func createControllerConfig(cr *argoproj.ArgoCD, centralTLSProfile tlsProfile.TLSConfigProfile) deploymentConfig {
	return deploymentConfig{
		command:         buildContainerCommand(binaryControllerCmd),
		args:            buildControllerArgs(cr, centralTLSProfile),
		securityContext: buildControllerSecurityContext(),
		livenessProbe:   buildControllerLivenessProbe(),
		readinessProbe:  buildControllerReadinessProbe(),
		volumes:         buildControllerVolumes(),
		volumeMounts:    buildControllerVolumeMounts(),
	}
}

// createAPIServerConfig creates the deploymentConfig that is to be used with the API Server
func createAPIServerConfig(cr *argoproj.ArgoCD) deploymentConfig {
	cfg := deploymentConfig{
		command:         buildContainerCommand(binaryAPIServerCmd),
		args:            buildAPIServerArgs(),
		securityContext: buildAPIServerSecurityContext(),
		ports:           buildAPIServerPorts(),
		livenessProbe:   buildAPIServerLivenessProbe(),
		readinessProbe:  buildAPIServerReadinessProbe(),
		volumes:         buildAPIServerVolumes(cr),
		volumeMounts:    buildAPIServerVolumeMounts(),
	}
	if usesBuiltInAPIServerCerts(cr) {
		// Built-in certs start empty; give the controller time to issue them before
		// liveness probing takes over (mirrors upstream certs-builtin patch).
		cfg.startupProbe = buildAPIServerStartupProbe()
	}
	return cfg
}

// ReconcilePromoterControllerDeployment reconciles the Promoter's Controller Deployment
// Calls the generic ReconcilePromoterDeployment function with the settings for the Controller
func ReconcilePromoterControllerDeployment(client client.Client, compName string, sa *corev1.ServiceAccount, cr *argoproj.ArgoCD, scheme *runtime.Scheme, centralTLSProfile tlsProfile.TLSConfigProfile) (*appsv1.Deployment, error) {
	cfg := createControllerConfig(cr, centralTLSProfile)
	deployment, err := ReconcilePromoterDeployment(client, compName, sa, cr, scheme, cfg, true)
	if err != nil {
		return nil, err
	}
	return deployment, nil
}

// ReconcilePromoterAPIServerDeployment reconciles the Promoter's API Server Deployment
// Calls the generic ReconcilePromoterDeployment function with the settings for the API Server
func ReconcilePromoterAPIServerDeployment(client client.Client, compName string, sa *corev1.ServiceAccount, cr *argoproj.ArgoCD, scheme *runtime.Scheme) (*appsv1.Deployment, error) {
	cfg := createAPIServerConfig(cr)
	enabled := isAPIServerEnabled(cr)

	deployment, err := ReconcilePromoterDeployment(client, compName, sa, cr, scheme, cfg, enabled)
	if err != nil {
		return nil, err
	}
	return deployment, nil
}

// ReconcilePromoterDeployment is a generic reconcilation function for Promoter Deployments. Handles creation, updating, and deletion.
// Specific settings are determined by the provided deploymentConfig
func ReconcilePromoterDeployment(client client.Client, compName string, sa *corev1.ServiceAccount, cr *argoproj.ArgoCD, scheme *runtime.Scheme, cfg deploymentConfig, enabled bool) (*appsv1.Deployment, error) {
	deployment := buildDeployment(cr, compName)

	allowed := argoutil.IsNamespaceClusterConfigNamespace(cr.Namespace)

	exists := true
	if err := argoutil.FetchObject(client, deployment.Namespace, deployment.Name, deployment); err != nil {
		if !errors.IsNotFound(err) {
			return nil, err
		}
		exists = false
	}

	if exists {
		if !cr.Spec.Promoter.IsEnabled() || !enabled || !allowed {
			argoutil.LogResourceDeletion(log, deployment, "promoter deployment is being deleted due to being disabled")
			if err := client.Delete(context.Background(), deployment); err != nil {
				return nil, fmt.Errorf("failed to delete deployment %s: %v", deployment.Name, err)
			}
			return deployment, nil
		}

		changed := false
		if !reflect.DeepEqual(deployment.Spec.Selector, buildSelector(compName, cr)) {
			deployment.Spec.Selector = buildSelector(compName, cr)
			changed = true
		}

		if !reflect.DeepEqual(deployment.Spec.Template.Spec.Containers[0].Command, cfg.command) {
			deployment.Spec.Template.Spec.Containers[0].Command = cfg.command
			changed = true
		}

		actualArgs := sliceEmptyToNil(deployment.Spec.Template.Spec.Containers[0].Args)
		desiredArgs := sliceEmptyToNil(cfg.args)
		if !reflect.DeepEqual(actualArgs, desiredArgs) {
			deployment.Spec.Template.Spec.Containers[0].Args = cfg.args
			changed = true
		}

		if !reflect.DeepEqual(deployment.Spec.Template.Spec.Containers[0].Image, selectImage(cr)) {
			deployment.Spec.Template.Spec.Containers[0].Image = selectImage(cr)
			changed = true
		}

		if !reflect.DeepEqual(deployment.Spec.Template.Spec.Containers[0].ImagePullPolicy, argoutil.GetImagePullPolicy(cr.Spec.ImagePullPolicy)) {
			deployment.Spec.Template.Spec.Containers[0].ImagePullPolicy = argoutil.GetImagePullPolicy(cr.Spec.ImagePullPolicy)
			changed = true
		}

		if !reflect.DeepEqual(deployment.Spec.Template.Spec.Containers[0].Name, generatePromoterResourceName(compName, cr)) {
			deployment.Spec.Template.Spec.Containers[0].Name = generatePromoterResourceName(compName, cr)
			changed = true
		}

		actualEnv := sliceEmptyToNil(deployment.Spec.Template.Spec.Containers[0].Env)
		desiredEnv := sliceEmptyToNil(cr.Spec.Promoter.Env)
		if !reflect.DeepEqual(actualEnv, desiredEnv) {
			deployment.Spec.Template.Spec.Containers[0].Env = cr.Spec.Promoter.Env
			changed = true
		}

		if !reflect.DeepEqual(deployment.Spec.Template.Spec.Containers[0].SecurityContext, cfg.securityContext) {
			deployment.Spec.Template.Spec.Containers[0].SecurityContext = cfg.securityContext
			changed = true
		}

		if !reflect.DeepEqual(deployment.Spec.Template.Spec.Containers[0].Ports, cfg.ports) {
			deployment.Spec.Template.Spec.Containers[0].Ports = cfg.ports
			changed = true
		}

		if !reflect.DeepEqual(deployment.Spec.Template.Spec.Containers[0].Resources, getResources(cr)) {
			deployment.Spec.Template.Spec.Containers[0].Resources = getResources(cr)
			changed = true
		}

		if !reflect.DeepEqual(deployment.Spec.Template.Spec.Containers[0].LivenessProbe, cfg.livenessProbe) {
			deployment.Spec.Template.Spec.Containers[0].LivenessProbe = cfg.livenessProbe
			changed = true
		}

		if !reflect.DeepEqual(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe, cfg.readinessProbe) {
			deployment.Spec.Template.Spec.Containers[0].ReadinessProbe = cfg.readinessProbe
			changed = true
		}

		if !reflect.DeepEqual(deployment.Spec.Template.Spec.Containers[0].StartupProbe, cfg.startupProbe) {
			deployment.Spec.Template.Spec.Containers[0].StartupProbe = cfg.startupProbe
			changed = true
		}

		if !reflect.DeepEqual(deployment.Spec.Template.Spec.ServiceAccountName, sa.Name) {
			deployment.Spec.Template.Spec.ServiceAccountName = sa.Name
			changed = true
		}

		if !reflect.DeepEqual(deployment.Spec.Template.Spec.TerminationGracePeriodSeconds, ptr.To(int64(10))) {
			deployment.Spec.Template.Spec.TerminationGracePeriodSeconds = ptr.To(int64(10))
			changed = true
		}

		actualVolumes := sliceEmptyToNil(deployment.Spec.Template.Spec.Volumes)
		desiredVolumes := sliceEmptyToNil(cfg.volumes)
		if !reflect.DeepEqual(actualVolumes, desiredVolumes) {
			deployment.Spec.Template.Spec.Volumes = cfg.volumes
			changed = true
		}

		actualVolumeMounts := sliceEmptyToNil(deployment.Spec.Template.Spec.Containers[0].VolumeMounts)
		desiredVolumeMounts := sliceEmptyToNil(cfg.volumeMounts)
		if !reflect.DeepEqual(actualVolumeMounts, desiredVolumeMounts) {
			deployment.Spec.Template.Spec.Containers[0].VolumeMounts = cfg.volumeMounts
			changed = true
		}

		if changed {
			argoutil.LogResourceUpdate(log, deployment)
			if err := client.Update(context.Background(), deployment); err != nil {
				return nil, err
			}
			return deployment, nil
		}
		return deployment, nil
	}

	if !cr.Spec.Promoter.IsEnabled() || !enabled || !allowed {
		return deployment, nil
	}
	deployment.Spec = buildDeploymentSpec(compName, sa, cr, cfg)
	if err := controllerutil.SetControllerReference(cr, deployment, scheme); err != nil {
		return nil, fmt.Errorf("failed to set argocd cr %s as owner for deployment %s: %v", cr.Name, sa.Name, err)
	}

	argoutil.LogResourceCreation(log, deployment)
	if err := client.Create(context.Background(), deployment); err != nil {
		return nil, fmt.Errorf("failed to create deployment %s: %v", deployment.Name, err)
	}
	return deployment, nil
}

// buildDeployment creates a Deployment object with meta data
func buildDeployment(cr *argoproj.ArgoCD, compName string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generatePromoterResourceName(compName, cr),
			Namespace: cr.Namespace,
			Labels:    buildLabelsForPromoterResources(compName, cr),
		},
	}
}

// buildDeploymentSpec builds the Deployment's spec based on the config provided and the CR
func buildDeploymentSpec(compName string, sa *corev1.ServiceAccount, cr *argoproj.ArgoCD, cfg deploymentConfig) appsv1.DeploymentSpec {
	return appsv1.DeploymentSpec{
		Selector: buildSelector(compName, cr),
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: buildLabelsForPromoterResources(compName, cr),
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Command:         cfg.command,
						Image:           selectImage(cr),
						ImagePullPolicy: argoutil.GetImagePullPolicy(cr.Spec.ImagePullPolicy),
						Name:            generatePromoterResourceName(compName, cr),
						Args:            cfg.args,
						Env:             cr.Spec.Promoter.Env,
						SecurityContext: cfg.securityContext,
						Resources:       getResources(cr),
						Ports:           cfg.ports,
						LivenessProbe:   cfg.livenessProbe,
						ReadinessProbe:  cfg.readinessProbe,
						StartupProbe:    cfg.startupProbe,
						VolumeMounts:    cfg.volumeMounts,
					},
				},
				ServiceAccountName:            sa.Name,
				TerminationGracePeriodSeconds: ptr.To(int64(10)),
				Volumes:                       cfg.volumes,
			},
		},
	}
}

// buildSelector builds the selector for the Deployment
func buildSelector(compName string, cr *argoproj.ArgoCD) *metav1.LabelSelector {
	return &metav1.LabelSelector{
		MatchLabels: buildLabelsForPromoterResources(compName, cr),
	}
}

func buildContainerCommand(cmd string) []string {
	return []string{"/usr/bin/tini", "--", "/gitops-promoter", cmd}
}

// selectImage selects the image to be used for the Promoter based on the following priority
// CR's .Spec.Promoter.Image field -> GITOPS_PROMOTER_IMAGE env variable -> Default Image on argoproj-labs quay repository
func selectImage(cr *argoproj.ArgoCD) string {
	if cr.Spec.Promoter != nil && cr.Spec.Promoter.Image != "" {
		return cr.Spec.Promoter.Image
	}

	if image := os.Getenv(EnvGitOpsPromoterImage); image != "" {
		return image
	}

	return common.GitOpsPromoterDefaultImageName
}

// buildControllerSecurityContext builds the SecurityContext for the Controller's container within its Deployment
func buildControllerSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		RunAsNonRoot:             ptr.To(true),
		AllowPrivilegeEscalation: ptr.To(false),
		ReadOnlyRootFilesystem:   ptr.To(true),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}
}

// buildAPIServerSecurityContext builds the SecurityContext for the API Server's container within its Deployment
func buildAPIServerSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		RunAsNonRoot:             ptr.To(true),
		AllowPrivilegeEscalation: ptr.To(false),
		ReadOnlyRootFilesystem:   ptr.To(true),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}
}

// getResources gets the resources value to be used for the Deployment's container
func getResources(cr *argoproj.ArgoCD) corev1.ResourceRequirements {
	resources := corev1.ResourceRequirements{}
	if cr.Spec.Promoter != nil && cr.Spec.Promoter.Resources != nil {
		resources = *cr.Spec.Promoter.Resources
	}
	return resources
}

// buildControllerReadinessProbe builds the LivenessProbe for the Controller's container within its Deployment
func buildControllerLivenessProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Scheme: corev1.URISchemeHTTP,
				Path:   "/healthz",
				Port:   intstr.FromInt(9081),
			},
		},
		TimeoutSeconds:      1,
		SuccessThreshold:    1,
		FailureThreshold:    3,
		InitialDelaySeconds: 15,
		PeriodSeconds:       20,
	}
}

// buildControllerReadinessProbe builds the ReadinessProbe for the Controller's container within its Deployment
func buildControllerReadinessProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Scheme: corev1.URISchemeHTTP,
				Path:   "/readyz",
				Port:   intstr.FromInt(9081),
			},
		},
		TimeoutSeconds:      1,
		SuccessThreshold:    1,
		FailureThreshold:    3,
		InitialDelaySeconds: 5,
		PeriodSeconds:       10,
	}
}

// buildAPIServerPorts builds the Port for the API Server's container within its Deployment
func buildAPIServerPorts() []corev1.ContainerPort {
	return []corev1.ContainerPort{
		{
			Name:          APIServerPortName,
			ContainerPort: 6443,
			Protocol:      corev1.ProtocolTCP,
		},
	}
}

// buildAPIServerLivenessProbe builds the LivenessProbe for the API Server's container within its Deployment
func buildAPIServerLivenessProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:   "/livez",
				Port:   intstr.FromString("https"),
				Scheme: corev1.URISchemeHTTPS,
			},
		},
		TimeoutSeconds:      1,
		SuccessThreshold:    1,
		FailureThreshold:    3,
		InitialDelaySeconds: 15,
		PeriodSeconds:       20,
	}
}

// buildAPIServerReadinessProbe builds the ReadinessProbe for the API Server's container within its Deployment
func buildAPIServerReadinessProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:   "/readyz",
				Port:   intstr.FromString("https"),
				Scheme: corev1.URISchemeHTTPS,
			},
		},
		TimeoutSeconds:      1,
		SuccessThreshold:    1,
		FailureThreshold:    3,
		InitialDelaySeconds: 5,
		PeriodSeconds:       10,
	}
}

// buildAPIServerStartupProbe waits for built-in serving certs to be issued before liveness.
func buildAPIServerStartupProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:   "/livez",
				Port:   intstr.FromString("https"),
				Scheme: corev1.URISchemeHTTPS,
			},
		},
		PeriodSeconds:    5,
		FailureThreshold: 60,
	}
}

// buildControllerArgs builds the Args for the Controller's container within its Deployment,
// including TLS settings derived from the OpenShift cluster TLS security profile and optional
// built-in apiserver cert-rotation flags.
func buildControllerArgs(cr *argoproj.ArgoCD, centralTLSProfile tlsProfile.TLSConfigProfile) []string {
	args := BuildPromoterTLSArgsFromClusterTLSProfile(centralTLSProfile)
	if usesBuiltInAPIServerCerts(cr) && isAPIServerEnabled(cr) {
		args = append(args,
			"--apiserver-cert-rotation",
			"--apiserver-cert-rotation-secret-name="+DefaultAPIServerServingCertSecretName,
			"--apiserver-cert-rotation-service-name="+generatePromoterResourceName(string(argoproj.PromoterComponentTypeAPIServer), cr),
			"--apiserver-cert-rotation-apiservice-name="+DefaultAPIServerAPIServiceName,
		)
	}
	return args
}

// BuildPromoterTLSArgsFromClusterTLSProfile converts the cluster TLS profile into
// gitops-promoter controller CLI flags (--tls-min-version, --tls-cipher-suites,
// --tls-curve-preferences). Empty/disabled profiles produce no args so promoter
// keeps Go defaults.
func BuildPromoterTLSArgsFromClusterTLSProfile(centralTLSProfile tlsProfile.TLSConfigProfile) []string {
	if centralTLSProfile.DisableClusterTLSProfile {
		return nil
	}

	var args []string

	// Promoter accepts Kubernetes-style version names and rejects versions below TLS 1.2.
	switch centralTLSProfile.MinVersion {
	case configv1.VersionTLS12, configv1.VersionTLS13:
		args = append(args, "--tls-min-version="+string(centralTLSProfile.MinVersion))
	}

	if ciphers := argoutil.MapCipherSuites(centralTLSProfile.Ciphers); len(ciphers) > 0 {
		// Go's tls.Config.CipherSuites only applies to TLS 1.2 and below, and
		// promoter rejects TLS 1.3 suite names. Omit ciphers when min version is
		// TLS 1.3; otherwise drop TLS 1.3-only suites from the list.
		if centralTLSProfile.MinVersion != configv1.VersionTLS13 {
			tls13Ciphers := map[string]bool{
				"TLS_AES_128_GCM_SHA256":       true,
				"TLS_AES_256_GCM_SHA384":       true,
				"TLS_CHACHA20_POLY1305_SHA256": true,
			}
			filtered := make([]string, 0, len(ciphers))
			for _, cipher := range ciphers {
				if !tls13Ciphers[cipher] {
					filtered = append(filtered, cipher)
				}
			}
			if len(filtered) > 0 {
				args = append(args, "--tls-cipher-suites="+strings.Join(filtered, ","))
			}
		}
	}

	curves, err := argoutil.MapOpenShiftTLSGroupsToGoCurveIDs(centralTLSProfile.CurvePreferences, "gitops-promoter")
	if err != nil {
		// Do not fail reconciliation for incompatible curve preferences. Keep min
		// version/ciphers and fall back to promoter's default curve selection.
		log.Info("skipping TLS curve preferences; falling back to component defaults",
			"component", "gitops-promoter", "error", err.Error())
	} else if curves != "" {
		args = append(args, "--tls-curve-preferences="+curves)
	}

	return args
}

// buildAPIServerArgs builds the Args for the API Server's container within its Deployment
// #TODO: Allow for custom args through CR if there are worth it settings in the Promoter
func buildAPIServerArgs() []string {
	return []string{
		"--secure-port=6443",
		"--tls-cert-file=/serving-certs/tls.crt",
		"--tls-private-key-file=/serving-certs/tls.key",
	}
}

// buildControllerVolumes provides writable storage for Git clones and temporary index files.
func buildControllerVolumes() []corev1.Volume {
	return []corev1.Volume{
		{
			Name: "tmp",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}
}

// buildControllerVolumeMounts mounts the controller's writable temporary storage.
func buildControllerVolumeMounts() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{
			Name:      "tmp",
			MountPath: "/tmp",
		},
	}
}

// buildAPIServerVolumes builds the Volumes for the API Server
func buildAPIServerVolumes(cr *argoproj.ArgoCD) []corev1.Volume {
	secretVolume := &corev1.SecretVolumeSource{
		SecretName:  getAPIServerServingCertSecretName(cr),
		DefaultMode: ptr.To(int32(420)),
	}
	if usesBuiltInAPIServerCerts(cr) {
		// Secret starts empty; project only the serving key pair and mark optional so the
		// pod can schedule before the controller issues the cert. CA private key stays out.
		secretVolume.Optional = ptr.To(true)
		secretVolume.Items = []corev1.KeyToPath{
			{Key: "tls.crt", Path: "tls.crt"},
			{Key: "tls.key", Path: "tls.key"},
		}
	}

	return []corev1.Volume{
		{
			Name: apiServerTLSVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: secretVolume,
			},
		},
		{
			Name: "tmp",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}
}

// buildAPIServerVolumeMounts builds the VolumeMounts for the API Server
func buildAPIServerVolumeMounts() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{
			Name:      apiServerTLSVolumeName,
			MountPath: "/serving-certs",
			ReadOnly:  true,
		},
		{
			Name:      "tmp",
			MountPath: "/tmp",
		},
	}
}

// sliceEmptyToNil returns nil if the slice is empty for more easy comparisons in updated checks
func sliceEmptyToNil[T any](s []T) []T {
	if len(s) == 0 {
		return nil
	}
	return s
}
