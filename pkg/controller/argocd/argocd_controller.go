package argocd

import (
	"context"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"log"

	"github.com/go-logr/logr"
	console "github.com/openshift/api/console/v1"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/rakyll/statik/fs"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	// register the statik zip content data
	_ "github.com/redhat-developer/gitops-operator/pkg/controller/argocd/statik"
)

var logs = logf.Log.WithName("controller_argocd_route")

const (
	argocdNS           = "openshift-gitops"
	depracatedArgoCDNS = "openshift-pipelines-app-delivery"
	consoleLinkName    = "argocd"
	argocdRouteName    = "openshift-gitops-server"
	iconFilePath       = "/argo.png"
)

//go:generate statik --src ./img -f
var image string

func init() {
	image = imageDataURL(base64.StdEncoding.EncodeToString(readStatikImage()))
}

// Add creates a new ArgoCD Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileArgoCDRoute{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {

	reqLogger := logs.WithValues()
	reqLogger.Info("Watching ArgoCD Server Route")

	c, err := controller.New("argocd-route-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to argocd-server route in argocd namespace
	// The ConsoleLink holds the route URL and should be regenerated when route is updated
	err = c.Watch(&source.Kind{Type: &routev1.Route{}}, &handler.EnqueueRequestForObject{}, filterPredicate(filterArgoCDRoute))
	if err != nil {
		return err
	}

	reqLogger.Info("Created controller for ArgoCD server route")
	return nil
}

func filterPredicate(assert func(namespace, name string) bool) predicate.Funcs {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return assert(e.MetaNew.GetNamespace(), e.MetaNew.GetName()) &&
				e.MetaNew.GetResourceVersion() != e.MetaOld.GetResourceVersion()
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return assert(e.Meta.GetNamespace(), e.Meta.GetName())
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return assert(e.Meta.GetNamespace(), e.Meta.GetName())
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
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a ArgoCD Route object and makes changes based on the state read
// and what is in the Route.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileArgoCDRoute) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := logs.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling ArgoCD Route")

	ctx := context.Background()

	// Fetch ArgoCD server route
	argoCDRoute := &routev1.Route{}
	err := r.client.Get(ctx, types.NamespacedName{Name: argocdRouteName, Namespace: argocdNS}, argoCDRoute)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("ArgoCD server route not found", "Route.Namespace", argocdNS)
			// if argocd-server route is deleted, remove the ConsoleLink if present
			return reconcile.Result{}, r.deleteConsoleLinkIfPresent(ctx, reqLogger)
		}
		return reconcile.Result{}, err
	}
	reqLogger.Info("Route found for argocd-server", "Route.Host", argoCDRoute.Spec.Host)

	argocCDRouteURL := fmt.Sprintf("https://%s", argoCDRoute.Spec.Host)

	consoleLink := newConsoleLink(argocCDRouteURL, "ArgoCD")

	found := &console.ConsoleLink{}
	err = r.client.Get(ctx, types.NamespacedName{Name: consoleLink.Name}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Creating a new ConsoleLink", "ConsoleLink.Name", consoleLink.Name)
			return reconcile.Result{}, r.client.Create(ctx, consoleLink)
		}
		reqLogger.Error(err, "Failed to create ConsoleLink", "ConsoleLink.Name", consoleLink.Name)
		return reconcile.Result{}, err
	}

	if found.Spec.Href != argocCDRouteURL {
		reqLogger.Info("Updating the existing ConsoleLink", "ConsoleLink.Name", consoleLink.Name)
		found.Spec.Href = argocCDRouteURL
		return reconcile.Result{}, r.client.Update(ctx, found)
	}

	reqLogger.Info("Skip reconcile: ConsoleLink already exists", "ConsoleLink.Name", consoleLink.Name)
	return reconcile.Result{}, nil
}

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
				ImageURL: image,
			},
		},
	}
}

func (r *ReconcileArgoCDRoute) deleteConsoleLinkIfPresent(ctx context.Context, log logr.Logger) error {
	err := r.client.Get(ctx, types.NamespacedName{Name: consoleLinkName}, &console.ConsoleLink{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	log.Info("Deleting ConsoleLink", "ConsoleLink.Name", consoleLinkName)
	return r.client.Delete(ctx, &console.ConsoleLink{ObjectMeta: metav1.ObjectMeta{Name: consoleLinkName}})
}

func readStatikImage() []byte {
	statikFs, err := fs.New()
	if err != nil {
		log.Fatalf("Failed to create a new statik filesystem: %v", err)
	}
	file, err := statikFs.Open(iconFilePath)
	if err != nil {
		log.Fatalf("Failed to open ArgoCD icon file: %v", err)
	}
	defer file.Close()
	data, err := ioutil.ReadAll(file)
	if err != nil {
		log.Fatalf("Failed to read ArgoCD icon file: %v", err)
	}
	return data
}

func imageDataURL(data string) string {
	return fmt.Sprintf("data:image/png;base64,%s", data)
}
