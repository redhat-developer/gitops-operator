package subscription

import (
	"context"

	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	olmv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/gomega"
)

func GetEnv(s *olmv1alpha1.Subscription, key string) (*string, error) {

	k8sClient, _, err := utils.GetE2ETestKubeClientWithError()
	if err != nil {
		return nil, err
	}

	if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(s), s); err != nil {
		return nil, err
	}
	if s.Spec == nil {
		return nil, nil
	}

	if s.Spec.Config == nil {
		return nil, nil
	}

	if s.Spec.Config.Env == nil {
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
				// replace with the value from the param
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

func RemoveEnv(subscription *olmv1alpha1.Subscription, key string) {

	Update(subscription, func(s *olmv1alpha1.Subscription) {

		if s.Spec == nil {
			return
		}

		if s.Spec.Config == nil {
			return
		}

		if s.Spec.Config.Env == nil {
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

// RemoveSpecConfig removes any configuration data (environment variables) specified under .spec.config of Subscription
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
		// Retrieve the latest version of the object
		err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(obj), obj)
		if err != nil {
			return err
		}

		modify(obj)

		// Attempt to update the object
		return k8sClient.Update(context.Background(), obj)
	})
	Expect(err).ToNot(HaveOccurred())

}
