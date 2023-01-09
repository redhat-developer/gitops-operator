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

package controllers

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	// embed the Argo icon during compile time
	_ "embed"

	"github.com/go-logr/logr"
	console "github.com/openshift/api/console/v1"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/redhat-developer/gitops-operator/common"
	"github.com/redhat-developer/gitops-operator/controllers/util"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	argocdNS        = "openshift-gitops"
	consoleLinkName = "argocd"
	argocdRouteName = "openshift-gitops-server"
)

var (
	encodedArgoImage string

	//go:embed argocd/img/argo.png
	argoImage []byte
)

func init() {
	encodedArgoImage = imageDataURL(base64.StdEncoding.EncodeToString(argoImage))
}

// if DISABLE_DEFAULT_ARGOCD_CONSOLELINK env variable is true, Argo CD ConsoleLink will be deleted
func isConsoleLinkDisabled() bool {
	return strings.ToLower(os.Getenv(common.DisableDefaultArgoCDConsoleLink)) == "true"
}

// SetupWithManager sets up the controller with the Manager.
func (r *ReconcileArgoCDRoute) SetupWithManager(mgr ctrl.Manager) error {
	// Watch for changes to argocd-server route in the default argocd instance namespace
	// The ConsoleLink holds the route URL and should be regenerated when route is updated

	return ctrl.NewControllerManagedBy(mgr).
		For(&routev1.Route{}, builder.WithPredicates(filterPredicate(filterArgoCDRoute))).
		Complete(r)
}

func filterPredicate(assert func(namespace, name string) bool) predicate.Funcs {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return assert(e.ObjectNew.GetNamespace(), e.ObjectNew.GetName()) &&
				e.ObjectNew.GetResourceVersion() != e.ObjectOld.GetResourceVersion()
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return assert(e.Object.GetNamespace(), e.Object.GetName())
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return assert(e.Object.GetNamespace(), e.Object.GetName())
		},
	}
}

func filterArgoCDRoute(namespace, name string) bool {
	return namespace == argocdNS && argocdRouteName == name
}

// blank assignment to verify that ReconcileArgoCDRoute implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileArgoCDRoute{}

// ReconcileArgoCDRoute reconciles a ArgoCD Route object
type ReconcileArgoCDRoute struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	Client client.Client
	Scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a ArgoCD Route object and makes changes based on the state read
// and what is in the Route.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileArgoCDRoute) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	var logs = logf.Log.WithName("controller_argocd_route")
	reqLogger := logs.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling ArgoCD Route")

	// Stop, if OpenShift Console API is unavailable
	if !util.IsConsoleAPIFound() {
		reqLogger.Info("Skipping reconcile: OpenShift Console API not found")
		return reconcile.Result{}, nil
	}

	// Fetch the Argo CD server route object (represents the URL to access ArgoCD console).
	argoCDRoute, err := getArgoCDRoute(r.Client, ctx, reqLogger)
	if err != nil {
		return reconcile.Result{}, err
	}

	if argoCDRoute == nil {
		// if argocd-server route is not found or deleted, remove the ConsoleLink if present
		if err := r.deleteConsoleLinkIfPresent(ctx, reqLogger); err != nil {
			reqLogger.Error(err, "Failed to delete ConsoleLink")
			return reconcile.Result{}, err
		}
	}

	// Get the URL, or stop if it's empty.
	if argoCDRoute.Spec.Host == "" {
		reqLogger.Info("Skipping reconcile: Argo CD route host is empty")
		return reconcile.Result{}, nil
	}
	argoCDRouteURL := fmt.Sprintf("https://%s", argoCDRoute.Spec.Host)
	reqLogger.Info("Route found for argocd-server", "Route.Spec.Host", argoCDRouteURL)

	// Helper code: checks whether the ConsoleLink already exists in the Kubernetes API server,
	// and prepares an "expected" ConsoleLink for comparison (see ACTIONS below).
	consoleLink := newConsoleLink(argoCDRouteURL, "Cluster Argo CD")
	found := &console.ConsoleLink{}
	err = r.Client.Get(ctx, types.NamespacedName{Name: consoleLink.Name}, found)
	consoleLinkExists := err == nil // true if err is nil (i.e., if the ConsoleLink was found), and false otherwise.
	if !consoleLinkExists && !errors.IsNotFound(err) {
		reqLogger.Error(err, "consoleLink exists but failed get it.", "consoleLink.Name", consoleLink.Name)
		return reconcile.Result{}, err
	}

	// If the ConsoleLink exists, take one of the following (a,b,c) actions:
	if consoleLinkExists {
		// a. If the ConsoleLink is disabled, delete it.
		if isConsoleLinkDisabled() {
			return reconcile.Result{}, r.deleteConsoleLinkIfPresent(ctx, reqLogger)
		}
		// b. If the ConsoleLink URL is different from the Argo CD server route URL, update the ConsoleLink.
		if found.Spec.Href != argoCDRouteURL {
			reqLogger.Info("Updating the existing ConsoleLink", "ConsoleLink.Name", consoleLink.Name)
			found.Spec.Href = argoCDRouteURL
			return reconcile.Result{}, r.Client.Update(ctx, found)
		}
		// c. If the ConsoleLink URL is already correct, do nothing.
		reqLogger.Info("Skip reconcile: ConsoleLink already exists", "consoleLink.Name", consoleLink.Name)
		return reconcile.Result{}, nil
	}

	// If the ConsoleLink does not exist and is enabled, create it.
	if !isConsoleLinkDisabled() {
		reqLogger.Info("Creating a new ConsoleLink", "ConsoleLink.Name", consoleLink.Name)
		return reconcile.Result{}, r.Client.Create(ctx, consoleLink)
	}

	// If the ConsoleLink does not exist and is disabled, do nothing.
	return reconcile.Result{}, nil
}

// newConsoleLink returns a new ConsoleLink object with the specified href and text.
func newConsoleLink(href, text string) *console.ConsoleLink {
	return &console.ConsoleLink{
		ObjectMeta: metav1.ObjectMeta{
			Name: consoleLinkName,
		},
		Spec: console.ConsoleLinkSpec{
			Link: console.Link{
				Text: text,
				Href: href,
			},
			Location: console.ApplicationMenu,
			ApplicationMenu: &console.ApplicationMenuSpec{
				Section:  "OpenShift GitOps",
				ImageURL: encodedArgoImage,
			},
		},
	}
}

// deleteConsoleLinkIfPresent deletes the ConsoleLink object if it exists in the Kubernetes API server.
// If it doesn't exist, it simply returns nil. If there is any other error, it returns the error.
func (r *ReconcileArgoCDRoute) deleteConsoleLinkIfPresent(ctx context.Context, log logr.Logger) error {
	// Check if the ConsoleLink object exists.
	consoleLink := &console.ConsoleLink{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: consoleLinkName}, consoleLink)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	// The ConsoleLink object exists - delete it.
	log.Info("Deleting ConsoleLink", "consoleLink.Name", consoleLinkName)
	return r.Client.Delete(ctx, consoleLink)
}

// imageDataURL returns the data URL of the image data in PNG format.
func imageDataURL(data string) string {
	return fmt.Sprintf("data:image/png;base64,%s", data)
}

// getArgoCDRoute fetches the Argo CD server route object from the Kubernetes API server.
func getArgoCDRoute(client client.Client, ctx context.Context, logger logr.Logger) (*routev1.Route, error) {
	// Define the name and namespace of the route to fetch
	routeName := types.NamespacedName{Name: argocdRouteName, Namespace: argocdNS}

	// Create a new Route object to hold the fetched route
	argoCDRoute := &routev1.Route{}

	// Fetch the route from the Kubernetes API server
	err := client.Get(ctx, routeName, argoCDRoute)
	if err != nil {
		// If the route is not found, this is not considered to be an error
		// so return nil for the Route object and nil for the error as well
		if errors.IsNotFound(err) {
			logger.Info("ArgoCD server route not found", "Route.Namespace", argocdNS)
			return nil, nil
		}
		// If another error occurred, return nil for the Route object and the error
		return nil, err
	}

	// Return the fetched Route object and nil for the error
	return argoCDRoute, nil
}
