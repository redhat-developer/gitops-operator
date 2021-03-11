package controller

import (
	keycloak "github.com/keycloak/keycloak-operator/pkg/controller/keycloak"
	keycloakclient "github.com/keycloak/keycloak-operator/pkg/controller/keycloakclient"
	keycloakrealm "github.com/keycloak/keycloak-operator/pkg/controller/keycloakrealm"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, keycloak.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, keycloakclient.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, keycloakrealm.Add)
}
