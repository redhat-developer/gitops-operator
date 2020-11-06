package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GitopsServiceSpec defines the desired state of GitopsService
type GitopsServiceSpec struct {
	// Add a prefix to environment names(dev,stage,prod,etc.) to distinguish and identify individual environments
	Prefix string `json:"prefix,omitempty"`
}

// GitopsServiceStatus defines the observed state of GitopsService
type GitopsServiceStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GitopsService is the Schema for the gitopsservices API
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=gitopsservices,scope=Cluster
type GitopsService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GitopsServiceSpec   `json:"spec,omitempty"`
	Status GitopsServiceStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GitopsServiceList contains a list of GitopsService
type GitopsServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GitopsService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GitopsService{}, &GitopsServiceList{})
}
