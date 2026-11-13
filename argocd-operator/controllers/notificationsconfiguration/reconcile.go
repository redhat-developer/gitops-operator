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

package notificationsconfiguration

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1alpha1"
)

// reconcileNotificationsConfigurationResources will reconcile all the resources for the given CR.
func (r *NotificationsConfigurationReconciler) reconcileNotificationsConfigurationResources(cr *v1alpha1.NotificationsConfiguration) error {
	if err := r.reconcileNotificationsConfigmap(cr); err != nil {
		return err
	}
	return nil
}

// setResourceWatches will register Watches for each of the supported Resources.
func setResourceWatches(bld *builder.Builder) *builder.Builder {
	bld.For(&v1alpha1.NotificationsConfiguration{})
	bld.Owns(&corev1.ConfigMap{})
	// When the AI-managed NC CR changes or is deleted, re-trigger reconciliation
	// of the default NC CR so it can rebuild the ConfigMap without AI entries.
	bld.Watches(&v1alpha1.NotificationsConfiguration{}, handler.EnqueueRequestsFromMapFunc(
		func(_ context.Context, obj client.Object) []reconcile.Request {
			if obj.GetName() != aiNotificationsConfigurationName {
				return nil
			}
			return []reconcile.Request{{
				NamespacedName: types.NamespacedName{
					Name:      defaultNotificationsConfigurationName,
					Namespace: obj.GetNamespace(),
				},
			}}
		},
	))
	return bld
}
