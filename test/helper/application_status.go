/*
Copyright 2021.

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

package helper

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	argoapp "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	corev1 "k8s.io/api/core/v1"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	StandaloneArgoCDNamespace = "gitops-standalone-test"
)

// ProjectExists return true if the AppProject exists in the namespace,
// false otherwise (with an error, if available).
func ProjectExists(projectName string, namespace string) (bool, error) {
	var stdout, stderr bytes.Buffer
	ocPath, err := exec.LookPath("oc")
	if err != nil {
		return false, err
	}

	cmd := exec.Command(ocPath, "get", "appproject/"+projectName, "-n", namespace)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("oc command failed. Stdout: %s, Stderr: %s", stdout.String(), stderr.String())
	}

	return true, nil
}

// ApplicationHealthStatus returns an error if the application is not 'Healthy'
func ApplicationHealthStatus(appname string, namespace string) error {
	var stdout, stderr bytes.Buffer
	ocPath, err := exec.LookPath("oc")
	if err != nil {
		return err
	}

	cmd := exec.Command(ocPath, "get", "application/"+appname, "-n", namespace, "-o", "jsonpath='{.status.health.status}'")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("oc command failed: %s%s", stdout.String(), stderr.String())
	}

	if output := strings.TrimSpace(stdout.String()); output != "'Healthy'" {
		return fmt.Errorf("application '%s' health is %s", appname, output)
	}

	return nil
}

// ApplicationSyncStatus returns an error if the application is not 'Synced'
func ApplicationSyncStatus(appname string, namespace string) error {
	var stdout, stderr bytes.Buffer
	ocPath, err := exec.LookPath("oc")
	if err != nil {
		return err
	}

	cmd := exec.Command(ocPath, "get", "application/"+appname, "-n", namespace, "-o", "jsonpath='{.status.sync.status}'")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("oc command failed: %s%s", stdout.String(), stderr.String())
	}

	if output := strings.TrimSpace(stdout.String()); output != "'Synced'" {
		return fmt.Errorf("application '%s' status is %s", appname, output)
	}

	return nil
}

// ResourceList is used by waitForResourcesByName
type ResourceList struct {
	// resource is the type of resource to verify that it exists
	Resource client.Object

	// expectedResources are the names of the resources of the above type
	ExpectedResources []string
}

// WaitForResourcesByName will wait up to 'timeout' minutes for a set of resources to exist; the resources
// should be of the given type (Deployment, Service, etc) and name(s).
// Returns error if the resources could not be found within the given time frame.
func WaitForResourcesByName(k8sClient client.Client, resourceList []ResourceList, namespace string, timeout time.Duration) error {
	// Wait X seconds for all the resources to be created
	err := wait.Poll(time.Second*1, timeout, func() (bool, error) {
		for _, resourceListEntry := range resourceList {
			for _, resourceName := range resourceListEntry.ExpectedResources {
				resource := resourceListEntry.Resource
				namespacedName := types.NamespacedName{Name: resourceName, Namespace: namespace}
				if err := k8sClient.Get(context.TODO(), namespacedName, resource); err != nil {
					log.Printf("Unable to retrieve expected resource %s: %v", resourceName, err)
					return false, nil
				}
				log.Printf("Able to retrieve %s: %s", resource.GetObjectKind().GroupVersionKind().Kind, resourceName)
			}
		}
		return true, nil
	})
	return err
}

// EnsureCleanSlate runs before the tests, to ensure that the cluster is in the expected pre-test state
func EnsureCleanSlate(k8sClient client.Client) error {

	GinkgoT().Log("Running ensureCleanSlate")

	return DeleteNamespace(k8sClient, StandaloneArgoCDNamespace)

}

// DeleteNamespace deletes a namespace, and waits for deletion to complete.
func DeleteNamespace(k8sClient client.Client, nsToDelete string) error {
	// Delete the standaloneArgoCDNamespace namespace and wait for it to not exist
	nsTarget := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsToDelete,
		},
	}
	err := k8sClient.Delete(context.Background(), nsTarget)
	if err != nil {
		if kubeerrors.IsNotFound(err) {
			// Success: the namespace doesn't exist.
			return nil
		}
		return fmt.Errorf("unable to delete namespace %v", err)
	}

	err = wait.Poll(1*time.Second, 5*time.Minute, func() (bool, error) {

		// Patch all the ArgoCDs in the NS, to remove the finalizer (so the namespace can be deleted)
		var list argoapp.ArgoCDList

		opts := &client.ListOptions{
			Namespace: nsToDelete,
		}
		if err = k8sClient.List(context.Background(), &list, opts); err != nil {
			GinkgoT().Errorf("Unable to list ArgoCDs %v", err)
			// Report failure, but still continue
		}
		for _, item := range list.Items {

			if len(item.Finalizers) == 0 {
				continue
			}

			item.Finalizers = []string{}
			GinkgoT().Logf("Updating ArgoCD operand '%s' to remove finalizers, for deletion.", item.Name)
			err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: item.Namespace, Name: item.Name}, &item)
				if err != nil {
					if kubeerrors.IsNotFound(err) {
						return nil
					}
					return err
				}
				item.Finalizers = []string{}
				return k8sClient.Update(context.Background(), &item)
			})
			if err != nil {
				GinkgoT().Errorf("Unable to update ArgoCD application finalizer on '%s': %v", item.Name, err)
				// Report failure, but still continue
			}
		}

		if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: nsTarget.Name},
			nsTarget); kubeerrors.IsNotFound(err) {
			GinkgoT().Logf("Namespace '%s' no longer exists", nsTarget.Name)
			return true, nil
		}

		GinkgoT().Logf("Namespace '%s' still exists (finalizers: %v)", nsTarget.Name, len(list.Items))

		return false, nil
	})

	if err != nil {
		return fmt.Errorf("namespace '%s' was not deleted: %v", nsToDelete, err)
	}

	return nil

}
