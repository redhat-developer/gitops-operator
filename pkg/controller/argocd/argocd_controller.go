package argocd

import (
	"context"

	argoprojv1alpha1 "github.com/argoproj-labs/argocd-operator/pkg/apis/argoproj/v1alpha1"
	"github.com/go-logr/logr"
	console "github.com/openshift/api/console/v1"
	routev1 "github.com/openshift/api/route/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_argocd")

const (
	argocdNS           = "argocd"
	consoleLinkName    = "argocd-application"
	argocdInstanceName = "argocd"
	argocdRouteName    = "argocd-server"
)

// Add creates a new ArgoCD Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileArgoCD{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("argocd-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource ArgoCD
	err = c.Watch(&source.Kind{Type: &argoprojv1alpha1.ArgoCD{}}, &handler.EnqueueRequestForObject{}, filterPredicate(assertArgoCD))
	if err != nil {
		return err
	}

	// Watch for changes to argocd-server route in argocd namespace
	// The ConsoleLink holds the route URL and should be regenerated when route is updated
	err = c.Watch(&source.Kind{Type: &routev1.Route{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &argoprojv1alpha1.ArgoCD{},
	}, filterPredicate(assertArgoCDRoute))
	if err != nil {
		return err
	}

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

func assertArgoCD(namespace, name string) bool {
	return namespace == argocdNS && argocdInstanceName == name
}

func assertArgoCDRoute(namespace, name string) bool {
	return namespace == argocdNS && argocdRouteName == name
}

// blank assignment to verify that ReconcileArgoCD implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileArgoCD{}

// ReconcileArgoCD reconciles a ArgoCD object
type ReconcileArgoCD struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a ArgoCD object and makes changes based on the state read
// and what is in the ArgoCD.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileArgoCD) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling ArgoCD")

	ctx := context.Background()

	// Fetch the ArgoCD instance
	argocdInstance := &argoprojv1alpha1.ArgoCD{}
	err := r.client.Get(ctx, request.NamespacedName, argocdInstance)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("ArgoCD instance not found")
			// if argocd instance is deleted, remove the ConsoleLink if present
			return reconcile.Result{}, r.deleteConsoleLinkIfPresent(ctx, reqLogger)
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	reqLogger.Info("ArgoCD instance found", "ArgoCD.Namespace:", argocdInstance.Namespace, "ArgoCD.Name", argocdInstance.Name)

	// Set ArgoCD instance as the owner
	if err := controllerutil.SetControllerReference(argocdInstance, newArgoCDRoute(), r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	argoCDRoute := &routev1.Route{}
	err = r.client.Get(ctx, types.NamespacedName{Name: argocdRouteName, Namespace: argocdNS}, argoCDRoute)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("ArgoCD server route not found", "Route.Namespace", argocdNS)
			// if argocd-server route is deleted, remove the ConsoleLink if present
			return reconcile.Result{}, r.deleteConsoleLinkIfPresent(ctx, reqLogger)
		}
		return reconcile.Result{}, err
	}

	reqLogger.Info("Route found for argocd-server", "Route.Host", argoCDRoute.Spec.Host)

	consoleLink := newConsoleLink("https://"+argoCDRoute.Spec.Host, "ArgoCD Dashboard")

	found := &console.ConsoleLink{}
	err = r.client.Get(ctx, types.NamespacedName{Name: consoleLink.Name}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new ConsoleLink", "ConsoleLink.Name", consoleLink.Name)
		err = r.client.Create(ctx, consoleLink)
		if err != nil {
			return reconcile.Result{}, err
		}
		// ConsoleLink created successfully - don't requeue
		return reconcile.Result{}, nil
	} else if err != nil {
		reqLogger.Error(err, "Failed to create ConsoleLink", "ConsoleLink.Name", consoleLink.Name)
		return reconcile.Result{}, err
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
				Section:  "Application Stages",
				ImageURL: "https://raw.githubusercontent.com/argoproj/argo-cd/master/ui/src/assets/images/argo.png",
			},
		},
	}
}

func (r *ReconcileArgoCD) deleteConsoleLinkIfPresent(ctx context.Context, log logr.Logger) error {
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

func newArgoCDRoute() *routev1.Route {
	return &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      argocdRouteName,
			Namespace: argocdNS,
		},
	}
}
