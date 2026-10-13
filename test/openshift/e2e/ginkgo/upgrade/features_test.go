/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package upgrade

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	rolloutmanagerv1alpha1 "github.com/argoproj-labs/argo-rollouts-manager/api/v1alpha1"
	argov1beta1api "github.com/argoproj-labs/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/argoproj/argo-cd/gitops-engine/pkg/health"
	argocdv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	applicationFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/application"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	configmapFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/configmap"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	routeFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/route"
	statefulsetFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/statefulset"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Where the nginx example ends up: deployed by the Application, by the ApplicationSet,
// and by the Rollout.
var workloadNamespaces = []string{"upgrade-app", "upgrade-appset", "upgrade-rollouts"}

// readConfig returns every object in config/*.yaml, in file name order.
func readConfig() []*unstructured.Unstructured {
	files, err := filepath.Glob("config/*.yaml")
	Expect(err).ToNot(HaveOccurred())
	Expect(files).ToNot(BeEmpty(), "no configuration found; the suite must run from its own directory")

	var objects []*unstructured.Unstructured
	for _, file := range files {
		f, err := os.Open(file)
		Expect(err).ToNot(HaveOccurred())
		defer f.Close()

		decoder := yaml.NewYAMLOrJSONDecoder(f, 4096)
		for {
			obj := &unstructured.Unstructured{}
			err := decoder.Decode(obj)
			if errors.Is(err, io.EOF) {
				break
			}
			Expect(err).ToNot(HaveOccurred(), file)
			if len(obj.Object) > 0 {
				objects = append(objects, obj)
			}
		}
	}
	return objects
}

// applyConfig applies config/*.yaml the way 'oc apply --server-side' would. Each object
// is retried for a while, because some kinds only exist once an earlier object has been
// reconciled (the Rollout CRD is installed when the RolloutManager is created).
func applyConfig() {
	for _, obj := range readConfig() {
		By("applying " + obj.GetKind() + " " + obj.GetNamespace() + "/" + obj.GetName())
		Eventually(func() error {
			return k8sClient.Apply(ctx, client.ApplyConfigurationFromUnstructured(obj.DeepCopy()),
				client.FieldOwner("upgrade-test"), client.ForceOwnership)
		}, "5m", "5s").Should(Succeed())
	}
}

// configSpecs returns the .spec of every object in config/ as it is on the cluster now.
func configSpecs() map[string]any {
	specs := map[string]any{}
	for _, obj := range readConfig() {
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed())
		specs[obj.GetKind()+" "+obj.GetNamespace()+"/"+obj.GetName()] = obj.Object["spec"]
	}
	return specs
}

// workloadPods returns the UID of every nginx Pod. A Pod that was deleted and recreated
// comes back with a different UID.
func workloadPods() map[string]types.UID {
	pods := map[string]types.UID{}
	for _, ns := range workloadNamespaces {
		var podList corev1.PodList
		Expect(k8sClient.List(ctx, &podList, client.InNamespace(ns), client.MatchingLabels{"app": "nginx"})).To(Succeed())
		for _, pod := range podList.Items {
			pods[pod.Namespace+"/"+pod.Name] = pod.UID
		}
	}
	return pods
}

func waitForWorkloadsToBeReady(namespace string) {
	fixture.WaitForAllDeploymentsInTheNamespaceToBeReady(namespace, k8sClient)
	fixture.WaitForAllStatefulSetsInTheNamespaceToBeReady(namespace, k8sClient)
}

// verifyFeatures checks that everything config/ asks for is there and working. It runs
// twice, unchanged: before the upgrade and after it.
func verifyFeatures() {

	By("default instance: is available, and has the memory limit we set on its application controller")

	defaultArgoCD := &argov1beta1api.ArgoCD{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops", Namespace: "openshift-gitops"}}
	Eventually(defaultArgoCD, "10m", "10s").Should(argocdFixture.BeAvailable())
	Eventually(func() string {
		return applicationControllerMemoryLimit("openshift-gitops", "openshift-gitops-application-controller")
	}, "5m", "10s").Should(Equal("3Gi"))

	By("second instance: is available, with every component running")

	argoCD := &argov1beta1api.ArgoCD{ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: "upgrade-argocd"}}
	Eventually(argoCD, "10m", "10s").Should(argocdFixture.BeAvailable())
	Eventually(argoCD, "5m", "10s").Should(And(
		argocdFixture.HaveServerStatus("Running"),
		argocdFixture.HaveRepoStatus("Running"),
		argocdFixture.HaveRedisStatus("Running"),
		argocdFixture.HaveApplicationControllerStatus("Running"),
		argocdFixture.HaveApplicationSetControllerStatus("Running"),
		argocdFixture.HaveNotificationControllerStatus("Running"),
		argocdFixture.HaveSSOStatus("Running"),
	))

	By("second instance: Redis runs in HA mode")

	Eventually(&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "argocd-redis-ha-server", Namespace: "upgrade-argocd"}}, "5m", "10s").
		Should(statefulsetFixture.HaveReadyReplicas(3))
	Eventually(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argocd-redis-ha-haproxy", Namespace: "upgrade-argocd"}}, "5m", "10s").
		Should(deploymentFixture.HaveReadyReplicas(3))

	By("second instance: ApplicationSet, notifications and Dex are deployed, and the server Route is admitted")

	for _, name := range []string{"argocd-applicationset-controller", "argocd-notifications-controller", "argocd-dex-server"} {
		Eventually(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "upgrade-argocd"}}, "5m", "10s").
			Should(deploymentFixture.HaveReadyReplicas(1), name)
	}
	Eventually(&routev1.Route{ObjectMeta: metav1.ObjectMeta{Name: "argocd-server", Namespace: "upgrade-argocd"}}, "5m", "10s").
		Should(routeFixture.HaveAdmittedIngress())

	By("second instance: settings from the ArgoCD CR are in the generated ConfigMaps and workloads")

	argocdCM := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "argocd-cm", Namespace: "upgrade-argocd"}}
	Eventually(argocdCM, "5m", "10s").Should(And(
		configmapFixture.HaveStringDataKeyValue("kustomize.buildOptions", "--enable-helm"),
		configmapFixture.HaveStringDataKeyValue("upgrade-test.marker", "kept"),
		configmapFixture.HaveNonEmptyDataKey("resource.customizations.health.example.com_Widget"),
	))
	argocdRBACCM := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "argocd-rbac-cm", Namespace: "upgrade-argocd"}}
	Eventually(argocdRBACCM, "5m", "10s").Should(And(
		configmapFixture.HaveStringDataKeyValue("policy.default", "role:readonly"),
		configmapFixture.HaveStringDataKeyValueContainsSubstring("policy.csv", "upgrade-test-admins"),
	))
	Eventually(func() string {
		return applicationControllerMemoryLimit("upgrade-argocd", "argocd-application-controller")
	}, "5m", "10s").Should(Equal("2Gi"))

	By("second instance: has cluster-wide permissions, from ARGOCD_CLUSTER_CONFIG_NAMESPACES on the Subscription")

	clusterRoleName := "argocd-upgrade-argocd-argocd-application-controller"
	Eventually(&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName}}, "5m", "10s").Should(k8sFixture.ExistByName())
	Eventually(&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName}}, "5m", "10s").Should(k8sFixture.ExistByName())

	By("managed namespaces: the second instance has a Role and RoleBinding in each")

	for _, ns := range []string{"upgrade-app", "upgrade-appset"} {
		Eventually(&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-application-controller", Namespace: ns}}, "5m", "10s").
			Should(k8sFixture.ExistByName())
		Eventually(&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "argocd-argocd-application-controller", Namespace: ns}}, "5m", "10s").
			Should(k8sFixture.ExistByName())
	}

	By("applications: the Application, and the one generated by the ApplicationSet, are Synced and Healthy")

	for _, name := range []string{"nginx", "nginx-appset"} {
		app := &argocdv1alpha1.Application{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "upgrade-argocd"}}
		Eventually(app, "10m", "10s").Should(And(
			applicationFixture.HaveSyncStatusCode(argocdv1alpha1.SyncStatusCodeSynced),
			applicationFixture.HaveHealthStatusCode(health.HealthStatusHealthy),
		), name)
	}

	By("rollouts: the RolloutManager is available and the Rollout is Healthy")

	Eventually(func() rolloutmanagerv1alpha1.RolloutControllerPhase {
		rolloutManager := &rolloutmanagerv1alpha1.RolloutManager{ObjectMeta: metav1.ObjectMeta{Name: "rollout-manager", Namespace: "upgrade-rollouts"}}
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(rolloutManager), rolloutManager); err != nil {
			GinkgoWriter.Println(err)
			return ""
		}
		return rolloutManager.Status.Phase
	}, "5m", "10s").Should(Equal(rolloutmanagerv1alpha1.PhaseAvailable))

	Eventually(func() string {
		rollout := &unstructured.Unstructured{}
		rollout.SetAPIVersion("argoproj.io/v1alpha1")
		rollout.SetKind("Rollout")
		if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: "upgrade-rollouts", Name: "nginx"}, rollout); err != nil {
			GinkgoWriter.Println(err)
			return ""
		}
		phase, _, _ := unstructured.NestedString(rollout.Object, "status", "phase")
		return phase
	}, "5m", "10s").Should(Equal("Healthy"))
}

func applicationControllerMemoryLimit(namespace string, statefulSetName string) string {
	statefulSet := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: statefulSetName, Namespace: namespace}}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(statefulSet), statefulSet); err != nil {
		GinkgoWriter.Println(err)
		return ""
	}
	return statefulSet.Spec.Template.Spec.Containers[0].Resources.Limits.Memory().String()
}
