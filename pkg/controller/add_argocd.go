package controller

import (
	argocdprovisioner "github.com/argoproj-labs/argocd-operator/pkg/controller/argocd"
	"github.com/redhat-developer/gitops-operator/pkg/controller/argocd"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, argocd.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, argocdprovisioner.Add)
}
