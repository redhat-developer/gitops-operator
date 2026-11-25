package ai

const (
	EnrollmentConfigMapName  = "gitops-ai-controller-instances"
	EnrollmentManagedByLabel = "gitops.openshift.io/managed-by"
	EnrollmentManager        = "gitops-ai-controller"
)

// InstanceEnrollment binds an instance and its notification identity after the
// operator successfully configures AI notifications. Entries are stored by
// ArgoCD namespace in the operator namespace's enrollment ConfigMap.
type InstanceEnrollment struct {
	Name              string `json:"name"`
	UID               string `json:"uid"`
	ServiceAccountUID string `json:"serviceAccountUID"`
}
