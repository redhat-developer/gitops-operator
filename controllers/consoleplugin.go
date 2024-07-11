package controllers

import (
	"context"
	"fmt"
	"os"
	"reflect"

	argocommon "github.com/argoproj-labs/argocd-operator/common"
	argocdutil "github.com/argoproj-labs/argocd-operator/controllers/argoutil"
	consolev1 "github.com/openshift/api/console/v1"
	pipelinesv1alpha1 "github.com/redhat-developer/gitops-operator/api/v1alpha1"
	"github.com/redhat-developer/gitops-operator/controllers/util"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/redhat-developer/gitops-operator/common"
	"k8s.io/apimachinery/pkg/api/errors"
	resourcev1 "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	gitopsPluginName             = "gitops-plugin"
	displayName                  = "GitOps Plugin"
	gitopsPluginSvcName          = gitopsPluginName + "-service"
	proxyAlias                   = "gitops"
	pluginImageEnv               = "GITOPS_CONSOLE_PLUGIN_IMAGE"
	servicePort                  = 9001
	pluginServingCertName        = "console-serving-cert"
	kubeAppLabelApp              = "app"
	kubeAppLabelComponent        = "app.kubernetes.io/component"
	kubeAppLabelInstance         = "app.kubernetes.io/instance"
	kubeAppLabelPartOf           = "app.kubernetes.io/part-of"
	kubeAppLabelRuntimeNamespace = "app.kubernetes.io/runtime-namespace"
	httpdConfigMapName           = "httpd-cfg"
)

func getPluginPodSpec() corev1.PodSpec {
	consolePluginImage := os.Getenv(pluginImageEnv)
	if consolePluginImage == "" {
		image := common.DefaultConsoleImage
		version := common.DefaultConsoleVersion
		consolePluginImage = image + ":" + version
	}

	podSpec := corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Env:             util.ProxyEnvVars(),
				Name:            gitopsPluginName,
				Image:           consolePluginImage,
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
						DefaultMode: pointer.Int32Ptr(420),
					},
				},
			},
			{
				Name: httpdConfigMapName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: httpdConfigMapName,
						},
						DefaultMode: pointer.Int32Ptr(420),
					},
				},
			},
		},
		RestartPolicy: corev1.RestartPolicyAlways,
		DNSPolicy:     corev1.DNSClusterFirst,
	}

	return podSpec
}

func pluginDeployment() *appsv1.Deployment {
	podSpec := getPluginPodSpec()
	template := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				kubeAppLabelApp: gitopsPluginName,
			},
		},
		Spec: podSpec,
	}
	var replicas int32 = 1
	return &appsv1.Deployment{
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
					kubeAppLabelApp: gitopsPluginName,
				},
			},
			Template: template,
		},
	}
}

func consolePlugin() *consolev1.ConsolePlugin {
	return &consolev1.ConsolePlugin{
		ObjectMeta: metav1.ObjectMeta{
			Name: gitopsPluginName,
		},
		Spec: consolev1.ConsolePluginSpec{
			DisplayName: displayName,
			Backend: consolev1.ConsolePluginBackend{
				Type: consolev1.Service,
				Service: &consolev1.ConsolePluginService{
					Name:      gitopsPluginName,
					Namespace: serviceNamespace,
					Port:      servicePort,
					BasePath:  "/",
				},
			},
			I18n: consolev1.ConsolePluginI18n{
				LoadType: consolev1.Preload,
			},
		},
	}
}

func pluginService() *corev1.Service {
	spec := corev1.ServiceSpec{
		Selector: map[string]string{
			kubeAppLabelApp: gitopsPluginName,
		},
		Ports: []corev1.ServicePort{{
			Port:       servicePort,
			Protocol:   corev1.ProtocolTCP,
			Name:       "tcp-9001",
			TargetPort: intstr.FromInt(int(servicePort)),
		}},
	}

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
		Spec: spec,
	}
	return svc
}

var httpdConfig = fmt.Sprintf(`LoadModule ssl_module modules/mod_ssl.so
Listen %d https
ServerRoot "/etc/httpd"

<VirtualHost *:%d>
	DocumentRoot /var/www/html/plugin
	SSLEngine on
	SSLCertificateFile "/etc/httpd-ssl/certs/tls.crt"
	SSLCertificateKeyFile "/etc/httpd-ssl/private/tls.key"
</VirtualHost>`, servicePort, servicePort)

func pluginConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      httpdConfigMapName,
			Namespace: serviceNamespace,
			Labels: map[string]string{
				kubeAppLabelApp:    gitopsPluginName,
				kubeAppLabelPartOf: gitopsPluginName,
			},
		},
		Data: map[string]string{
			"httpd.conf": httpdConfig,
		},
	}
}

func (r *ReconcileGitopsService) reconcileDeployment(cr *pipelinesv1alpha1.GitopsService, request reconcile.Request) (reconcile.Result, error) {
	reqLogger := logs.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	newPluginDeployment := pluginDeployment()

	if err := controllerutil.SetControllerReference(cr, newPluginDeployment, r.Scheme); err != nil {
		return reconcile.Result{}, err
	}

	newPluginDeployment.Spec.Template.Spec.NodeSelector = argocommon.DefaultNodeSelector()

	if cr.Spec.RunOnInfra {
		newPluginDeployment.Spec.Template.Spec.NodeSelector[common.InfraNodeLabelSelector] = ""
	}
	if len(cr.Spec.NodeSelector) > 0 {
		newPluginDeployment.Spec.Template.Spec.NodeSelector = argocdutil.AppendStringMap(newPluginDeployment.Spec.Template.Spec.NodeSelector, cr.Spec.NodeSelector)
	}

	if len(cr.Spec.Tolerations) > 0 {
		newPluginDeployment.Spec.Template.Spec.Tolerations = cr.Spec.Tolerations
	}

	// Check if this Deployment already exists
	existingPluginDeployment := &appsv1.Deployment{}

	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: newPluginDeployment.Name, Namespace: newPluginDeployment.Namespace}, existingPluginDeployment)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Creating a new Plugin Deployment", "Namespace", newPluginDeployment.Namespace, "Name", newPluginDeployment.Name)
			err = r.Client.Create(context.TODO(), newPluginDeployment)
			if err != nil {
				return reconcile.Result{}, err
			}
		} else {
			return reconcile.Result{}, err
		}
	} else {
		existingSpecTemplate := &existingPluginDeployment.Spec.Template
		newSpecTemplate := newPluginDeployment.Spec.Template
		changed := !reflect.DeepEqual(existingPluginDeployment.ObjectMeta.Labels, newPluginDeployment.ObjectMeta.Labels) ||
			!reflect.DeepEqual(existingPluginDeployment.Spec.Replicas, newPluginDeployment.Spec.Replicas) ||
			!reflect.DeepEqual(existingPluginDeployment.Spec.Selector, newPluginDeployment.Spec.Selector) ||
			!reflect.DeepEqual(existingSpecTemplate.Labels, newSpecTemplate.Labels) ||
			!reflect.DeepEqual(existingSpecTemplate.Spec.Containers, newSpecTemplate.Spec.Containers) ||
			!reflect.DeepEqual(existingSpecTemplate.Spec.Volumes, newSpecTemplate.Spec.Volumes) ||
			!reflect.DeepEqual(existingSpecTemplate.Spec.RestartPolicy, newSpecTemplate.Spec.RestartPolicy) ||
			!reflect.DeepEqual(existingSpecTemplate.Spec.DNSPolicy, newSpecTemplate.Spec.DNSPolicy) ||
			!reflect.DeepEqual(existingPluginDeployment.Spec.Template.Spec.NodeSelector, newPluginDeployment.Spec.Template.Spec.NodeSelector) ||
			!reflect.DeepEqual(existingPluginDeployment.Spec.Template.Spec.Tolerations, newPluginDeployment.Spec.Template.Spec.Tolerations)

		if changed {
			reqLogger.Info("Reconciling plugin deployment", "Namespace", existingPluginDeployment.Namespace, "Name", existingPluginDeployment.Name)
			existingPluginDeployment.ObjectMeta.Labels = newPluginDeployment.ObjectMeta.Labels
			existingPluginDeployment.Spec.Replicas = newPluginDeployment.Spec.Replicas
			existingPluginDeployment.Spec.Selector = newPluginDeployment.Spec.Selector
			existingSpecTemplate.Labels = newSpecTemplate.Labels
			existingSpecTemplate.Spec.Containers = newSpecTemplate.Spec.Containers
			existingSpecTemplate.Spec.Volumes = newSpecTemplate.Spec.Volumes
			existingSpecTemplate.Spec.RestartPolicy = newSpecTemplate.Spec.RestartPolicy
			existingSpecTemplate.Spec.DNSPolicy = newSpecTemplate.Spec.DNSPolicy
			existingPluginDeployment.Spec.Template.Spec.NodeSelector = newPluginDeployment.Spec.Template.Spec.NodeSelector
			existingPluginDeployment.Spec.Template.Spec.Tolerations = newPluginDeployment.Spec.Template.Spec.Tolerations
			return reconcile.Result{}, r.Client.Update(context.TODO(), existingPluginDeployment)
		}
	}
	return reconcile.Result{}, nil
}

func (r *ReconcileGitopsService) reconcileService(instance *pipelinesv1alpha1.GitopsService, request reconcile.Request) (reconcile.Result, error) {
	reqLogger := logs.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	pluginServiceRef := pluginService()
	// Set GitopsService instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, pluginServiceRef, r.Scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Check if this Service already exists
	existingServiceRef := &corev1.Service{}
	if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: pluginServiceRef.Name, Namespace: pluginServiceRef.Namespace},
		existingServiceRef); err != nil {

		if errors.IsNotFound(err) {
			reqLogger.Info("Creating a new plugin Service", "Namespace", pluginServiceRef.Namespace, "Name", pluginServiceRef.Name)
			err = r.Client.Create(context.TODO(), pluginServiceRef)
			if err != nil {
				return reconcile.Result{}, err
			}
		} else {
			return reconcile.Result{}, err
		}
	} else {
		changed := !reflect.DeepEqual(existingServiceRef.ObjectMeta.Annotations, pluginServiceRef.ObjectMeta.Annotations) ||
			!reflect.DeepEqual(existingServiceRef.ObjectMeta.Labels, pluginServiceRef.ObjectMeta.Labels) ||
			!reflect.DeepEqual(existingServiceRef.Spec.Selector, pluginServiceRef.Spec.Selector) ||
			!reflect.DeepEqual(existingServiceRef.Spec.Ports, pluginServiceRef.Spec.Ports)

		if changed {
			reqLogger.Info("Reconciling plugin service", "Namespace", existingServiceRef.Namespace, "Name", existingServiceRef.Name)
			existingServiceRef.ObjectMeta.Annotations = pluginServiceRef.ObjectMeta.Annotations
			existingServiceRef.ObjectMeta.Labels = pluginServiceRef.ObjectMeta.Labels
			existingServiceRef.Spec.Selector = pluginServiceRef.Spec.Selector
			existingServiceRef.Spec.Ports = pluginServiceRef.Spec.Ports
			return reconcile.Result{}, r.Client.Update(context.TODO(), pluginServiceRef)
		}
	}
	return reconcile.Result{}, nil
}

func (r *ReconcileGitopsService) reconcileConsolePlugin(instance *pipelinesv1alpha1.GitopsService, request reconcile.Request) (reconcile.Result, error) {
	reqLogger := logs.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	newConsolePlugin := consolePlugin()

	if err := controllerutil.SetControllerReference(instance, newConsolePlugin, r.Scheme); err != nil {
		return reconcile.Result{}, err
	}

	existingPlugin := &consolev1.ConsolePlugin{}

	if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: gitopsPluginName},
		existingPlugin); err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Creating a new ConsolePlugin", "Namespace", serviceNamespace, "Name", gitopsPluginName)
			err = r.Client.Create(context.TODO(), newConsolePlugin)
			if err != nil {
				reqLogger.Error(err, "Error creating a new console plugin",
					"Name", newConsolePlugin.Name)
				return reconcile.Result{}, err
			}
		} else {
			return reconcile.Result{}, err
		}
	} else {
		changed := !reflect.DeepEqual(existingPlugin.Spec.DisplayName, newConsolePlugin.Spec.DisplayName) ||
			!reflect.DeepEqual(existingPlugin.Spec.Backend.Service, newConsolePlugin.Spec.Backend.Service)

		if changed {
			reqLogger.Info("Reconciling Console Plugin", "Namespace", existingPlugin.Namespace, "Name", existingPlugin.Name)
			existingPlugin.Spec.DisplayName = newConsolePlugin.Spec.DisplayName
			existingPlugin.Spec.Backend.Service = newConsolePlugin.Spec.Backend.Service
			return reconcile.Result{}, r.Client.Update(context.TODO(), newConsolePlugin)
		}
	}
	return reconcile.Result{}, nil
}

func (r *ReconcileGitopsService) reconcileConfigMap(instance *pipelinesv1alpha1.GitopsService, request reconcile.Request) (reconcile.Result, error) {
	reqLogger := logs.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	newPluginConfigMap := pluginConfigMap()

	if err := controllerutil.SetControllerReference(instance, newPluginConfigMap, r.Scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Check if this ConfigMap already exists
	existingPluginConfigMap := &corev1.ConfigMap{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: newPluginConfigMap.Name, Namespace: newPluginConfigMap.Namespace}, existingPluginConfigMap)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Creating a new Plugin ConfigMap", "Namespace", newPluginConfigMap.Namespace, "Name", newPluginConfigMap.Name)
			err = r.Client.Create(context.TODO(), newPluginConfigMap)
			if err != nil {
				return reconcile.Result{}, err
			}
		} else {
			return reconcile.Result{}, err
		}
	} else {
		changed := !reflect.DeepEqual(existingPluginConfigMap.Data, newPluginConfigMap.Data) ||
			!reflect.DeepEqual(existingPluginConfigMap.ObjectMeta.Labels, newPluginConfigMap.ObjectMeta.Labels)
		if changed {
			reqLogger.Info("Reconciling plugin configMap", "Namespace", existingPluginConfigMap.Namespace, "Name", existingPluginConfigMap.Name)
			existingPluginConfigMap.Data = newPluginConfigMap.Data
			existingPluginConfigMap.ObjectMeta.Labels = newPluginConfigMap.ObjectMeta.Labels
			return reconcile.Result{}, r.Client.Update(context.TODO(), newPluginConfigMap)
		}
	}
	return reconcile.Result{}, nil
}

// is this func the reconciler enty point to reconcile the current plugin state?
func (r *ReconcileGitopsService) reconcilePlugin(instance *pipelinesv1alpha1.GitopsService, request reconcile.Request) (reconcile.Result, error) {
	reqLogger := logs.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	if !util.IsConsoleAPIFound() {
		reqLogger.Info("Skip console plugin reconcile: OpenShift Console API not found")
		return reconcile.Result{}, nil
	}

	if result, err := r.reconcileService(instance, request); err != nil {
		return result, err
	}

	if result, err := r.reconcileDeployment(instance, request); err != nil {
		return result, err
	}

	if result, err := r.reconcileConfigMap(instance, request); err != nil {
		return result, err
	}

	if result, err := r.reconcileConsolePlugin(instance, request); err != nil {
		return result, err
	}

	return reconcile.Result{}, nil
}
