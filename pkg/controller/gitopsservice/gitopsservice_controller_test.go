package gitopsservice

import (
	"os"
	"testing"

	pipelinesv1alpha1 "github.com/redhat-developer/gitops-operator/pkg/apis/pipelines/v1alpha1"
)

func TestImageFromEnvVariable(t *testing.T) {
	cr := &pipelinesv1alpha1.GitopsService{}

	t.Run("Image present as env variable", func(t *testing.T) {
		image := "quay.io/org/test"
		os.Setenv(backendImageEnvName, image)

		deployment := newDeploymentForCR(cr)

		got := deployment.Spec.Template.Spec.Containers[0].Image
		if got != image {
			t.Errorf("Image mismatch: got %s, want %s", got, image)
		}
		assertNoError(t, os.Unsetenv(backendImageEnvName))
	})
	t.Run("env variable for image not found", func(t *testing.T) {
		deployment := newDeploymentForCR(cr)

		got := deployment.Spec.Template.Spec.Containers[0].Image
		if got != backendImage {
			t.Errorf("Image mismatch: got %s, want %s", got, backendImage)
		}
	})
}

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
