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
	"strings"

	argoproj "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
)

// ArgoCDIdledAnnotationKey is set on the ArgoCD CR by an external Idler controller to request
// that gitops-operator scale the workloads it manages for this CR down to 0 replicas.
const ArgoCDIdledAnnotationKey = "gitops-operator.openshift.io/idled"

// isIdled reports whether the ArgoCD CR has been marked idle by the Idler controller.
func isIdled(cr *argoproj.ArgoCD) bool {
	if cr == nil || cr.Annotations == nil {
		return false
	}
	v, ok := cr.Annotations[ArgoCDIdledAnnotationKey]
	if !ok {
		return false
	}
	return v == "" || strings.EqualFold(v, "true")
}
