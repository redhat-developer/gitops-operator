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
	argocdNS        = "argocd"
	consoleLink     = "argocd-application"
	argocdInstance  = "argocd"
	argocdRouteName = "argocd-server"
)

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

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
	err = c.Watch(&source.Kind{Type: &argoprojv1alpha1.ArgoCD{}}, &handler.EnqueueRequestForObject{}, argocdPredicate())
	if err != nil {
		return err
	}

	// Watch for changes to argocd-server route
	// The ConsoleLink holds the route URL and should be regenerated when route is updated
	err = c.Watch(&source.Kind{Type: &routev1.Route{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &argoprojv1alpha1.ArgoCD{},
	}, argocdPredicate())
	if err != nil {
		return err
	}

	return nil
}

// only reconcile for events from argocd namespace
func argocdPredicate() predicate.Funcs {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return assertArgoCD(types.NamespacedName{Namespace: e.MetaNew.GetNamespace(), Name: e.MetaNew.GetName()})
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return assertArgoCD(types.NamespacedName{Namespace: e.Meta.GetNamespace(), Name: e.Meta.GetName()})
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return assertArgoCD(types.NamespacedName{Namespace: e.Meta.GetNamespace(), Name: e.Meta.GetName()})
		},
	}
}

func assertArgoCD(n types.NamespacedName) bool {
	return n.Namespace == argocdNS && n.Name == argocdInstance
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

	reqLogger.Info("ArgoCD Namespace", "argocd.namespace", request.Namespace)

	// Fetch the ArgoCD instance
	instance := &argoprojv1alpha1.ArgoCD{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("ArgoCD instance not found")
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, deleteConsoleLinkIfExists(r.client, reqLogger)
		}
		reqLogger.Error(err, "Error reading argocd")
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	reqLogger.Info("ArgoCD instance found", "ArgoCD.Namespace:", instance.Namespace, "ArgoCD.Name", instance.Name)

	// Set GitopsService instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, newArgoCDRoute(), r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	argoCDRoute := &routev1.Route{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: argocdRouteName, Namespace: argocdNS}, argoCDRoute)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("ArgoCD server route not found", "Route.Namespace", argocdNS)
			return reconcile.Result{}, deleteConsoleLinkIfExists(r.client, reqLogger)
		}
		return reconcile.Result{}, err
	}

	reqLogger.Info("Route found for argocd-server", "URL Path", argoCDRoute.Spec.Host)

	// Create the ConsoleLink object for the route
	consoleLink := newConsoleLink("https://"+argoCDRoute.Spec.Host, "ArgoCD dashboard")

	found := &console.ConsoleLink{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: consoleLink.Name}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Creating a new ConsoleLink", "ConsoleLink.Name", consoleLink.Name)
			err = r.client.Create(context.TODO(), consoleLink)
			if err != nil {
				return reconcile.Result{}, err
			}
			// ConsoleLink created successfully - don't requeue
			return reconcile.Result{}, nil
		}
		reqLogger.Error(err, "Failed to create ConsoleLink", "ConsoleLink.Name", consoleLink.Name)
		return reconcile.Result{}, err
	}

	// ConsoleLink already exists - don't requeue
	reqLogger.Info("Skip reconcile: ConsoleLink already exists", "ConsoleLink.Name", consoleLink.Name)
	return reconcile.Result{}, nil
}

func newConsoleLink(href, text string) *console.ConsoleLink {
	return &console.ConsoleLink{
		ObjectMeta: metav1.ObjectMeta{
			Name: consoleLink,
		},
		Spec: console.ConsoleLinkSpec{
			Link: console.Link{
				Text: text,
				Href: href,
			},
			Location: console.HelpMenu,
		},
	}
}

func deleteConsoleLinkIfExists(c client.Client, log logr.Logger) error {
	err := c.Get(context.TODO(), types.NamespacedName{Name: consoleLink}, &console.ConsoleLink{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	log.Info("Deleting ConsoleLink", "ConsoleLink.Name", consoleLink)
	return c.Delete(context.TODO(), &console.ConsoleLink{ObjectMeta: metav1.ObjectMeta{Name: consoleLink}})
}

func newArgoCDRoute() *routev1.Route {
	return &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      argocdRouteName,
			Namespace: argocdNS,
		},
	}
}
