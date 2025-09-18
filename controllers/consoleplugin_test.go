package controllers

import (
	"context"
	"maps"
	"testing"

	argocommon "github.com/argoproj-labs/argocd-operator/common"
	consolev1 "github.com/openshift/api/console/v1"
	pipelinesv1alpha1 "github.com/redhat-developer/gitops-operator/api/v1alpha1"
	"github.com/redhat-developer/gitops-operator/common"
	"gotest.tools/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestPlugin(t *testing.T) {
	testConsolePlugin := consolePlugin()

	testDisplayName := displayName
	assert.Equal(t, testConsolePlugin.Spec.DisplayName, testDisplayName)

	testPluginService := &consolev1.ConsolePluginService{
		Name:      gitopsPluginName,
		Namespace: serviceNamespace,
		Port:      servicePort,
		BasePath:  "/",
	}
	assert.DeepEqual(t, testConsolePlugin.Spec.I18n.LoadType, consolev1.Preload)
	assert.DeepEqual(t, testConsolePlugin.Spec.Backend.Type, consolev1.Service)
	assert.DeepEqual(t, testConsolePlugin.Spec.Backend.Service, testPluginService)
}

func TestPlugin_reconcileDeployment_changedLabels(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
	}{
		{
			name: "default labels",
			labels: map[string]string{
				kubeAppLabelApp:              gitopsPluginName,
				kubeAppLabelComponent:        gitopsPluginName,
				kubeAppLabelInstance:         gitopsPluginName,
				kubeAppLabelPartOf:           gitopsPluginName,
				kubeAppLabelRuntimeNamespace: serviceNamespace,
			},
		},
		{
			name: "changed labels",
			labels: map[string]string{
				kubeAppLabelApp:              "wrong name",
				kubeAppLabelComponent:        "wrong name",
				kubeAppLabelInstance:         "wrong name",
				kubeAppLabelPartOf:           "wrong name",
				kubeAppLabelRuntimeNamespace: "wrong namespace",
			},
		},
	}

	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(newGitopsService()).Build()
	reconciler := newReconcileGitOpsService(fakeClient, s)
	instance := &pipelinesv1alpha1.GitopsService{}
	var replicas int32 = 1

	for x, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      gitopsPluginName,
					Namespace: serviceNamespace,
					Labels:    test.labels,
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							kubeAppLabelName: gitopsPluginName,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								kubeAppLabelApp: gitopsPluginName,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:            gitopsPluginName,
									Image:           "fake-image-repo-rul",
									ImagePullPolicy: corev1.PullAlways,
									Ports: []corev1.ContainerPort{
										{
											Name:          "http",
											Protocol:      corev1.ProtocolTCP,
											ContainerPort: servicePort,
										},
									},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      pluginServingCertName,
											ReadOnly:  true,
											MountPath: "/etc/httpd-ssl/certs/tls.crt",
											SubPath:   "tls.crt",
										},
										{
											Name:      pluginServingCertName,
											ReadOnly:  true,
											MountPath: "/etc/httpd-ssl/private/tls.key",
											SubPath:   "tls.key",
										},
										{
											Name:      httpdConfigMapName,
											ReadOnly:  true,
											MountPath: "/etc/httpd-cfg/httpd.conf",
											SubPath:   "httpd.conf",
										},
									},
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceMemory: resourcev1.MustParse("128Mi"),
											corev1.ResourceCPU:    resourcev1.MustParse("250m"),
										},
										Limits: corev1.ResourceList{
											corev1.ResourceMemory: resourcev1.MustParse("256Mi"),
											corev1.ResourceCPU:    resourcev1.MustParse("500m"),
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: pluginServingCertName,
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName:  pluginServingCertName,
											DefaultMode: ptr.To(int32(420)),
										},
									},
								},
								{
									Name: pluginServingCertName,
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: httpdConfigMapName,
											},
											DefaultMode: ptr.To(int32(420)),
										},
									},
								},
							},
							RestartPolicy: corev1.RestartPolicyAlways,
							DNSPolicy:     corev1.DNSClusterFirst,
						},
					},
				},
			}

			if x == 0 {
				err := reconciler.Client.Create(context.TODO(), d)
				assert.NilError(t, err)
			}

			_, err := reconciler.reconcileConsolePlugin(instance, newRequest(serviceNamespace, gitopsPluginName))
			assertNoError(t, err)

			deployment := &appsv1.Deployment{}
			err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName, Namespace: serviceNamespace}, deployment)
			assertNoError(t, err)

			assert.Equal(t, deployment.Labels[kubeAppLabelApp], "gitops-plugin")
			assert.Equal(t, deployment.Labels[kubeAppLabelComponent], "gitops-plugin")
			assert.Equal(t, deployment.Labels[kubeAppLabelInstance], "gitops-plugin")
			assert.Equal(t, deployment.Labels[kubeAppLabelPartOf], "gitops-plugin")
			assert.Equal(t, deployment.Labels[kubeAppLabelRuntimeNamespace], "openshift-gitops")
		})
	}
}

func TestPlugin_reconcileDeployment_changedReplicas(t *testing.T) {
	var replicas int32 = 1
	var wrongReplicas int32 = 2
	tests := []struct {
		name     string
		replicas int32
	}{
		{
			name:     "default replicas",
			replicas: replicas,
		},
		{
			name:     "changed replicas",
			replicas: wrongReplicas,
		},
	}

	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(newGitopsService()).Build()
	reconciler := newReconcileGitOpsService(fakeClient, s)
	instance := &pipelinesv1alpha1.GitopsService{}

	for x, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      gitopsPluginName,
					Namespace: serviceNamespace,
					Labels: map[string]string{
						kubeAppLabelApp:              gitopsPluginName,
						kubeAppLabelComponent:        gitopsPluginName,
						kubeAppLabelInstance:         gitopsPluginName,
						kubeAppLabelPartOf:           gitopsPluginName,
						kubeAppLabelRuntimeNamespace: serviceNamespace,
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &test.replicas,
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							kubeAppLabelName: gitopsPluginName,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								kubeAppLabelApp: gitopsPluginName,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:            gitopsPluginName,
									Image:           "fake-image-repo-rul",
									ImagePullPolicy: corev1.PullAlways,
									Ports: []corev1.ContainerPort{
										{
											Name:          "http",
											Protocol:      corev1.ProtocolTCP,
											ContainerPort: servicePort,
										},
									},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      pluginServingCertName,
											ReadOnly:  true,
											MountPath: "/etc/httpd-ssl/certs/tls.crt",
											SubPath:   "tls.crt",
										},
										{
											Name:      pluginServingCertName,
											ReadOnly:  true,
											MountPath: "/etc/httpd-ssl/private/tls.key",
											SubPath:   "tls.key",
										},
										{
											Name:      httpdConfigMapName,
											ReadOnly:  true,
											MountPath: "/etc/httpd-cfg/httpd.conf",
											SubPath:   "httpd.conf",
										},
									},
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceMemory: resourcev1.MustParse("128Mi"),
											corev1.ResourceCPU:    resourcev1.MustParse("250m"),
										},
										Limits: corev1.ResourceList{
											corev1.ResourceMemory: resourcev1.MustParse("256Mi"),
											corev1.ResourceCPU:    resourcev1.MustParse("500m"),
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: pluginServingCertName,
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName:  pluginServingCertName,
											DefaultMode: ptr.To(int32(420)),
										},
									},
								},
								{
									Name: pluginServingCertName,
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: httpdConfigMapName,
											},
											DefaultMode: ptr.To(int32(420)),
										},
									},
								},
							},
							RestartPolicy: corev1.RestartPolicyAlways,
							DNSPolicy:     corev1.DNSClusterFirst,
						},
					},
				},
			}
			if x == 0 {
				err := reconciler.Client.Create(context.TODO(), d)
				assertNoError(t, err)
			}

			_, err := reconciler.reconcileConsolePlugin(instance, newRequest(serviceNamespace, gitopsPluginName))
			assertNoError(t, err)

			deployment := &appsv1.Deployment{}
			err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName, Namespace: serviceNamespace}, deployment)
			assertNoError(t, err)
			assert.Equal(t, *deployment.Spec.Replicas, replicas)
		})
	}
}

func TestPlugin_reconcileDeployment_changedSelector(t *testing.T) {
	var replicas int32 = 1
	tests := []struct {
		name                string
		selectorMatchLabels map[string]string
	}{
		{
			name: "default selector",
			selectorMatchLabels: map[string]string{
				kubeAppLabelName: gitopsPluginName,
			},
		},
		{
			name: "changed selector",
			selectorMatchLabels: map[string]string{
				kubeAppLabelName: "wrong name",
			},
		},
	}

	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(newGitopsService()).Build()
	reconciler := newReconcileGitOpsService(fakeClient, s)
	instance := &pipelinesv1alpha1.GitopsService{}

	for x, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      gitopsPluginName,
					Namespace: serviceNamespace,
					Labels: map[string]string{
						kubeAppLabelApp:              gitopsPluginName,
						kubeAppLabelComponent:        gitopsPluginName,
						kubeAppLabelInstance:         gitopsPluginName,
						kubeAppLabelPartOf:           gitopsPluginName,
						kubeAppLabelRuntimeNamespace: serviceNamespace,
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{
						MatchLabels: test.selectorMatchLabels,
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								kubeAppLabelApp: gitopsPluginName,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:            gitopsPluginName,
									Image:           "fake-image-repo-rul",
									ImagePullPolicy: corev1.PullAlways,
									Ports: []corev1.ContainerPort{
										{
											Name:          "http",
											Protocol:      corev1.ProtocolTCP,
											ContainerPort: servicePort,
										},
									},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      pluginServingCertName,
											ReadOnly:  true,
											MountPath: "/etc/httpd-ssl/certs/tls.crt",
											SubPath:   "tls.crt",
										},
										{
											Name:      pluginServingCertName,
											ReadOnly:  true,
											MountPath: "/etc/httpd-ssl/private/tls.key",
											SubPath:   "tls.key",
										},
										{
											Name:      httpdConfigMapName,
											ReadOnly:  true,
											MountPath: "/etc/httpd-cfg/httpd.conf",
											SubPath:   "httpd.conf",
										},
									},
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceMemory: resourcev1.MustParse("128Mi"),
											corev1.ResourceCPU:    resourcev1.MustParse("250m"),
										},
										Limits: corev1.ResourceList{
											corev1.ResourceMemory: resourcev1.MustParse("256Mi"),
											corev1.ResourceCPU:    resourcev1.MustParse("500m"),
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: pluginServingCertName,
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName:  pluginServingCertName,
											DefaultMode: ptr.To(int32(420)),
										},
									},
								},
								{
									Name: pluginServingCertName,
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: httpdConfigMapName,
											},
											DefaultMode: ptr.To(int32(420)),
										},
									},
								},
							},
							RestartPolicy: corev1.RestartPolicyAlways,
							DNSPolicy:     corev1.DNSClusterFirst,
						},
					},
				},
			}

			if x == 0 {
				err := reconciler.Client.Create(context.TODO(), d)
				assertNoError(t, err)
			}

			_, err := reconciler.reconcileConsolePlugin(instance, newRequest(serviceNamespace, gitopsPluginName))
			assertNoError(t, err)

			deployment := &appsv1.Deployment{}
			err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName, Namespace: serviceNamespace}, deployment)
			assertNoError(t, err)
			assert.Equal(t, deployment.Spec.Selector.MatchLabels[kubeAppLabelName], "gitops-plugin")
		})
	}
}

func TestPlugin_reconcileDeployment_changedTemplateLabels(t *testing.T) {
	tests := []struct {
		name           string
		templateLabels map[string]string
	}{
		{
			name: "default template labels",
			templateLabels: map[string]string{
				kubeAppLabelApp: gitopsPluginName,
			},
		},
		{
			name: "changed template labels",
			templateLabels: map[string]string{
				kubeAppLabelApp: "wrong name",
			},
		},
	}

	s := scheme.Scheme
	addKnownTypesToScheme(s)

	instance := &pipelinesv1alpha1.GitopsService{}
	var replicas int32 = 1

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      gitopsPluginName,
					Namespace: serviceNamespace,
					Labels: map[string]string{
						kubeAppLabelApp:              gitopsPluginName,
						kubeAppLabelComponent:        gitopsPluginName,
						kubeAppLabelInstance:         gitopsPluginName,
						kubeAppLabelPartOf:           gitopsPluginName,
						kubeAppLabelRuntimeNamespace: serviceNamespace,
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							kubeAppLabelName: gitopsPluginName,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: test.templateLabels,
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:            gitopsPluginName,
									Image:           "fake-image-repo-rul",
									ImagePullPolicy: corev1.PullAlways,
									Ports: []corev1.ContainerPort{
										{
											Name:          "http",
											Protocol:      corev1.ProtocolTCP,
											ContainerPort: servicePort,
										},
									},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      pluginServingCertName,
											ReadOnly:  true,
											MountPath: "/etc/httpd-ssl/certs/tls.crt",
											SubPath:   "tls.crt",
										},
										{
											Name:      pluginServingCertName,
											ReadOnly:  true,
											MountPath: "/etc/httpd-ssl/private/tls.key",
											SubPath:   "tls.key",
										},
										{
											Name:      httpdConfigMapName,
											ReadOnly:  true,
											MountPath: "/etc/httpd-cfg/httpd.conf",
											SubPath:   "httpd.conf",
										},
									},
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceMemory: resourcev1.MustParse("128Mi"),
											corev1.ResourceCPU:    resourcev1.MustParse("250m"),
										},
										Limits: corev1.ResourceList{
											corev1.ResourceMemory: resourcev1.MustParse("256Mi"),
											corev1.ResourceCPU:    resourcev1.MustParse("500m"),
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: pluginServingCertName,
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName:  pluginServingCertName,
											DefaultMode: ptr.To(int32(420)),
										},
									},
								},
								{
									Name: pluginServingCertName,
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: httpdConfigMapName,
											},
											DefaultMode: ptr.To(int32(420)),
										},
									},
								},
							},
							RestartPolicy: corev1.RestartPolicyAlways,
							DNSPolicy:     corev1.DNSClusterFirst,
						},
					},
				},
			}

			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(newGitopsService(), d).Build()
			reconciler := newReconcileGitOpsService(fakeClient, s)

			_, err := reconciler.reconcileDeployment(instance, newRequest(serviceNamespace, gitopsPluginName))
			assertNoError(t, err)

			deployment := &appsv1.Deployment{}
			err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName, Namespace: serviceNamespace}, deployment)
			assertNoError(t, err)

			assert.Equal(t, deployment.Spec.Template.Labels[kubeAppLabelApp], "gitops-plugin")
		})
	}
}

func TestPlugin_reconcileDeployment_changedContainers(t *testing.T) {
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(newGitopsService()).Build()
	reconciler := newReconcileGitOpsService(fakeClient, s)
	instance := &pipelinesv1alpha1.GitopsService{}

	assertPluginDeploymentSpec := func(t *testing.T, deployment *appsv1.Deployment) {
		t.Helper()

		consoleImage := common.DefaultConsoleImage + ":" + common.DefaultConsoleVersion
		assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].Name, "gitops-plugin")
		assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].Image, consoleImage)
		assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].ImagePullPolicy, corev1.PullAlways)
		assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].Ports[0].Name, "http")
		assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].Ports[0].Protocol, corev1.ProtocolTCP)
		assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort, int32(9001))
		assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].VolumeMounts[0].Name, "console-serving-cert")
		assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].VolumeMounts[0].ReadOnly, true)
		assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].VolumeMounts[0].MountPath, "/etc/httpd-ssl/certs/tls.crt")
		assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].VolumeMounts[1].MountPath, "/etc/httpd-ssl/private/tls.key")
		assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].VolumeMounts[2].Name, "httpd-cfg")
		assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].VolumeMounts[1].ReadOnly, true)
		assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].VolumeMounts[2].MountPath, "/etc/httpd-cfg/httpd.conf")
		assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].VolumeMounts[2].SubPath, "httpd.conf")
		assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].Resources.Requests["memory"], resourcev1.MustParse("128Mi"))
		assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].Resources.Requests["cpu"], resourcev1.MustParse("250m"))
		assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].Resources.Limits["memory"], resourcev1.MustParse("256Mi"))
		assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].Resources.Limits["cpu"], resourcev1.MustParse("500m"))
		assert.DeepEqual(t, deployment.Spec.Template.Spec.Containers[0].SecurityContext, securityContextForPlugin())
	}

	_, err := reconciler.reconcileDeployment(instance, newRequest(serviceNamespace, gitopsPluginName))
	assertNoError(t, err)

	// There should be a new console plugin deployment created
	deployment := &appsv1.Deployment{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName, Namespace: serviceNamespace}, deployment)
	assertNoError(t, err)

	assertPluginDeploymentSpec(t, deployment)

	// Update the deployment with new containers
	deployment.Spec.Template.Spec.Containers = []corev1.Container{
		{
			Name:  "wrong name",
			Image: "wrong image",
			SecurityContext: &corev1.SecurityContext{
				Privileged: ptr.To(true),
			},
		},
	}

	err = fakeClient.Update(context.TODO(), deployment)
	assertNoError(t, err)

	// Verify if the containers are reconciled back to the default values
	_, err = reconciler.reconcileDeployment(instance, newRequest(serviceNamespace, gitopsPluginName))
	assertNoError(t, err)

	deployment = &appsv1.Deployment{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName, Namespace: serviceNamespace}, deployment)
	assertNoError(t, err)

	assertPluginDeploymentSpec(t, deployment)
}

func TestPlugin_reconcileDeployment_changedRestartPolicy(t *testing.T) {
	var replicas int32 = 1
	tests := []struct {
		name          string
		restartPolicy corev1.RestartPolicy
	}{
		{
			name:          "default restartPolicy",
			restartPolicy: corev1.RestartPolicyAlways,
		},
		{
			name:          "changed restartPolicy",
			restartPolicy: corev1.RestartPolicyOnFailure,
		},
	}

	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(newGitopsService()).Build()
	reconciler := newReconcileGitOpsService(fakeClient, s)
	instance := &pipelinesv1alpha1.GitopsService{}

	for x, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      gitopsPluginName,
					Namespace: serviceNamespace,
					Labels: map[string]string{
						kubeAppLabelApp:              gitopsPluginName,
						kubeAppLabelComponent:        gitopsPluginName,
						kubeAppLabelInstance:         gitopsPluginName,
						kubeAppLabelPartOf:           gitopsPluginName,
						kubeAppLabelRuntimeNamespace: serviceNamespace,
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							kubeAppLabelName: gitopsPluginName,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								kubeAppLabelApp: gitopsPluginName,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:            gitopsPluginName,
									Image:           "fake-image-repo-rul",
									ImagePullPolicy: corev1.PullAlways,
									Ports: []corev1.ContainerPort{
										{
											Name:          "http",
											Protocol:      corev1.ProtocolTCP,
											ContainerPort: servicePort,
										},
									},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      pluginServingCertName,
											ReadOnly:  true,
											MountPath: "/etc/httpd-ssl/certs/tls.crt",
											SubPath:   "tls.crt",
										},
										{
											Name:      pluginServingCertName,
											ReadOnly:  true,
											MountPath: "/etc/httpd-ssl/private/tls.key",
											SubPath:   "tls.key",
										},
										{
											Name:      httpdConfigMapName,
											ReadOnly:  true,
											MountPath: "/etc/httpd-cfg/httpd.conf",
											SubPath:   "httpd.conf",
										},
									},
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceMemory: resourcev1.MustParse("128Mi"),
											corev1.ResourceCPU:    resourcev1.MustParse("250m"),
										},
										Limits: corev1.ResourceList{
											corev1.ResourceMemory: resourcev1.MustParse("256Mi"),
											corev1.ResourceCPU:    resourcev1.MustParse("500m"),
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: pluginServingCertName,
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName:  pluginServingCertName,
											DefaultMode: ptr.To(int32(420)),
										},
									},
								},
								{
									Name: pluginServingCertName,
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: httpdConfigMapName,
											},
											DefaultMode: ptr.To(int32(420)),
										},
									},
								},
							},
							RestartPolicy: test.restartPolicy,
							DNSPolicy:     corev1.DNSClusterFirst,
						},
					},
				},
			}

			if x == 0 {
				err := reconciler.Client.Create(context.TODO(), d)
				assertNoError(t, err)
			}

			_, err := reconciler.reconcileConsolePlugin(instance, newRequest(serviceNamespace, gitopsPluginName))
			assertNoError(t, err)

			deployment := &appsv1.Deployment{}
			err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName, Namespace: serviceNamespace}, deployment)
			assertNoError(t, err)
			assert.Equal(t, deployment.Spec.Template.Spec.RestartPolicy, corev1.RestartPolicyAlways)
		})
	}
}

func TestPlugin_reconcileDeployment_changedDNSPolicy(t *testing.T) {
	var replicas int32 = 1
	tests := []struct {
		name      string
		dnsPolicy corev1.DNSPolicy
	}{
		{
			name:      "default DNSPolicy",
			dnsPolicy: corev1.DNSClusterFirst,
		},
		{
			name:      "changed DNSPolicy",
			dnsPolicy: corev1.DNSDefault,
		},
	}

	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(newGitopsService()).Build()
	reconciler := newReconcileGitOpsService(fakeClient, s)
	instance := &pipelinesv1alpha1.GitopsService{}

	for x, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      gitopsPluginName,
					Namespace: serviceNamespace,
					Labels: map[string]string{
						kubeAppLabelApp:              gitopsPluginName,
						kubeAppLabelComponent:        gitopsPluginName,
						kubeAppLabelInstance:         gitopsPluginName,
						kubeAppLabelPartOf:           gitopsPluginName,
						kubeAppLabelRuntimeNamespace: serviceNamespace,
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							kubeAppLabelName: gitopsPluginName,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								kubeAppLabelApp: gitopsPluginName,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:            gitopsPluginName,
									Image:           "fake-image-repo-rul",
									ImagePullPolicy: corev1.PullAlways,
									Ports: []corev1.ContainerPort{
										{
											Name:          "http",
											Protocol:      corev1.ProtocolTCP,
											ContainerPort: servicePort,
										},
									},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      pluginServingCertName,
											ReadOnly:  true,
											MountPath: "/etc/httpd-ssl/certs/tls.crt",
											SubPath:   "tls.crt",
										},
										{
											Name:      pluginServingCertName,
											ReadOnly:  true,
											MountPath: "/etc/httpd-ssl/private/tls.key",
											SubPath:   "tls.key",
										},
										{
											Name:      httpdConfigMapName,
											ReadOnly:  true,
											MountPath: "/etc/httpd-cfg/httpd.conf",
											SubPath:   "httpd.conf",
										},
									},
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceMemory: resourcev1.MustParse("128Mi"),
											corev1.ResourceCPU:    resourcev1.MustParse("250m"),
										},
										Limits: corev1.ResourceList{
											corev1.ResourceMemory: resourcev1.MustParse("256Mi"),
											corev1.ResourceCPU:    resourcev1.MustParse("500m"),
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: pluginServingCertName,
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName:  pluginServingCertName,
											DefaultMode: ptr.To(int32(420)),
										},
									},
								},
								{
									Name: pluginServingCertName,
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: httpdConfigMapName,
											},
											DefaultMode: ptr.To(int32(420)),
										},
									},
								},
							},
							RestartPolicy: corev1.RestartPolicyAlways,
							DNSPolicy:     test.dnsPolicy,
						},
					},
				},
			}

			if x == 0 {
				err := reconciler.Client.Create(context.TODO(), d)
				assertNoError(t, err)
			}

			_, err := reconciler.reconcileConsolePlugin(instance, newRequest(serviceNamespace, gitopsPluginName))
			assertNoError(t, err)

			deployment := &appsv1.Deployment{}
			err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName, Namespace: serviceNamespace}, deployment)
			assertNoError(t, err)
			assert.Equal(t, deployment.Spec.Template.Spec.DNSPolicy, corev1.DNSClusterFirst)
		})
	}
}

func TestPlugin_reconcileDeployment_changedInfraNodeSelector(t *testing.T) {

	gitopsService := &pipelinesv1alpha1.GitopsService{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceName,
		},
		Spec: pipelinesv1alpha1.GitopsServiceSpec{
			RunOnInfra:  true,
			Tolerations: deploymentDefaultTolerations(),
		},
	}
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(gitopsService).Build()
	reconciler := newReconcileGitOpsService(fakeClient, s)

	_, err := reconciler.reconcileDeployment(gitopsService, newRequest(serviceNamespace, gitopsPluginName))
	assertNoError(t, err)

	deployment := &appsv1.Deployment{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName, Namespace: serviceNamespace}, deployment)
	assertNoError(t, err)

	nSelector := common.InfraNodeSelector()
	maps.Copy(nSelector, argocommon.DefaultNodeSelector())
	assert.DeepEqual(t, deployment.Spec.Template.Spec.NodeSelector, nSelector)
	assert.DeepEqual(t, deployment.Spec.Template.Spec.Tolerations, deploymentDefaultTolerations())
}

func TestPlugin_reconcileDeployment(t *testing.T) {
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(newGitopsService()).Build()
	reconciler := newReconcileGitOpsService(fakeClient, s)
	instance := &pipelinesv1alpha1.GitopsService{}

	_, err := reconciler.reconcileDeployment(instance, newRequest(serviceNamespace, gitopsPluginName))
	assertNoError(t, err)

	deployment := &appsv1.Deployment{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName, Namespace: serviceNamespace}, deployment)
	assertNoError(t, err)
}

func TestPlugin_reconcileDeployment_ChangedResources(t *testing.T) {
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(newGitopsService()).Build()
	reconciler := newReconcileGitOpsService(fakeClient, s)
	instance := &pipelinesv1alpha1.GitopsService{
		Spec: pipelinesv1alpha1.GitopsServiceSpec{
			Resources: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resourcev1.MustParse("256Mi"),
					corev1.ResourceCPU:    resourcev1.MustParse("500m"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resourcev1.MustParse("512Mi"),
					corev1.ResourceCPU:    resourcev1.MustParse("1"),
				},
			},
		},
	}

	_, err := reconciler.reconcileDeployment(instance, newRequest(serviceNamespace, gitopsPluginName))
	assertNoError(t, err)

	deployment := &appsv1.Deployment{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName, Namespace: serviceNamespace}, deployment)
	assertNoError(t, err)
	assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].Resources.Requests["memory"], resourcev1.MustParse("256Mi"))
	assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].Resources.Requests["cpu"], resourcev1.MustParse("500m"))
	assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].Resources.Limits["memory"], resourcev1.MustParse("512Mi"))
	assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].Resources.Limits["cpu"], resourcev1.MustParse("1"))
}

func TestPlugin_ReconcileDeployment_ChangeExistingResourceValues(t *testing.T) {
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(newGitopsService()).Build()
	reconciler := newReconcileGitOpsService(fakeClient, s)
	instance := &pipelinesv1alpha1.GitopsService{
		Spec: pipelinesv1alpha1.GitopsServiceSpec{
			Resources: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resourcev1.MustParse("256Mi"),
					corev1.ResourceCPU:    resourcev1.MustParse("500m"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resourcev1.MustParse("512Mi"),
					corev1.ResourceCPU:    resourcev1.MustParse("1"),
				},
			},
		},
	}

	_, err := reconciler.reconcileDeployment(instance, newRequest(serviceNamespace, gitopsPluginName))
	assertNoError(t, err)

	deployment := &appsv1.Deployment{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName, Namespace: serviceNamespace}, deployment)
	assertNoError(t, err)
	assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].Resources.Requests["memory"], resourcev1.MustParse("256Mi"))
	assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].Resources.Requests["cpu"], resourcev1.MustParse("500m"))
	assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].Resources.Limits["memory"], resourcev1.MustParse("512Mi"))
	assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].Resources.Limits["cpu"], resourcev1.MustParse("1"))

	// Update the resource values again
	instance.Spec.Resources = &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resourcev1.MustParse("128Mi"),
			corev1.ResourceCPU:    resourcev1.MustParse("250m"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resourcev1.MustParse("256Mi"),
			corev1.ResourceCPU:    resourcev1.MustParse("500m"),
		},
	}

	_, err = reconciler.reconcileDeployment(instance, newRequest(serviceNamespace, gitopsPluginName))
	assertNoError(t, err)

	deployment = &appsv1.Deployment{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName, Namespace: serviceNamespace}, deployment)
	assertNoError(t, err)
	assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].Resources.Requests["memory"], resourcev1.MustParse("128Mi"))
	assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].Resources.Requests["cpu"], resourcev1.MustParse("250m"))
	assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].Resources.Limits["memory"], resourcev1.MustParse("256Mi"))
	assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].Resources.Limits["cpu"], resourcev1.MustParse("500m"))

}

func TestPlugin_reconcileService(t *testing.T) {
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(newGitopsService()).Build()
	reconciler := newReconcileGitOpsService(fakeClient, s)

	instance := &pipelinesv1alpha1.GitopsService{}
	_, err := reconciler.reconcileService(instance, newRequest(serviceNamespace, gitopsPluginName))
	assertNoError(t, err)

	service := &corev1.Service{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName, Namespace: serviceNamespace}, service)
	assertNoError(t, err)
}

func TestPlugin_reconcileService_changedAnnotations(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
	}{
		{
			name: "default annotations",
			annotations: map[string]string{
				"service.beta.openshift.io/serving-cert-secret-name": pluginServingCertName,
			},
		},
		{
			name: "changed annotations",
			annotations: map[string]string{
				"service.beta.openshift.io/serving-cert-secret-name": "wrong name",
			},
		},
	}

	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(newGitopsService()).Build()
	reconciler := newReconcileGitOpsService(fakeClient, s)
	instance := &pipelinesv1alpha1.GitopsService{}

	for x, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      gitopsPluginName,
					Namespace: serviceNamespace,
					Labels: map[string]string{
						kubeAppLabelApp:       gitopsPluginName,
						kubeAppLabelComponent: gitopsPluginName,
						kubeAppLabelInstance:  gitopsPluginName,
						kubeAppLabelPartOf:    gitopsPluginName,
					},
					Annotations: map[string]string{
						"service.beta.openshift.io/serving-cert-secret-name": pluginServingCertName,
					},
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{
						kubeAppLabelApp: gitopsPluginName,
					},
					Ports: []corev1.ServicePort{{
						Port:       servicePort,
						Protocol:   corev1.ProtocolTCP,
						Name:       "tcp-9001",
						TargetPort: intstr.FromInt(int(servicePort)),
					}},
				},
			}

			if x == 0 {
				err := reconciler.Client.Create(context.TODO(), svc)
				assertNoError(t, err)
			}

			_, err := reconciler.reconcileConsolePlugin(instance, newRequest(serviceNamespace, gitopsPluginName))
			assertNoError(t, err)

			service := &corev1.Service{}
			err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName, Namespace: serviceNamespace}, service)
			assertNoError(t, err)

			assert.Equal(t, service.Annotations["service.beta.openshift.io/serving-cert-secret-name"], "console-serving-cert")
		})
	}
}

func TestPlugin_reconcileService_changedLabels(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
	}{
		{
			name: "default labels",
			labels: map[string]string{
				kubeAppLabelApp:       gitopsPluginName,
				kubeAppLabelComponent: gitopsPluginName,
				kubeAppLabelInstance:  gitopsPluginName,
				kubeAppLabelPartOf:    gitopsPluginName,
			},
		},
		{
			name: "changed labels",
			labels: map[string]string{
				kubeAppLabelApp:       "wrong name",
				kubeAppLabelComponent: "wrong name",
				kubeAppLabelInstance:  "wrong name",
				kubeAppLabelPartOf:    "wrong name",
			},
		},
	}

	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(newGitopsService()).Build()
	reconciler := newReconcileGitOpsService(fakeClient, s)
	instance := &pipelinesv1alpha1.GitopsService{}

	for x, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      gitopsPluginName,
					Namespace: serviceNamespace,
					Labels:    test.labels,
					Annotations: map[string]string{
						"service.beta.openshift.io/serving-cert-secret-name": pluginServingCertName,
					},
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{
						kubeAppLabelApp: gitopsPluginName,
					},
					Ports: []corev1.ServicePort{{
						Port:       servicePort,
						Protocol:   corev1.ProtocolTCP,
						Name:       "tcp-9001",
						TargetPort: intstr.FromInt(int(servicePort)),
					}},
				},
			}
			if x == 0 {
				err := reconciler.Client.Create(context.TODO(), svc)
				assertNoError(t, err)
			}

			_, err := reconciler.reconcileConsolePlugin(instance, newRequest(serviceNamespace, gitopsPluginName))
			assertNoError(t, err)

			service := &corev1.Service{}
			err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName, Namespace: serviceNamespace}, service)
			assertNoError(t, err)

			assert.Equal(t, service.Labels[kubeAppLabelApp], "gitops-plugin")
			assert.Equal(t, service.Labels[kubeAppLabelComponent], "gitops-plugin")
			assert.Equal(t, service.Labels[kubeAppLabelInstance], "gitops-plugin")
			assert.Equal(t, service.Labels[kubeAppLabelPartOf], "gitops-plugin")
		})
	}
}

func TestPlugin_reconcileService_changedSelector(t *testing.T) {
	tests := []struct {
		name     string
		selector map[string]string
	}{
		{
			name: "default selector",
			selector: map[string]string{
				kubeAppLabelApp: gitopsPluginName,
			},
		},
		{
			name: "changed selector",
			selector: map[string]string{
				kubeAppLabelApp: "wrong name",
			},
		},
	}

	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(newGitopsService()).Build()
	reconciler := newReconcileGitOpsService(fakeClient, s)
	instance := &pipelinesv1alpha1.GitopsService{}

	for x, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      gitopsPluginName,
					Namespace: serviceNamespace,
					Labels: map[string]string{
						kubeAppLabelApp:       gitopsPluginName,
						kubeAppLabelComponent: gitopsPluginName,
						kubeAppLabelInstance:  gitopsPluginName,
						kubeAppLabelPartOf:    gitopsPluginName,
					},
					Annotations: map[string]string{
						"service.beta.openshift.io/serving-cert-secret-name": pluginServingCertName,
					},
				},
				Spec: corev1.ServiceSpec{
					Selector: test.selector,
					Ports: []corev1.ServicePort{{
						Port:       servicePort,
						Protocol:   corev1.ProtocolTCP,
						Name:       "tcp-9001",
						TargetPort: intstr.FromInt(int(servicePort)),
					}},
				},
			}
			if x == 0 {
				err := reconciler.Client.Create(context.TODO(), svc)
				assertNoError(t, err)
			}

			_, err := reconciler.reconcileConsolePlugin(instance, newRequest(serviceNamespace, gitopsPluginName))
			assertNoError(t, err)

			service := &corev1.Service{}
			err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName, Namespace: serviceNamespace}, service)
			assertNoError(t, err)

			assert.Equal(t, service.Spec.Selector[kubeAppLabelApp], "gitops-plugin")
		})
	}
}

func TestPlugin_reconcileService_changedPorts(t *testing.T) {
	tests := []struct {
		name  string
		ports []corev1.ServicePort
	}{
		{
			name: "default port",
			ports: []corev1.ServicePort{{
				Port:       servicePort,
				Protocol:   corev1.ProtocolTCP,
				Name:       "tcp-9001",
				TargetPort: intstr.FromInt(int(servicePort)),
			}},
		},
		{
			name: "changed port",
			ports: []corev1.ServicePort{{
				Port:       servicePort,
				Protocol:   corev1.ProtocolTCP,
				Name:       "tcp-9001",
				TargetPort: intstr.FromInt(int(servicePort)),
			}},
		},
	}

	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(newGitopsService()).Build()
	reconciler := newReconcileGitOpsService(fakeClient, s)
	instance := &pipelinesv1alpha1.GitopsService{}

	for x, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      gitopsPluginName,
					Namespace: serviceNamespace,
					Labels: map[string]string{
						kubeAppLabelApp:       gitopsPluginName,
						kubeAppLabelComponent: gitopsPluginName,
						kubeAppLabelInstance:  gitopsPluginName,
						kubeAppLabelPartOf:    gitopsPluginName,
					},
					Annotations: map[string]string{
						"service.beta.openshift.io/serving-cert-secret-name": pluginServingCertName,
					},
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{
						kubeAppLabelApp: gitopsPluginName,
					},
					Ports: []corev1.ServicePort{{
						Port:       servicePort,
						Protocol:   corev1.ProtocolTCP,
						Name:       "tcp-9001",
						TargetPort: intstr.FromInt(int(servicePort)),
					}},
				},
			}
			if x == 0 {
				err := reconciler.Client.Create(context.TODO(), svc)
				assertNoError(t, err)
			}

			_, err := reconciler.reconcileConsolePlugin(instance, newRequest(serviceNamespace, gitopsPluginName))
			assertNoError(t, err)

			service := &corev1.Service{}
			err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName, Namespace: serviceNamespace}, service)
			assertNoError(t, err)

			assert.Equal(t, service.Spec.Ports[0].Port, int32(9001))
			assert.Equal(t, service.Spec.Ports[0].Protocol, corev1.ProtocolTCP)
			assert.Equal(t, service.Spec.Ports[0].Name, "tcp-9001")
			assert.Equal(t, service.Spec.Ports[0].TargetPort, intstr.FromInt(int(servicePort)))
		})
	}
}

func TestPlugin_reconcileConsolePlugin(t *testing.T) {
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(newGitopsService()).Build()
	reconciler := newReconcileGitOpsService(fakeClient, s)

	instance := &pipelinesv1alpha1.GitopsService{}

	_, err := reconciler.reconcileConsolePlugin(instance, newRequest(serviceNamespace, gitopsPluginName))
	assertNoError(t, err)

	consolePlugin := &consolev1.ConsolePlugin{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName}, consolePlugin)
	assertNoError(t, err)
}

func TestPlugin_reconcileConsolePlugin_changedDisplayName(t *testing.T) {
	tests := []struct {
		name        string
		displayName string
	}{
		{
			name:        "default displayName",
			displayName: displayName,
		},
		{
			name:        "changed displayName",
			displayName: "fake displayName",
		},
	}

	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(newGitopsService()).Build()
	reconciler := newReconcileGitOpsService(fakeClient, s)
	instance := &pipelinesv1alpha1.GitopsService{}

	for x, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cp := &consolev1.ConsolePlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: gitopsPluginName,
				},
				Spec: consolev1.ConsolePluginSpec{
					DisplayName: test.displayName,
					Backend: consolev1.ConsolePluginBackend{
						Type: consolev1.Service,
						Service: &consolev1.ConsolePluginService{
							Name:      gitopsPluginName,
							Namespace: serviceNamespace,
							Port:      servicePort,
							BasePath:  "/",
						},
					},
				},
			}
			if x == 0 {
				err := reconciler.Client.Create(context.TODO(), cp)
				assertNoError(t, err)
			}

			_, err := reconciler.reconcileConsolePlugin(instance, newRequest(serviceNamespace, gitopsPluginName))
			assertNoError(t, err)

			consolePlugin := &consolev1.ConsolePlugin{}
			err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName}, consolePlugin)
			assertNoError(t, err)

			assert.Equal(t, consolePlugin.Spec.DisplayName, displayName)
		})
	}
}
func TestPlugin_reconcileConsolePlugin_changedService(t *testing.T) {
	tests := []struct {
		name    string
		service consolev1.ConsolePluginService
	}{
		{
			name: "default service",
			service: consolev1.ConsolePluginService{
				Name:      gitopsPluginName,
				Namespace: serviceNamespace,
				Port:      servicePort,
				BasePath:  "/",
			},
		},
		{
			name: "changed service",
			service: consolev1.ConsolePluginService{
				Name:      "wrong name",
				Namespace: "wrong namespace",
				Port:      int32(9002),
				BasePath:  "/root",
			},
		},
	}

	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(newGitopsService()).Build()
	reconciler := newReconcileGitOpsService(fakeClient, s)
	instance := &pipelinesv1alpha1.GitopsService{}

	for x, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cp := &consolev1.ConsolePlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name: gitopsPluginName,
				},
				Spec: consolev1.ConsolePluginSpec{
					DisplayName: displayName,
					Backend: consolev1.ConsolePluginBackend{
						Type:    consolev1.Service,
						Service: &test.service,
					},
				},
			}
			if x == 0 {
				err := reconciler.Client.Create(context.TODO(), cp)
				assertNoError(t, err)
			}

			_, err := reconciler.reconcileConsolePlugin(instance, newRequest(serviceNamespace, gitopsPluginName))
			assertNoError(t, err)

			consolePlugin := &consolev1.ConsolePlugin{}
			err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName}, consolePlugin)
			assertNoError(t, err)

			assert.Equal(t, consolePlugin.Spec.Backend.Service.Name, "gitops-plugin")
			assert.Equal(t, consolePlugin.Spec.Backend.Service.Namespace, "openshift-gitops")
			assert.Equal(t, consolePlugin.Spec.Backend.Service.Port, int32(9001))
			assert.Equal(t, consolePlugin.Spec.Backend.Service.BasePath, "/")
		})
	}
}

func TestPlug_reconcileConfigMap(t *testing.T) {
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(newGitopsService()).Build()
	reconciler := newReconcileGitOpsService(fakeClient, s)

	instance := &pipelinesv1alpha1.GitopsService{}
	_, err := reconciler.reconcileConfigMap(instance, newRequest(serviceNamespace, httpdConfigMapName))
	assertNoError(t, err)

	configMap := &corev1.ConfigMap{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: httpdConfigMapName, Namespace: serviceNamespace}, configMap)
	assertNoError(t, err)
}

func TestPlug_reconcileConfigMap_changedLabels(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
	}{
		{
			name: "default Labels",
			labels: map[string]string{
				kubeAppLabelApp:    gitopsPluginName,
				kubeAppLabelPartOf: gitopsPluginName,
			},
		},
		{
			name: "changed Labels",
			labels: map[string]string{
				kubeAppLabelApp:    "wrong name",
				kubeAppLabelPartOf: "wrong name",
			},
		},
	}

	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(newGitopsService()).Build()
	reconciler := newReconcileGitOpsService(fakeClient, s)
	instance := &pipelinesv1alpha1.GitopsService{}

	for x, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      httpdConfigMapName,
					Namespace: serviceNamespace,
					Labels:    test.labels,
				},
				Data: map[string]string{
					"httpd.conf": httpdConfig,
				},
			}
			if x == 0 {
				err := reconciler.Client.Create(context.TODO(), cm)
				assertNoError(t, err)
			}

			_, err := reconciler.reconcileConsolePlugin(instance, newRequest(serviceNamespace, gitopsPluginName))
			assertNoError(t, err)

			configMap := &corev1.ConfigMap{}
			err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: httpdConfigMapName, Namespace: serviceNamespace}, configMap)
			assertNoError(t, err)

			assert.Equal(t, configMap.Labels[kubeAppLabelApp], "gitops-plugin")
			assert.Equal(t, configMap.Labels[kubeAppLabelPartOf], "gitops-plugin")
		})
	}
}
