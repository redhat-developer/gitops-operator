// Copyright 2019 ArgoCD Operator Developers
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package argocd

import (
	corev1 "k8s.io/api/core/v1"
)

// mergeOperatorVolumes combines operator-managed volumes with user-supplied
// volumes. A user volume whose name matches an operator-managed volume
// replaces it, so administrators can override internally managed emptyDir
// volumes with a Generic Ephemeral Volume (ephemeral.volumeClaimTemplate) or
// another volume source. User volumes with new names are appended. Operator
// volume order is preserved.
func mergeOperatorVolumes(operatorVolumes, userVolumes []corev1.Volume) []corev1.Volume {
	if len(userVolumes) == 0 {
		return operatorVolumes
	}

	userByName := make(map[string]corev1.Volume, len(userVolumes))
	for _, volume := range userVolumes {
		userByName[volume.Name] = volume
	}

	merged := make([]corev1.Volume, 0, len(operatorVolumes)+len(userVolumes))
	seen := make(map[string]struct{}, len(operatorVolumes))
	for _, volume := range operatorVolumes {
		seen[volume.Name] = struct{}{}
		if override, ok := userByName[volume.Name]; ok {
			merged = append(merged, override)
			continue
		}
		merged = append(merged, volume)
	}
	for _, volume := range userVolumes {
		if _, ok := seen[volume.Name]; ok {
			continue
		}
		merged = append(merged, volume)
	}
	return merged
}
