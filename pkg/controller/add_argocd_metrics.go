package controller

import (
	"github.com/redhat-developer/gitops-operator/pkg/controller/argocdmetrics"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, argocdmetrics.Add)
}
