package gitopsservice

import (
	json "encoding/json"

	appsv1 "github.com/openshift/api/apps/v1"
	routev1 "github.com/openshift/api/route/v1"
	template "github.com/openshift/api/template/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var (
	privilegedMode bool  = false
	graceTime      int64 = 75
)

func getSSOConfigMapTemplate() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"description": "ConfigMap providing service ca bundle",
				"service.beta.openshift.io/inject-cabundle": "true",
			},
			Labels: map[string]string{
				"application": "${APPLICATION_NAME}",
			},
			Name: "${APPLICATION_NAME}-service-ca",
		},
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
	}
}

func getRHSSOContainer() corev1.Container {
	return corev1.Container{
		Env: []corev1.EnvVar{
			{Name: "SSO_HOSTNAME", Value: "${SSO_HOSTNAME}"},
			{Name: "DB_MIN_POOL_SIZE", Value: "${DB_MIN_POOL_SIZE}"},
			{Name: "DB_MAX_POOL_SIZE", Value: "${DB_MAX_POOL_SIZE}"},
			{Name: "DB_TX_ISOLATION", Value: "${DB_TX_ISOLATION}"},
			{Name: "JGROUPS_PING_PROTOCOL", Value: "openshift.DNS_PING"},
			{Name: "OPENSHIFT_DNS_PING_SERVICE_NAME", Value: "${APPLICATION_NAME}-ping"},
			{Name: "OPENSHIFT_DNS_PING_SERVICE_PORT", Value: "8888"},
			{Name: "X509_CA_BUNDLE", Value: "/var/run/configmaps/service-ca/service-ca.crt /var/run/secrets/kubernetes.io/serviceaccount/ca.crt"},
			{Name: "JGROUPS_CLUSTER_PASSWORD", Value: "${JGROUPS_CLUSTER_PASSWORD}"},
			{Name: "SSO_ADMIN_USERNAME", Value: "${SSO_ADMIN_USERNAME}"},
			{Name: "SSO_ADMIN_PASSWORD", Value: "${SSO_ADMIN_PASSWORD}"},
			{Name: "SSO_REALM", Value: "${SSO_REALM}"},
			{Name: "SSO_SERVICE_USERNAME", Value: "${SSO_SERVICE_USERNAME}"},
			{Name: "SSO_SERVICE_PASSWORD", Value: "${SSO_SERVICE_PASSWORD}"},
		},
		Image:           "${APPLICATION_NAME}",
		ImagePullPolicy: "Always",
		LivenessProbe: &corev1.Probe{
			FailureThreshold: 3,
			Handler: corev1.Handler{
				Exec: &corev1.ExecAction{
					Command: []string{
						"/bin/bash",
						"-c",
						"/opt/eap/bin/livenessProbe.sh",
					},
				},
			},
			InitialDelaySeconds: 60,
		},
		Name: "${APPLICATION_NAME}",
		Ports: []corev1.ContainerPort{
			{ContainerPort: 8778, Name: "jolokia", Protocol: "TCP"},
			{ContainerPort: 8080, Name: "http", Protocol: "TCP"},
			{ContainerPort: 8443, Name: "https", Protocol: "TCP"},
			{ContainerPort: 8888, Name: "ping", Protocol: "TCP"},
		},
		ReadinessProbe: &corev1.Probe{
			FailureThreshold: 10,
			Handler: corev1.Handler{
				Exec: &corev1.ExecAction{
					Command: []string{
						"/bin/bash",
						"-c",
						"/opt/eap/bin/readinessProbe.sh",
					},
				},
			},
			InitialDelaySeconds: 60,
		},
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				"memory": resource.Quantity{
					Format: "${MEMORY_LIMIT}",
				},
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				MountPath: "/etc/x509/https",
				Name:      "sso-x509-https-volume",
				ReadOnly:  true,
			},
			{
				MountPath: "/etc/x509/jgroups",
				Name:      "sso-x509-jgroups-volume",
				ReadOnly:  true,
			},
			{
				MountPath: "/var/run/configmaps/service-ca",
				Name:      "service-ca",
				ReadOnly:  true,
			},
		},
	}
}

func getSSODeploymentConfigTemplate() *appsv1.DeploymentConfig {
	rhSSOContainer := getRHSSOContainer()
	return &appsv1.DeploymentConfig{
		ObjectMeta: metav1.ObjectMeta{
			Labels:    map[string]string{"application": "${APPLICATION_NAME}"},
			Name:      "${APPLICATION_NAME}",
			Namespace: serviceNamespace,
		},
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "DeploymentConfig"},
		Spec: appsv1.DeploymentConfigSpec{
			Replicas: 1,
			Selector: map[string]string{"deploymentConfig": "${APPLICATION_NAME}"},
			Strategy: appsv1.DeploymentStrategy{
				Type: "Recreate",
			},
			Template: &corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"application":      "${APPLICATION_NAME}",
						"deploymentConfig": "${APPLICATION_NAME}",
					},
					Name: "${APPLICATION_NAME}",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						rhSSOContainer,
					},
					TerminationGracePeriodSeconds: &graceTime,
					Volumes: []corev1.Volume{
						{
							Name: "sso-x509-https-volume",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: "sso-x509-https-secret",
								},
							},
						},
						{
							Name: "sso-x509-jgroups-volume",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: "sso-x509-https-secret",
								},
							},
						},
						{
							Name: "service-ca",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "${APPLICATION_NAME}-service-ca",
									},
								},
							},
						},
					},
				},
			},
			Triggers: appsv1.DeploymentTriggerPolicies{
				appsv1.DeploymentTriggerPolicy{
					Type: "ImageChange",
					ImageChangeParams: &appsv1.DeploymentTriggerImageChangeParams{
						Automatic:      true,
						ContainerNames: []string{"${APPLICATION_NAME}"},
						From: corev1.ObjectReference{
							Kind:      "ImageStreamTag",
							Name:      "sso74-openshift-rhel8:7.4",
							Namespace: "${IMAGE_STREAM_NAMESPACE}",
						},
					},
				},
				appsv1.DeploymentTriggerPolicy{
					Type: "ConfigChange",
				},
			},
		},
	}
}

func getSSOServiceTemplate() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{"application": "${APPLICATION_NAME}"},
			Name:   "${APPLICATION_NAME}",
			Annotations: map[string]string{
				"description": "The web server's https port",
				"service.alpha.openshift.io/serving-cert-secret-name": "sso-x509-https-secret",
			},
		},
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Port: portTLS, TargetPort: intstr.FromInt(int(portTLS))},
			},
			Selector: map[string]string{
				"deploymentConfig": "${APPLICATION_NAME}",
			},
		},
	}
}

func getSSOPingServiceTemplate() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{"application": "${APPLICATION_NAME}"},
			Name:   "${APPLICATION_NAME}",
			Annotations: map[string]string{
				"description": "The JGroups ping port for clustering",
				"service.alpha.kubernetes.io/tolerate-unready-endpoints": "true",
				"service.alpha.openshift.io/serving-cert-secret-name":    "sso-x509-jgroups-secret",
			},
		},
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Ports: []corev1.ServicePort{
				{Name: "ping", Port: 8888},
			},
			Selector: map[string]string{
				"deploymentConfig": "${APPLICATION_NAME}",
			},
		},
	}
}

func getSSORouteTemplate() *routev1.Route {
	return &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      map[string]string{"application": "${APPLICATION_NAME}"},
			Name:        "${APPLICATION_NAME}",
			Annotations: map[string]string{"description": "Route for application's https service"},
		},
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Route"},
		Spec: routev1.RouteSpec{
			TLS: &routev1.TLSConfig{
				Termination: "reencrypt",
			},
			To: routev1.RouteTargetReference{
				Name: "${APPLICATION_NAME}",
			},
		},
	}
}

func newSSOTemplateInstance() *template.TemplateInstance {
	tpl, err := newSSOTemplate()
	if err != nil {
		log.Error(err, "Failed creating template instance for sso")
		return &template.TemplateInstance{}
	}
	return &template.TemplateInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rhsso",
			Namespace: "openshift-gitops",
		},
		Spec: template.TemplateInstanceSpec{
			Template: tpl,
		},
	}
}

func newSSOTemplate() (template.Template, error) {
	tmpl := template.Template{}
	configMapTemplate := getSSOConfigMapTemplate()
	deploymentConfigTemplate := getSSODeploymentConfigTemplate()
	serviceTemplate := getSSOServiceTemplate()
	pingServiceTemplate := getSSOPingServiceTemplate()
	routeTemplate := getSSORouteTemplate()

	configMap, err := json.Marshal(configMapTemplate)
	if err != nil {
		return tmpl, err
	}

	deploymentConfig, err := json.Marshal(deploymentConfigTemplate)
	if err != nil {
		return tmpl, err
	}

	service, err := json.Marshal(serviceTemplate)
	if err != nil {
		return tmpl, err
	}

	pingService, err := json.Marshal(pingServiceTemplate)
	if err != nil {
		return tmpl, err
	}

	route, err := json.Marshal(routeTemplate)
	if err != nil {
		return tmpl, err
	}

	tmpl = template.Template{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"description":               "RH-SSO Template for Gitops Operator",
				"iconClass":                 "icon-sso",
				"openshift.io/display-name": "Keycloak",
				"tags":                      "keycloak",
				"version":                   "9.0.4-SNAPSHOT",
			},
			Name:      "rhsso",
			Namespace: "openshift-gitops",
		},
		Objects: []runtime.RawExtension{
			{
				Raw: json.RawMessage(configMap),
			},
			{
				Raw: json.RawMessage(deploymentConfig),
			},
			{
				Raw: json.RawMessage(service),
			},
			{
				Raw: json.RawMessage(pingService),
			},
			{
				Raw: json.RawMessage(route),
			},
		},
		Parameters: []template.Parameter{
			{Name: "APPLICATION_NAME", Value: "sso", Required: true},
			{Name: "SSO_HOSTNAME"},
			{Name: "JGROUPS_CLUSTER_PASSWORD", Generate: "expression", From: "[a-zA-Z0-9]{32}", Required: true},
			{Name: "DB_MIN_POOL_SIZE"},
			{Name: "DB_MAX_POOL_SIZE"},
			{Name: "DB_TX_ISOLATION"},
			{Name: "IMAGE_STREAM_NAMESPACE", Value: "openshift", Required: true},
			{Name: "SSO_ADMIN_USERNAME", Generate: "expression", From: "[a-zA-Z0-9]{8}", Required: true},
			{Name: "SSO_ADMIN_PASSWORD", Generate: "expression", From: "[a-zA-Z0-9]{8}", Required: true},
			{Name: "SSO_REALM", DisplayName: "RH-SSO Realm"},
			{Name: "SSO_SERVICE_USERNAME", DisplayName: "RH-SSO Service Username"},
			{Name: "SSO_SERVICE_PASSWORD", DisplayName: "RH-SSO Service Password"},
			{Name: "MEMORY_LIMIT", Value: "1Gi"},
		},
	}
	return tmpl, err
}
