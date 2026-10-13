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
	"os"
	"slices"

	argov1beta1api "github.com/argoproj-labs/gitops-operator/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	olmv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	operatorNamespace = "openshift-gitops-operator"
	operatorPackage   = "openshift-gitops-operator"

	// Part of the configuration under test, but it lives on the Subscription rather than
	// in config/. The first gives the Argo CD instance in upgrade-argocd cluster-wide
	// permissions. The second allows a RolloutManager outside openshift-gitops.
	clusterConfigNamespaces   = "openshift-gitops,upgrade-argocd"
	clusterRolloutsNamespaces = "upgrade-rollouts"
)

// The catalog the operator is installed from. Both channels must be in it.
func catalogSource() string {
	if v := os.Getenv("CATALOG_SOURCE"); v != "" {
		return v
	}
	return "redhat-operators"
}

func operatorSubscription() *olmv1alpha1.Subscription {
	return &olmv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{Name: operatorPackage, Namespace: operatorNamespace},
	}
}

// The suite installs the operator itself, so it refuses to run on a cluster that has it.
func expectOperatorIsNotInstalled() {
	var subs olmv1alpha1.SubscriptionList
	Expect(k8sClient.List(ctx, &subs)).To(Succeed())

	for _, sub := range subs.Items {
		Expect(sub.Spec.Package).ToNot(Equal(operatorPackage),
			"the operator is already installed (Subscription %s/%s); this suite needs a cluster without it", sub.Namespace, sub.Name)
	}
}

// channelHead returns the name of the newest CSV in the channel, as the catalog reports it.
func channelHead(channel string) string {
	packageManifests := &unstructured.UnstructuredList{}
	packageManifests.SetAPIVersion("packages.operators.coreos.com/v1")
	packageManifests.SetKind("PackageManifestList")
	Expect(k8sClient.List(ctx, packageManifests,
		client.InNamespace("openshift-marketplace"), client.MatchingLabels{"catalog": catalogSource()})).To(Succeed())

	for _, packageManifest := range packageManifests.Items {
		if packageManifest.GetName() != operatorPackage {
			continue
		}
		channels, _, _ := unstructured.NestedSlice(packageManifest.Object, "status", "channels")
		for _, c := range channels {
			if c.(map[string]any)["name"] == channel {
				return c.(map[string]any)["currentCSV"].(string)
			}
		}
	}
	Fail("channel " + channel + " of " + operatorPackage + " not found in CatalogSource " + catalogSource())
	return ""
}

// targetCSV is the CSV the operator should end up on: the head of the channel, or
// exactly 'version' if it is set.
func targetCSV(channel string, version string) string {
	if version != "" {
		return operatorPackage + ".v" + version
	}
	return channelHead(channel)
}

// installOperator installs the head of the channel, or exactly 'version' if it is set.
func installOperator(channel string, version string) {

	target := targetCSV(channel, version)

	By("creating the operator Namespace, OperatorGroup and Subscription for " + target + " from channel " + channel)

	Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:   operatorNamespace,
		Labels: map[string]string{"openshift.io/cluster-monitoring": "true"},
	}})).To(Succeed())

	operatorGroup := &unstructured.Unstructured{}
	operatorGroup.SetAPIVersion("operators.coreos.com/v1")
	operatorGroup.SetKind("OperatorGroup")
	operatorGroup.SetName(operatorPackage)
	operatorGroup.SetNamespace(operatorNamespace)
	Expect(k8sClient.Create(ctx, operatorGroup)).To(Succeed())

	sub := operatorSubscription()
	sub.Spec = &olmv1alpha1.SubscriptionSpec{
		Package:                operatorPackage,
		Channel:                channel,
		CatalogSource:          catalogSource(),
		CatalogSourceNamespace: "openshift-marketplace",
		// Manual, so that the operator moves only when this test approves an InstallPlan.
		// With Automatic it would not stop at a pinned version.
		InstallPlanApproval: olmv1alpha1.ApprovalManual,
		Config: &olmv1alpha1.SubscriptionConfig{
			Env: []corev1.EnvVar{
				{Name: "ARGOCD_CLUSTER_CONFIG_NAMESPACES", Value: clusterConfigNamespaces},
				{Name: "CLUSTER_SCOPED_ARGO_ROLLOUTS_NAMESPACES", Value: clusterRolloutsNamespaces},
			},
		},
	}
	if version != "" {
		sub.Spec.StartingCSV = target
	}
	Expect(k8sClient.Create(ctx, sub)).To(Succeed())

	By("approving InstallPlans until " + target + " is installed")
	approveInstallPlansUntilInstalled(target)

	By("waiting for the operator to create the default Argo CD instance")
	defaultArgoCD := &argov1beta1api.ArgoCD{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops", Namespace: "openshift-gitops"}}
	Eventually(defaultArgoCD, "10m", "10s").Should(k8sFixture.ExistByName())
	Eventually(defaultArgoCD, "10m", "10s").Should(argocdFixture.BeAvailable())
}

// upgradeOperator moves the Subscription to the channel and upgrades to its head, or to
// exactly 'version' if it is set.
func upgradeOperator(channel string, version string) {

	sub := operatorSubscription()
	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(sub), sub)).To(Succeed())
	previousCSV := sub.Status.InstalledCSV

	target := targetCSV(channel, version)
	Expect(target).ToNot(Equal(previousCSV), "the operator is already on %s, there is nothing to upgrade", target)

	By("switching the Subscription from " + sub.Spec.Channel + " to " + channel)
	sub.Spec.Channel = channel
	Expect(k8sClient.Update(ctx, sub)).To(Succeed())

	// The catalog may take the operator there through intermediate versions, one
	// InstallPlan each.
	By("approving InstallPlans until " + previousCSV + " is upgraded to " + target)
	approveInstallPlansUntilInstalled(target)

	// A Succeeded CSV only means the new operator Pod is running. Wait until it has
	// rolled out its own Argo CD image to both instances, otherwise the checks that
	// follow could pass against workloads the new operator has not touched yet.
	By("waiting for the upgraded operator to update the Argo CD workloads")
	Eventually(func(g Gomega) {
		image := operatorArgoCDImage()
		g.Expect(image).ToNot(BeEmpty(), "ARGOCD_IMAGE not found on the operator Deployment")
		g.Expect(serverImage("openshift-gitops", "openshift-gitops-server")).To(Equal(image))
		g.Expect(serverImage("upgrade-argocd", "argocd-server")).To(Equal(image))
	}, "10m", "10s").Should(Succeed(), "the upgraded operator did not roll out its Argo CD image")

	waitForWorkloadsToBeReady("openshift-gitops")
	waitForWorkloadsToBeReady("upgrade-argocd")
}

// approveInstallPlansUntilInstalled approves whichever InstallPlan the Subscription is
// waiting on, one at a time, until the CSV named 'target' is installed and has succeeded.
func approveInstallPlansUntilInstalled(target string) {

	sub := operatorSubscription()

	// Once the plan that installs the target is approved, approve nothing more. OLM
	// offers the next version as soon as one exists, often before the target has
	// finished installing, and approving that would carry a pinned version past its pin.
	targetApproved := false

	Eventually(func() bool {
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(sub), sub); err != nil {
			GinkgoWriter.Println(err)
			return false
		}
		GinkgoWriter.Printf("subscription: state=%q installedCSV=%q\n", sub.Status.State, sub.Status.InstalledCSV)
		for _, condition := range sub.Status.Conditions {
			if condition.Status == corev1.ConditionTrue {
				GinkgoWriter.Printf("subscription: %s: %s\n", condition.Type, condition.Message)
			}
		}

		// Are we there yet?
		if sub.Status.InstalledCSV == target {
			csv := &olmv1alpha1.ClusterServiceVersion{}
			csvKey := client.ObjectKey{Namespace: operatorNamespace, Name: target}
			if err := k8sClient.Get(ctx, csvKey, csv); err == nil && csv.Status.Phase == olmv1alpha1.CSVPhaseSucceeded {
				return true
			}
		}

		// Not yet: approve the InstallPlan the Subscription points at, if it needs it.
		if sub.Status.InstallPlanRef != nil && !targetApproved {
			plan := &olmv1alpha1.InstallPlan{}
			planKey := client.ObjectKey{Namespace: operatorNamespace, Name: sub.Status.InstallPlanRef.Name}
			if err := k8sClient.Get(ctx, planKey, plan); err == nil && !plan.Spec.Approved {
				GinkgoWriter.Println("approving InstallPlan", plan.Name, "for", plan.Spec.ClusterServiceVersionNames)
				plan.Spec.Approved = true
				if err := k8sClient.Update(ctx, plan); err != nil {
					GinkgoWriter.Println(err)
				} else if slices.Contains(plan.Spec.ClusterServiceVersionNames, target) {
					targetApproved = true
				}
			}
		}
		return false

	}, "20m", "10s").Should(BeTrue(), "the operator did not reach "+target)
}

// operatorArgoCDImage returns the Argo CD image the installed operator deploys, which
// it carries in the ARGOCD_IMAGE variable of its own Deployment.
func operatorArgoCDImage() string {
	depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-operator-controller-manager", Namespace: operatorNamespace}}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(depl), depl); err != nil {
		GinkgoWriter.Println(err)
		return ""
	}
	for _, container := range depl.Spec.Template.Spec.Containers {
		for _, env := range container.Env {
			if env.Name == "ARGOCD_IMAGE" {
				return env.Value
			}
		}
	}
	return ""
}

func serverImage(namespace string, deploymentName string) string {
	depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: deploymentName, Namespace: namespace}}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(depl), depl); err != nil {
		GinkgoWriter.Println(err)
		return ""
	}
	return depl.Spec.Template.Spec.Containers[0].Image
}
