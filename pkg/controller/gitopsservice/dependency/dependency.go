package dependency

import (
	"context"
	"fmt"
	"os"
	"time"

	argoapp "github.com/argoproj-labs/argocd-operator/pkg/apis/argoproj/v1alpha1"
	"github.com/go-logr/logr"
	v1 "github.com/operator-framework/api/pkg/operators/v1"
	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	cicdNs                 = "cicd"
	sealedSecretsSubName   = "sealed-secrets-operator-helm"
	argocdSubName          = "argocd-operator"
	sealedSecretsGroupName = "sealed-secrets-operator-group"
	argocdGroupName        = "argocd-operator-group"
	sealedSecretsImage     = "quay.io/bitnami/sealed-secrets-controller@sha256:8e9a37bb2e1a6f3a8bee949e3af0e9dab0d7dca618f1a63048dc541b5d554985"
)

var log = logf.Log.WithName("gitops_dependencies")

// Dependency represents an instance of GitOps dependency
type Dependency struct {
	client  client.Client
	timeout time.Duration
	isReady wait.ConditionFunc
	log     logr.Logger
}

// resource exclusions for the ArgoCD CR.
type resource struct {
	APIGroups []string `json:"apiGroups"`
	Kinds     []string `json:"kinds"`
	Clusters  []string `json:"clusters"`
}

// NewClient create a new instance of GitOps dependencies
func NewClient(client client.Client, timeout time.Duration) *Dependency {
	return &Dependency{
		client:  client,
		timeout: timeout,
		log:     log.WithName("GitOps Dependencies"),
	}
}

// Install the dependencies required by GitOps
func (d *Dependency) Install(prefixes []string) error {
	ctx := context.Background()
	operators := []operatorResource{}

	for _, prefix := range prefixes {
		operators = append(operators, newSealedSecretsOperator(prefix))
	}
	operators = append(operators, newArgoCDOperator())

	errs := make(chan error)
	for _, operator := range operators {
		// handle each operator installation by a separate goroutine
		go func(operator operatorResource) {
			err := d.installOperator(ctx, operator)
			errs <- err
		}(operator)
	}
	return <-errs
}

func (d *Dependency) installOperator(ctx context.Context, operator operatorResource) error {
	ns := operator.GetNamespace()
	d.log.Info("Creating Namespace", "Namespace.Name", ns.Name)
	err := d.createResourceIfAbsent(ctx, operator.GetNamespace(), types.NamespacedName{Name: ns.Name})
	if err != nil {
		return err
	}

	operatorGroup := operator.GetOperatorGroup()
	d.log.Info("Creating OperatorGroup", "OperatorGroup.Name", operatorGroup.Name)
	err = d.createResourceIfAbsent(ctx, operator.GetOperatorGroup(), types.NamespacedName{Name: operatorGroup.Name, Namespace: operatorGroup.Namespace})
	if err != nil {
		return err
	}

	subscription := operator.GetSubscription()
	d.log.Info("Creating Subscription", "Subscription.Name", subscription.Name)
	err = d.createResourceIfAbsent(ctx, operator.GetSubscription(), types.NamespacedName{Name: subscription.Name, Namespace: subscription.Namespace})
	if err != nil {
		return err
	}

	d.log.Info("Waiting for operator to install", "Operator.Name", operator.subscription, "Operator.Namespace", operator.namespace)
	err = waitForOperator(ctx, d.client, d.timeout, types.NamespacedName{Name: operator.csv, Namespace: operator.namespace}, d.isReady)
	if err != nil {
		return err
	}
	d.log.Info("Operator installed successfully", "Operator.Name", operator.subscription, "Operator.Namespace", operator.namespace)

	cr, name, err := operator.createCR(operator.namespace)
	d.log.Info("Creating the Operator instance", "CR.Name", name, "CR.Namespace", operator.namespace)
	err = d.createResourceIfAbsent(context.TODO(), cr, types.NamespacedName{Name: name, Namespace: operator.namespace})
	if err != nil {
		return err
	}
	d.log.Info("Operator instance created sucessfully", "CR.Name", name, "CR.Namespace", operator.namespace)

	return nil
}

func isOperatorReady(ctx context.Context, client client.Client, ns types.NamespacedName) wait.ConditionFunc {
	return func() (bool, error) {
		csv := &v1alpha1.ClusterServiceVersion{}
		err := client.Get(ctx, ns, csv)
		if err != nil && !errors.IsNotFound(err) {
			return false, err
		}

		switch csv.Status.Phase {
		case v1alpha1.CSVPhaseFailed:
			return false, fmt.Errorf("Operator installation failed: %s", csv.Status.Reason)
		case v1alpha1.CSVPhaseSucceeded:
			return true, nil
		}
		return false, nil
	}
}

func waitForOperator(ctx context.Context, client client.Client, timeout time.Duration, ns types.NamespacedName, waitFunc wait.ConditionFunc) error {
	if waitFunc == nil {
		waitFunc = isOperatorReady(ctx, client, ns)
	}
	// poll until waitFunc returns true, error or the timeout is reached
	return wait.PollImmediate(1*time.Second, timeout, waitFunc)
}

func (d *Dependency) createResourceIfAbsent(ctx context.Context, obj runtime.Object, ns types.NamespacedName) error {
	err := d.client.Get(ctx, ns, obj)
	if err != nil {
		switch errors.ReasonForError(err) {
		case metav1.StatusReasonNotFound:
			err = d.client.Create(ctx, obj)
			if err != nil {
				d.log.Error(err, "Unable to create resource", "Resource.Kind", obj.GetObjectKind(), "Resource.Name", ns.
					Name)
				return err
			}
			d.log.Info("Successfully created resource", "Resource.Kind", obj.GetObjectKind(), "Resource.Name", ns.Name, "Resource.Namespace", ns.
				Namespace)
		case metav1.StatusReasonAlreadyExists:
			d.log.Info("Resource already exists", "Resource.Kind", obj.GetObjectKind(), "Resource.Name", ns.Name)
		default:
			return err
		}
	}
	return nil
}

func newNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func newOperatorGroup(namespace, name string) *v1.OperatorGroup {
	return &v1.OperatorGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1.OperatorGroupSpec{
			TargetNamespaces: []string{namespace},
		},
	}
}

func newSubscription(namespace, name string) *v1alpha1.Subscription {
	return &v1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: &v1alpha1.SubscriptionSpec{
			Channel:                "alpha",
			CatalogSource:          "community-operators",
			CatalogSourceNamespace: "openshift-marketplace",
			Package:                name,
		},
	}
}

func addPrefixIfNecessary(prefix, name string) string {
	if prefix != "" {
		return prefix + "-" + name
	}
	return name
}

func argoCDCR(ns string) (runtime.Object, string, error) {
	name := "argocd"
	b, err := yaml.Marshal([]resource{
		{
			APIGroups: []string{"tekton.dev"},
			Kinds:     []string{"TaskRun", "PipelineRun"},
			Clusters:  []string{"*"},
		},
	})
	if err != nil {
		return nil, "", err
	}
	return &argoapp.ArgoCD{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ArgoCD",
			APIVersion: "argoproj.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: argoapp.ArgoCDSpec{
			ResourceExclusions: string(b),
			Server: argoapp.ArgoCDServerSpec{
				Route: argoapp.ArgoCDRouteSpec{Enabled: true},
			},
		},
	}, name, nil
}

func sealedSecretsCR(ns string) (runtime.Object, string, error) {
	name := "sealedsecretcontroller"
	image := os.Getenv("SEALED_SECRETS_IMAGE")
	if image == "" {
		image = sealedSecretsImage
	}
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "bitnami.com/v1alpha1",
			"kind":       "SealedSecretController",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": ns,
			},
			"spec": map[string]interface{}{
				"image": map[string]interface{}{
					"repository": "quay.io/bitnami/sealed-secrets-controller@sha256:8e9a37bb2e1a6f3a8bee949e3af0e9dab0d7dca618f1a63048dc541b5d554985",
					"pullPolicy": "IfNotPresent",
				},
				"controller": map[string]interface{}{
					"create": true,
				},
				"serviceAccount": map[string]interface{}{
					"create": true,
					"name":   "",
				},
				"rbac": map[string]interface{}{
					"create":     true,
					"pspenabled": false,
				},
				"secretName": "sealed-secrets-key",
			},
		},
	}, name, nil
}
