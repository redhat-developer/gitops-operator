package openshift

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	argoapp "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	"github.com/argoproj-labs/argocd-operator/controllers/argocd"
	"github.com/argoproj-labs/argocd-operator/controllers/argoutil"
	"github.com/go-logr/logr"
	"golang.org/x/mod/semver"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var log = logf.Log.WithName("openshift_controller_argocd")

// func init() {
// 	argocd.Register(reconcilerHook)
// }

func ReconcilerHook(cr *argoapp.ArgoCD, v interface{}, hint string) error {

	logv := log.WithValues("ArgoCD Namespace", cr.Namespace, "ArgoCD Name", cr.Name)
	switch o := v.(type) {
	case *rbacv1.ClusterRole:
		if o.Name == argocd.GenerateUniqueResourceName("argocd-application-controller", cr) {
			logv.Info("configuring openshift cluster config policy rules")
			o.Rules = policyRulesForClusterConfig()
		}
	case *appsv1.Deployment:
		switch o.Name {
		case cr.Name + "-redis":
			logv.Info("configuring openshift redis")
			o.Spec.Template.Spec.Containers[0].Args = append(getArgsForRedhatRedis(), o.Spec.Template.Spec.Containers[0].Args...)
		case cr.Name + "-redis-ha-haproxy":
			logv.Info("configuring openshift redis haproxy")
			o.Spec.Template.Spec.Containers[0].Command = append(getCommandForRedhatRedisHaProxy(), o.Spec.Template.Spec.Containers[0].Command...)
			version := hint
			// The Red Hat haproxy image sets the net_bind_service capability on the binary.  For 4.11
			// we need to add this to the capabilities.  For earlier versions, the default SCCs
			// won't let us add capabilities so we remove the "drop all" capability.
			if version == "" || semver.Compare(fmt.Sprintf("v%s", version), "v4.10.999") > 0 {
				o.Spec.Template.Spec.Containers[0].SecurityContext.Capabilities.Add = []corev1.Capability{
					"NET_BIND_SERVICE",
				}
			} else {
				o.Spec.Template.Spec.Containers[0].SecurityContext.Capabilities = nil
			}
		case cr.Name + "-repo-server":

			prodImage := o.Spec.Template.Spec.Containers[0].Image
			usingReleasedImages := strings.Contains(prodImage, "registry.redhat.io/openshift-gitops-1/argocd-rhel")
			if cr.Spec.Repo.SystemCATrust != nil && usingReleasedImages {
				updateSystemCATrustBuilding(cr, o, prodImage, logv)
			}
		}
	case *appsv1.StatefulSet:
		if o.Name == cr.Name+"-redis-ha-server" {
			logv.Info("configuring openshift redis-ha-server stateful set")
			for index := range o.Spec.Template.Spec.Containers {
				switch o.Spec.Template.Spec.Containers[index].Name {
				case "redis":
					o.Spec.Template.Spec.Containers[index].Args = getArgsForRedhatHaRedisServer()
					o.Spec.Template.Spec.Containers[index].Command = []string{}
				case "sentinel":
					o.Spec.Template.Spec.Containers[index].Args = getArgsForRedhatHaRedisSentinel()
					o.Spec.Template.Spec.Containers[index].Command = []string{}
				}
			}
			o.Spec.Template.Spec.InitContainers[0].Args = getArgsForRedhatHaRedisInitContainer()
			o.Spec.Template.Spec.InitContainers[0].Command = []string{}
		}
	case *corev1.Secret:
		if allowedNamespace(cr.Namespace, os.Getenv("ARGOCD_CLUSTER_CONFIG_NAMESPACES")) {
			logv.Info("configuring cluster secret with empty namespaces to allow cluster resources")
			delete(o.Data, "namespaces")
		}
	case *rbacv1.Role:
		if o.Name == cr.Name+"-"+"argocd-application-controller" {
			logv.Info("configuring policy rule for Application Controller")

			// can move this to somewhere common eventually, maybe init()
			k8sClient, err := initK8sClient()
			if err != nil {
				logv.Error(err, "failed to initialize kube client")
				return err
			}

			clusterRole, err := k8sClient.RbacV1().ClusterRoles().Get(context.TODO(), "admin", metav1.GetOptions{})

			if err != nil {
				logv.Error(err, "failed to retrieve Cluster Role admin")
				return err
			}
			policyRules := getPolicyRuleForApplicationController()
			policyRules = append(policyRules, clusterRole.Rules...)
			o.Rules = policyRules
		}
	}
	return nil
}

// updateSystemCATrustBuilding replaces the procedure based on ubuntu container with one based on rhel containers.
// This requires changing the init container image, its script and the mount points to all consuming containers.
func updateSystemCATrustBuilding(cr *argoapp.ArgoCD, o *appsv1.Deployment, prodImage string, logv logr.Logger) {
	// These volumes are created by argocd-operator
	volumeSource := "argocd-ca-trust-source"
	volumeTarget := "argocd-ca-trust-target"

	// Drop upstream init container and replace it with rhel specific logic
	o.Spec.Template.Spec.InitContainers = slices.DeleteFunc(
		o.Spec.Template.Spec.InitContainers,
		func(container corev1.Container) bool {
			return container.Name == "update-ca-certificates"
		},
	)

	initContainer := corev1.Container{
		Name:            "update-ca-certificates",
		Image:           prodImage,
		SecurityContext: argoutil.DefaultSecurityContext(),
		VolumeMounts: []corev1.VolumeMount{
			{Name: volumeSource, MountPath: "/var/run/secrets/ca-trust-source", ReadOnly: true},
			{Name: volumeTarget, MountPath: "/etc/pki/ca-trust"},
		},
		Command: []string{"/bin/bash", "-c"},
		Args: []string{`
set -eEuo pipefail
trap 's=$?; echo >&2 "$0: Error on line "$LINENO": $BASH_COMMAND"; exit $s' ERR

# Populate the empty volume with the expected structure
mkdir -p /etc/pki/ca-trust/{extracted/{openssl,pem,java,edk2},source/{anchors,blacklist}}

# Copy user anchors where update-ca-trust expects it
# Using loop over 'cp *' to work well with no CA files provided (all optional, none configured, etc.)
ls /var/run/secrets/ca-trust-source/ | while read -r f; do
    cp -L "/var/run/secrets/ca-trust-source/$f" /etc/pki/ca-trust/source/anchors/
done

echo "User defined trusted CA files:"
ls /etc/pki/ca-trust/source/anchors/

update-ca-trust

echo "Trusted anchors:"
trust list

echo "Done!"
			`},
	}

	// Replace distro CA certs with empty volume when the image CA's are supposed to be dropped
	if cr.Spec.Repo.SystemCATrust.DropImageCertificates {
		o.Spec.Template.Spec.Volumes = append(o.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: "distro-ca-trust-source",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
		initContainer.VolumeMounts = append(initContainer.VolumeMounts, corev1.VolumeMount{
			Name:      "distro-ca-trust-source",
			MountPath: "/usr/share/pki/ca-trust-source/",
		})
	}
	o.Spec.Template.Spec.InitContainers = append(o.Spec.Template.Spec.InitContainers, initContainer)

	// Update where to mount for prod containers
	var mountedTo []string
	for ci, container := range o.Spec.Template.Spec.Containers {
		// Only mount to production container or sidecars using the same image
		if container.Image != prodImage {
			continue
		}
		mountedTo = append(mountedTo, container.Name)

		// The source volume is not needed on RHEL
		o.Spec.Template.Spec.Containers[ci].VolumeMounts = slices.DeleteFunc(
			o.Spec.Template.Spec.Containers[ci].VolumeMounts,
			func(mount corev1.VolumeMount) bool {
				return mount.Name == volumeSource
			},
		)
		// Use the RHEL-specific mount point for the target trust volume
		for vi, volume := range o.Spec.Template.Spec.Containers[ci].VolumeMounts {
			if volume.Name == volumeTarget {
				o.Spec.Template.Spec.Containers[ci].VolumeMounts[vi].MountPath = "/etc/pki/ca-trust"
			}
		}
	}
	logv.Info(fmt.Sprintf("injected systemCATrust to repo-server containers: %s", strings.Join(mountedTo, ",")))
}

// BuilderHook updates the Argo CD controller builder to watch for changes to the "admin" ClusterRole
func BuilderHook(_ *argoapp.ArgoCD, v interface{}, _ string) error {
	logv := log.WithValues("module", "builder-hook")

	bldr, ok := v.(*argocd.BuilderHook)
	if !ok {
		return nil
	}

	logv.Info("updating the Argo CD controller to watch for changes to the admin ClusterRole")

	clusterResourceHandler := handler.EnqueueRequestsFromMapFunc(adminClusterRoleMapper(bldr.Client))
	bldr.Watches(&rbacv1.ClusterRole{}, clusterResourceHandler,
		builder.WithPredicates(predicate.NewPredicateFuncs(func(o client.Object) bool {
			return o.GetName() == "admin"
		})))

	return nil
}

func getPolicyRuleForApplicationController() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{
				"*",
			},
			Resources: []string{
				"*",
			},
			Verbs: []string{
				"get",
				"list",
				"watch",
			},
		},
		{
			APIGroups: []string{
				"monitoring.coreos.com",
			},
			Resources: []string{
				"*",
			},
			Verbs: []string{
				"*",
			},
		},
		{
			APIGroups: []string{
				"argoproj.io",
			},
			Resources: []string{
				"applications",
				"applicationsets",
				"appprojects",
				"argocds",
			},
			Verbs: []string{
				"*",
			},
		},
	}
}

// For OpenShift, we use a custom build of Redis provided by Red Hat
// which requires additional args in comparison to stock redis.
func getArgsForRedhatRedis() []string {
	return []string{
		"redis-server",
		"--protected-mode",
		"no",
	}
}

// For OpenShift, we use a custom build of haproxy provided by Red Hat
// which requires a command as opposed to args in stock haproxy.
func getCommandForRedhatRedisHaProxy() []string {
	return []string{
		"haproxy",
		"-f",
		"/usr/local/etc/haproxy/haproxy.cfg",
	}
}

// For OpenShift, we use a custom build of Redis provided by Red Hat
// which requires additional args in comparison to stock redis.
func getArgsForRedhatHaRedisServer() []string {
	return []string{
		"redis-server",
		"/data/conf/redis.conf",
	}
}

// For OpenShift, we use a custom build of Redis provided by Red Hat
// which requires additional args in comparison to stock redis.
func getArgsForRedhatHaRedisSentinel() []string {
	return []string{
		"redis-sentinel",
		"/data/conf/sentinel.conf",
	}
}

// For OpenShift, we use a custom build of Redis provided by Red Hat
// which requires additional args in comparison to stock redis.
func getArgsForRedhatHaRedisInitContainer() []string {
	return []string{
		"sh",
		"/readonly-config/init.sh",
	}
}

// policyRulesForClusterConfig defines rules for cluster config.
func policyRulesForClusterConfig() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{
				"*",
			},
			Resources: []string{
				"*",
			},
			Verbs: []string{
				"get",
				"list",
				"watch",
			},
		},
		{
			NonResourceURLs: []string{
				"*",
			},
			Verbs: []string{
				"get",
				"list",
			},
		},
		{
			APIGroups: []string{
				"operators.coreos.com",
			},
			Resources: []string{
				"*",
			},
			Verbs: []string{
				"*",
			},
		},
		{
			APIGroups: []string{
				"operator.openshift.io",
			},
			Resources: []string{
				"*",
			},
			Verbs: []string{
				"*",
			},
		},
		{
			APIGroups: []string{
				"user.openshift.io",
			},
			Resources: []string{
				"*",
			},
			Verbs: []string{
				"*",
			},
		},
		{
			APIGroups: []string{
				"config.openshift.io",
			},
			Resources: []string{
				"*",
			},
			Verbs: []string{
				"*",
			},
		},
		{
			APIGroups: []string{
				"console.openshift.io",
			},
			Resources: []string{
				"*",
			},
			Verbs: []string{
				"*",
			},
		},
		{
			APIGroups: []string{
				"",
			},
			Resources: []string{
				"namespaces",
				"persistentvolumeclaims",
				"persistentvolumes",
				"configmaps",
			},
			Verbs: []string{
				"*",
			},
		}, {
			APIGroups: []string{
				"rbac.authorization.k8s.io",
			},
			Resources: []string{
				"*",
			},
			Verbs: []string{
				"*",
			},
		}, {
			APIGroups: []string{
				"storage.k8s.io",
			},
			Resources: []string{
				"*",
			},
			Verbs: []string{
				"*",
			},
		}, {
			APIGroups: []string{
				"machine.openshift.io",
			},
			Resources: []string{
				"*",
			},
			Verbs: []string{
				"*",
			},
		}, {
			APIGroups: []string{
				"machineconfiguration.openshift.io",
			},
			Resources: []string{
				"*",
			},
			Verbs: []string{
				"*",
			},
		}, {
			APIGroups: []string{
				"compliance.openshift.io",
			},
			Resources: []string{
				"scansettingbindings",
			},
			Verbs: []string{
				"*",
			},
		},
		{
			APIGroups: []string{
				"",
			},
			Resources: []string{
				"serviceaccounts",
			},
			Verbs: []string{
				"impersonate",
			},
		},
	}
}

func allowedNamespace(current string, namespaces string) bool {

	clusterConfigNamespaces := splitList(namespaces)
	if len(clusterConfigNamespaces) > 0 {
		if clusterConfigNamespaces[0] == "*" {
			return true
		}

		for _, n := range clusterConfigNamespaces {
			if n == current {
				return true
			}
		}
	}
	return false
}

func splitList(s string) []string {
	elems := strings.Split(s, ",")
	for i := range elems {
		elems[i] = strings.TrimSpace(elems[i])
	}
	return elems
}

func initK8sClient() (*kubernetes.Clientset, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}

	kClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	return kClient, nil
}

// adminClusterRoleMapper maps changes to the "admin" ClusterRole to all Argo CD instances in the cluster
func adminClusterRoleMapper(k8sClient client.Client) handler.MapFunc {
	return func(ctx context.Context, o client.Object) []reconcile.Request {
		var result = []reconcile.Request{}

		// Only process the "admin" ClusterRole
		if o.GetName() != "admin" {
			return result
		}

		// Get all Argo CD instances in all namespaces
		argocds := &argoapp.ArgoCDList{}
		if err := k8sClient.List(ctx, argocds, &client.ListOptions{}); err != nil {
			log.Error(err, "failed to list Argo CD instances for admin ClusterRole mapping")
			return result
		}

		// Create reconcile requests for all Argo CD instances
		for _, argocd := range argocds.Items {
			namespacedName := client.ObjectKey{
				Name:      argocd.Name,
				Namespace: argocd.Namespace,
			}
			result = append(result, reconcile.Request{NamespacedName: namespacedName})
		}

		return result
	}
}
