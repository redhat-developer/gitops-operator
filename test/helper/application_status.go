package helper

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	framework "github.com/operator-framework/operator-sdk/pkg/test"
	corev1 "k8s.io/api/core/v1"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	argoapp "github.com/argoproj-labs/argocd-operator/pkg/apis/argoproj/v1alpha1"
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

// waitForResourcesByName will wait up to 'timeout' minutes for a set of resources to exist; the resources
// should be of the given type (Deployment, Service, etc) and name(s).
// Returns error if the resources could not be found within the given time frame.
func WaitForResourcesByName(resourceList []ResourceList, namespace string, timeout time.Duration, t *testing.T) error {

	f := framework.Global

	// Wait X seconds for all the resources to be created
	err := wait.Poll(time.Second*1, timeout, func() (bool, error) {

		for _, resourceListEntry := range resourceList {

			for _, resourceName := range resourceListEntry.ExpectedResources {

				resource := resourceListEntry.Resource.DeepCopyObject()
				namespacedName := types.NamespacedName{Name: resourceName, Namespace: namespace}
				if err := f.Client.Get(context.TODO(), namespacedName, resource); err != nil {
					t.Logf("Unable to retrieve expected resource %s: %v", resourceName, err)
					return false, nil
				} else {
					t.Logf("Able to retrieve %s", resourceName)
				}
			}

		}

		return true, nil
	})

	return err
}

// resourceList is used by waitForResourcesByName
type ResourceList struct {
	// resource is the type of resource to verify that it exists
	Resource runtime.Object

	// expectedResources are the names of the resources of the above type
	ExpectedResources []string
}

const (
	StandaloneArgoCDNamespace = "gitops-standalone-test"
)

// ensureCleanSlate runs before the tests, to ensure that the cluster is in the expected pre-test state
func EnsureCleanSlate(t *testing.T) error {

	t.Log("Running ensureCleanSlate")

	return DeleteNamespace(StandaloneArgoCDNamespace, t)

}

// DeleteNamespace deletes a namespace, and waits for deletion to complete.
func DeleteNamespace(nsToDelete string, t *testing.T) error {
	f := framework.Global

	// Delete the standaloneArgoCDNamespace namespace and wait for it to not exist
	nsTarget := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: nsToDelete,
		},
	}
	err := f.Client.Delete(context.Background(), nsTarget)
	if err != nil {
		if kubeerrors.IsNotFound(err) {
			// Success: the namespace doesn't exist.
			return nil
		}
		return fmt.Errorf("unable to delete namespace %v", err)
	}

	err = wait.Poll(1*time.Second, 2*time.Minute, func() (bool, error) {

		// Patch all the ArgoCDs in the NS, to remove the finalizer (so the namespace can be deleted)
		var list argoapp.ArgoCDList

		opts := &client.ListOptions{
			Namespace: nsToDelete,
		}
		if err = f.Client.List(context.Background(), &list, opts); err != nil {
			t.Errorf("Unable to list ArgoCDs %v", err)
			// Report failure, but still continue
		}
		for _, item := range list.Items {
			item.Finalizers = []string{}
			if err := f.Client.Update(context.Background(), &item); err != nil {
				t.Errorf("Unable to update ArgoCD application finalizer on '%s': %v", item.Name, err)
				// Report failure, but still continue
			}
		}

		if err := f.Client.Get(context.Background(), types.NamespacedName{Name: nsTarget.Name},
			nsTarget); kubeerrors.IsNotFound(err) {
			t.Logf("Namespace '%s' no longer exists", nsTarget.Name)
			return true, nil
		}

		t.Logf("Namespace '%s' still exists", nsTarget.Name)

		return false, nil
	})

	if err != nil {
		return fmt.Errorf("namespace '%s' was not deleted: %v", nsToDelete, err)
	}

	return nil

}
