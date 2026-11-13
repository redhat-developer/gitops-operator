package operator

import (
	"os"

	aiconfig "github.com/redhat-developer/gitops-operator/argocd-operator/pkg/ai"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	resourcev1 "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	ServiceName        = aiconfig.ServiceName
	ImageEnvName       = "GITOPS_AI_CONTROLLER_IMAGE"
	DefaultImage        = "ghcr.io/svghadi/gitops-ai-controller:latest"
	SkillsImageEnvName  = "GITOPS_AI_SKILLS_IMAGE"
	DefaultSkillsImage  = "ghcr.io/redhat-developer/agentic-skills:latest"
	TLSEnv              = "GITOPS_AI_CONTROLLER_TLS_ENABLED"
)

func NewServiceAccount(namespace string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ServiceName,
			Namespace: namespace,
		},
	}
}

func NewClusterRole() *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: ServiceName,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"argoproj.io"},
				Resources: []string{"applications"},
				Verbs:     []string{"get", "patch"},
			},
			{
				APIGroups: []string{"authentication.k8s.io"},
				Resources: []string{"tokenreviews"},
				Verbs:     []string{"create"},
			},
		},
	}
}

func NewClusterRoleBinding(namespace string) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: ServiceName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      ServiceName,
				Namespace: namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     ServiceName,
		},
	}
}

func NewRole(namespace string) *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ServiceName,
			Namespace: namespace,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"agentic.openshift.io"},
				Resources: []string{"agenticruns"},
				Verbs:     []string{"create", "get", "list", "watch"},
			},
			{
				APIGroups: []string{"agentic.openshift.io"},
				Resources: []string{"analysisresults"},
				Verbs:     []string{"get"},
			},
		},
	}
}

func NewRoleBinding(namespace, saNamespace string) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ServiceName,
			Namespace: namespace,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      ServiceName,
				Namespace: saNamespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     ServiceName,
		},
	}
}

func NewDeployment(namespace, argocdNamespace string, tlsEnabled bool) *appsv1.Deployment {
	image := os.Getenv(ImageEnvName)
	if image == "" {
		image = DefaultImage
	}

	replicas := int32(1)
	labels := map[string]string{
		"app.kubernetes.io/name":      ServiceName,
		"app.kubernetes.io/component": "gitops-ai-controller",
		"app.kubernetes.io/part-of":   "gitops-operator",
	}

	var args []string
	var port int32
	var portName string
	var volumeMounts []corev1.VolumeMount
	var volumes []corev1.Volume

	if tlsEnabled {
		port = 8443
		portName = "https"
		args = []string{"--addr=:8443", "--tls-cert=/etc/gitops-ai-controller/tls/tls.crt", "--tls-key=/etc/gitops-ai-controller/tls/tls.key"}
		volumeMounts = []corev1.VolumeMount{
			{
				Name:      "tls",
				MountPath: "/etc/gitops-ai-controller/tls",
				ReadOnly:  true,
			},
		}
		mode := int32(0644)
		volumes = []corev1.Volume{
			{
				Name: "tls",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName:  ServiceName + "-tls",
						DefaultMode: &mode,
					},
				},
			},
		}
	} else {
		port = 8080
		portName = "http"
		args = []string{"--addr=:8080", "--tls-enabled=false"}
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ServiceName,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: ServiceName,
					Containers: []corev1.Container{
						{
							Name:            ServiceName,
							Image:           image,
							ImagePullPolicy: corev1.PullAlways,
							Args:            args,
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: port,
									Name:          portName,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Env: []corev1.EnvVar{
								{Name: "ARGOCD_NAMESPACE", Value: argocdNamespace},
								{Name: "GITOPS_AI_AGENTIC_RUN_NAMESPACE", Value: envOrDefault("GITOPS_AI_AGENTIC_RUN_NAMESPACE", "openshift-lightspeed")},
								{Name: "GITOPS_AI_DIAGNOSIS_COOLDOWN", Value: envOrDefault("GITOPS_AI_DIAGNOSIS_COOLDOWN", "10m")},
								{Name: "GITOPS_AI_DIAGNOSIS_UNHEALTHY_DURATION", Value: envOrDefault("GITOPS_AI_DIAGNOSIS_UNHEALTHY_DURATION", "10m")},
								{Name: "GITOPS_AI_DIAGNOSIS_MAX_ATTEMPTS", Value: envOrDefault("GITOPS_AI_DIAGNOSIS_MAX_ATTEMPTS", "2")},
								{Name: SkillsImageEnvName, Value: envOrDefault(SkillsImageEnvName, DefaultSkillsImage)},
							},
							VolumeMounts: volumeMounts,
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resourcev1.MustParse("10m"),
									corev1.ResourceMemory: resourcev1.MustParse("32Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resourcev1.MustParse("100m"),
									corev1.ResourceMemory: resourcev1.MustParse("128Mi"),
								},
							},
						},
					},
					Volumes: volumes,
				},
			},
		},
	}
}

func NewService(namespace string, tlsEnabled bool) *corev1.Service {
	var port int32
	var portName string
	annotations := map[string]string{}

	if tlsEnabled {
		port = 8443
		portName = "https"
		annotations["service.beta.openshift.io/serving-cert-secret-name"] = ServiceName + "-tls"
	} else {
		port = 8080
		portName = "http"
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        ServiceName,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app.kubernetes.io/name": ServiceName,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       portName,
					Port:       port,
					TargetPort: intstr.FromInt32(port),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
