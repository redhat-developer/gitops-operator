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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GitopsServiceSpec defines the desired state of GitopsService
type GitopsServiceSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// InfraNodeEnabled will add infra NodeSelector to all the default workloads of gitops operator
	RunOnInfra bool `json:"runOnInfra,omitempty"`
	// Tolerations allow the default workloads to schedule onto nodes with matching taints
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// NodeSelector is a map of key value pairs used for node selection in the default workloads
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// ConsolePlugin defines the Resource configuration for the Console Plugin components
	ConsolePlugin *ConsolePluginStruct `json:"consolePlugin,omitempty"`
	// ImagePullPolicy defines the image pull policy for GitOps workloads
	// +kubebuilder:validation:Enum=Always;IfNotPresent;Never
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`
}

// ConsolePluginStruct defines the resource configuration for the Console Plugin components
type ConsolePluginStruct struct {
	// Backend defines the resource requests and limits for the backend service
	Backend *BackendStruct `json:"backend,omitempty"`
	// GitopsPlugin defines the resource requests and limits for the gitops plugin service
	GitopsPlugin *GitopsPluginStruct `json:"gitopsPlugin,omitempty"`
}

// BackendStruct defines the resource configuration for the Backend components
type BackendStruct struct {
	// Resources defines the resource requests and limits for the backend service
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// GitopsPluginStruct defines the resource configuration for the Gitops Plugin components
type GitopsPluginStruct struct {
	// Resources defines the resource requests and limits for the gitops plugin service
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// GitopsServiceStatus defines the observed state of GitopsService
type GitopsServiceStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// GitopsService is the Schema for the gitopsservices API
type GitopsService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GitopsServiceSpec   `json:"spec,omitempty"`
	Status GitopsServiceStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// GitopsServiceList contains a list of GitopsService
type GitopsServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GitopsService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GitopsService{}, &GitopsServiceList{})
}
