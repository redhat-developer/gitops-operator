package subscription

import (
	"context"
	"fmt"
	"time"

	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	olmv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils" // Ensure this path is correct
	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// PollForSubscriptionCurrentCSV waits for the Subscription to have a CurrentCSV in its status.
// It returns the CurrentCSV name once found.
func PollForSubscriptionCurrentCSV(ctx context.Context, subNamespace, subName string, timeout, pollingInterval time.Duration) string {
	k8sClient, _, err := utils.GetE2ETestKubeClientWithError()
	Expect(err).ToNot(HaveOccurred())

	GinkgoWriter.Printf("Polling for Subscription '%s/%s' to have a CurrentCSV...\n", subNamespace, subName)
	subKey := client.ObjectKey{
		Name:      subName,
		Namespace: subNamespace,
	}
	var sub olmv1alpha1.Subscription
	var csvName string

	Eventually(func() bool {
		getErr := k8sClient.Get(ctx, subKey, &sub)
		if getErr != nil {
			GinkgoWriter.Printf("Error getting Subscription '%s/%s': %v. Retrying...\n", subNamespace, subName, getErr)
			return false
		}
		if sub.Status.CurrentCSV != "" {
			csvName = sub.Status.CurrentCSV
			GinkgoWriter.Printf("Subscription '%s/%s' has CurrentCSV: %s\n", subNamespace, subName, csvName)
			return true
		}
		GinkgoWriter.Printf("Subscription '%s/%s' does not have CurrentCSV yet. Status state: %s. Retrying...\n", subNamespace, subName, sub.Status.State)
		return false
	}).WithTimeout(timeout).WithPolling(pollingInterval).Should(BeTrue(),
		fmt.Sprintf("Expected Subscription '%s/%s' to have a CurrentCSV within %s", subNamespace, subName, timeout))

	return csvName
}

// GetEnv retrieves the value of an environment variable from a Subscription's spec.config.env.
func GetEnv(s *olmv1alpha1.Subscription, key string) (*string, error) {
	k8sClient, _, err := utils.GetE2ETestKubeClientWithError()
	if err != nil {
		return nil, err
	}
	if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(s), s); err != nil {
		return nil, err
	}
	if s.Spec == nil || s.Spec.Config == nil || s.Spec.Config.Env == nil {
		return nil, nil
	}
	for idx := range s.Spec.Config.Env {
		idxEnv := s.Spec.Config.Env[idx]
		if idxEnv.Name == key {
			return &idxEnv.Value, nil
		}
	}
	return nil, nil
}

// SetEnv sets or updates an environment variable in a Subscription's spec.config.env.
func SetEnv(subscription *olmv1alpha1.Subscription, key string, value string) {
	Update(subscription, func(s *olmv1alpha1.Subscription) {
		if s.Spec == nil {
			s.Spec = &olmv1alpha1.SubscriptionSpec{}
		}
		if s.Spec.Config == nil {
			s.Spec.Config = &olmv1alpha1.SubscriptionConfig{}
		}
		if s.Spec.Config.Env == nil {
			s.Spec.Config.Env = []corev1.EnvVar{}
		}
		newEnvVars := []corev1.EnvVar{}
		match := false
		for idx := range s.Spec.Config.Env {
			currEnv := s.Spec.Config.Env[idx]
			if currEnv.Name == key {
				newEnvVars = append(newEnvVars, corev1.EnvVar{Name: key, Value: value})
				match = true
			} else {
				newEnvVars = append(newEnvVars, currEnv)
			}
		}
		if !match {
			newEnvVars = append(newEnvVars, corev1.EnvVar{Name: key, Value: value})
		}
		s.Spec.Config.Env = newEnvVars
	})
}

// RemoveEnv removes an environment variable from a Subscription's spec.config.env.
func RemoveEnv(subscription *olmv1alpha1.Subscription, key string) {
	Update(subscription, func(s *olmv1alpha1.Subscription) {
		if s.Spec == nil || s.Spec.Config == nil || s.Spec.Config.Env == nil {
			return
		}
		newEnvVars := []corev1.EnvVar{}
		for idx := range s.Spec.Config.Env {
			currEnv := s.Spec.Config.Env[idx]
			if currEnv.Name == key {
				// skip
			} else {
				newEnvVars = append(newEnvVars, currEnv)
			}
		}
		s.Spec.Config.Env = newEnvVars
	})
}

// RemoveSpecConfig removes any configuration data (environment variables) specified under .spec.config of Subscription.
func RemoveSpecConfig(sub *olmv1alpha1.Subscription) {
	Update(sub, func(s *olmv1alpha1.Subscription) {
		if s.Spec != nil {
			s.Spec.Config = nil
		}
	})
}

// Update will keep trying to update object until it succeeds, or times out.
func Update(obj *olmv1alpha1.Subscription, modify func(*olmv1alpha1.Subscription)) {
	k8sClient, _ := utils.GetE2ETestKubeClient()

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(obj), obj)
		if err != nil {
			return err
		}
		modify(obj)
		return k8sClient.Update(context.Background(), obj)
	})
	Expect(err).ToNot(HaveOccurred())
}
