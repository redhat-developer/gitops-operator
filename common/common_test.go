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

package common

import (
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestGetImagePullPolicy(t *testing.T) {
	tests := []struct {
		name           string
		crPolicy       corev1.PullPolicy
		envValue       string
		expectedPolicy corev1.PullPolicy
	}{
		{
			name:           "CR policy takes precedence",
			crPolicy:       corev1.PullIfNotPresent,
			envValue:       "Never",
			expectedPolicy: corev1.PullIfNotPresent,
		},
		{
			name:           "Environment variable used when CR policy empty",
			crPolicy:       "",
			envValue:       "IfNotPresent",
			expectedPolicy: corev1.PullIfNotPresent,
		},
		{
			name:           "Environment variable Never",
			crPolicy:       "",
			envValue:       "Never",
			expectedPolicy: corev1.PullNever,
		},
		{
			name:           "Environment variable Always",
			crPolicy:       "",
			envValue:       "Always",
			expectedPolicy: corev1.PullAlways,
		},
		{
			name:           "Default to IfNotPresent when no config",
			crPolicy:       "",
			envValue:       "",
			expectedPolicy: corev1.PullIfNotPresent,
		},
		{
			name:           "Invalid env value defaults to IfNotPresent",
			crPolicy:       "",
			envValue:       "InvalidValue",
			expectedPolicy: corev1.PullIfNotPresent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable
			if tt.envValue != "" {
				os.Setenv(ImagePullPolicy, tt.envValue)
			} else {
				os.Unsetenv(ImagePullPolicy)
			}
			defer os.Unsetenv(ImagePullPolicy)

			result := GetImagePullPolicy(tt.crPolicy)
			if result != tt.expectedPolicy {
				t.Errorf("GetImagePullPolicy() = %v, want %v", result, tt.expectedPolicy)
			}
		})
	}
}
