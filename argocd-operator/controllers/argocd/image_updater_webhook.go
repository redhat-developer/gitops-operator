package argocd

import (
	"context"
	"crypto/sha256"
	"fmt"
	"maps"
	"reflect"
	"strconv"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	"golang.org/x/mod/semver"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	argoproj "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/argocd-operator/common"
	"github.com/redhat-developer/gitops-operator/argocd-operator/controllers/argoutil"
)

const (
	// imageUpdaterWebhookPort is the port the Image Updater webhook server listens on. It is intentionally not
	// configurable: the Image Updater NetworkPolicy only admits ingress traffic on this port.
	imageUpdaterWebhookPort = 8082

	imageUpdaterWebhookPortName = "webhook"

	// imageUpdaterWebhookPath is the only path exposed through the Route and the Ingress.
	imageUpdaterWebhookPath = "/webhook"

	imageUpdaterWebhookTLSVolumeName = "webhook-tls"

	// imageUpdaterWebhookTLSMountPath is the directory the Image Updater webhook server reads tls.crt and tls.key from.
	imageUpdaterWebhookTLSMountPath = "/app/config/webhook/tls"

	// imageUpdaterWebhookChecksumLabel is set on the Image Updater pod template. Its value changes whenever
	// the webhook serving certificate or a webhook secret changes, which rolls the pod: the Image Updater only
	// reads both at startup.
	imageUpdaterWebhookChecksumLabel = "argocd.argoproj.io/image-updater-webhook-checksum"

	// minImageUpdaterWebhookTLSVersion is the first Image Updater release that reads the webhook serving certificate
	// from imageUpdaterWebhookTLSMountPath. Earlier releases either serve plain HTTP only (v1.1 and earlier) or read it
	// from /app/config/tls, which holds the Argo CD TLS certs ConfigMap (v1.2 and v1.3).
	minImageUpdaterWebhookTLSVersion = "v1.4.0"

	// argoCDTrackingIDAnnotation is set by Argo CD on the resources it syncs when it tracks them by annotation.
	argoCDTrackingIDAnnotation = "argocd.argoproj.io/tracking-id"
)

// imageUpdaterWebhookRegistry describes how the webhook secret of one registry type is passed to Image Updater.
type imageUpdaterWebhookRegistry struct {
	// field is the name of the field in .spec.imageUpdater.webhookServer.secrets.
	field string
	// envName is the environment variable Image Updater reads the secret from.
	envName string
	// fallbackKey is the key in argocd-image-updater-secret used when the secret is not declared in the CR.
	fallbackKey string
	getRef      func(*argoproj.ImageUpdaterWebhookSecretsSpec) *argoproj.WebhookSecretKeySelector
}

var imageUpdaterWebhookRegistries = []imageUpdaterWebhookRegistry{
	{
		field:       "docker",
		envName:     "DOCKER_WEBHOOK_SECRET",
		fallbackKey: "webhook.docker-secret",
		getRef: func(s *argoproj.ImageUpdaterWebhookSecretsSpec) *argoproj.WebhookSecretKeySelector {
			return s.Docker
		},
	},
	{
		field:       "ghcr",
		envName:     "GHCR_WEBHOOK_SECRET",
		fallbackKey: "webhook.ghcr-secret",
		getRef: func(s *argoproj.ImageUpdaterWebhookSecretsSpec) *argoproj.WebhookSecretKeySelector {
			return s.GHCR
		},
	},
	{
		field:       "quay",
		envName:     "QUAY_WEBHOOK_SECRET",
		fallbackKey: "webhook.quay-secret",
		getRef: func(s *argoproj.ImageUpdaterWebhookSecretsSpec) *argoproj.WebhookSecretKeySelector {
			return s.Quay
		},
	},
	{
		field:       "harbor",
		envName:     "HARBOR_WEBHOOK_SECRET",
		fallbackKey: "webhook.harbor-secret",
		getRef: func(s *argoproj.ImageUpdaterWebhookSecretsSpec) *argoproj.WebhookSecretKeySelector {
			return s.Harbor
		},
	},
	{
		field:       "acr",
		envName:     "ACR_WEBHOOK_SECRET",
		fallbackKey: "webhook.acr-secret",
		getRef: func(s *argoproj.ImageUpdaterWebhookSecretsSpec) *argoproj.WebhookSecretKeySelector {
			return s.ACR
		},
	},
	{
		field:       "aliyunACR",
		envName:     "ALIYUN_ACR_WEBHOOK_SECRET",
		fallbackKey: "webhook.aliyun-acr-secret",
		getRef: func(s *argoproj.ImageUpdaterWebhookSecretsSpec) *argoproj.WebhookSecretKeySelector {
			return s.AliyunACR
		},
	},
	{
		field:       "cloudEvents",
		envName:     "CLOUDEVENTS_WEBHOOK_SECRET",
		fallbackKey: "webhook.cloudevents-secret",
		getRef: func(s *argoproj.ImageUpdaterWebhookSecretsSpec) *argoproj.WebhookSecretKeySelector {
			return s.CloudEvents
		},
	},
}

// declaredRef returns the Secret key reference declared in the CR for this registry, or nil.
func (reg imageUpdaterWebhookRegistry) declaredRef(cr *argoproj.ArgoCD) *argoproj.WebhookSecretKeySelector {
	if cr.Spec.ImageUpdater.WebhookServer.Secrets == nil {
		return nil
	}
	return reg.getRef(cr.Spec.ImageUpdater.WebhookServer.Secrets)
}

// imageUpdaterWebhookServiceName returns the name of the Service, Route and Ingress for the Image Updater webhook.
func imageUpdaterWebhookServiceName(cr *argoproj.ArgoCD) string {
	return nameWithSuffix(common.ImageUpdaterWebhookSuffix, cr)
}

// imageUpdaterWebhookTLSSecretName returns the name of the Secret holding the webhook serving certificate.
func imageUpdaterWebhookTLSSecretName(cr *argoproj.ArgoCD) string {
	return nameWithSuffix(common.ImageUpdaterWebhookTLSSecretSuffix, cr)
}

// imageUpdaterWebhookEnv returns env with the webhook-related environment variables applied. When the webhook is
// not exposed, env is returned unchanged so that users can still configure the webhook server by hand.
//
// Webhook secrets are resolved in the following order:
//  1. A Secret key reference declared in .spec.imageUpdater.webhookServer.secrets. The reference is optional, so
//     a missing Secret or key only leaves that registry's handler unregistered instead of keeping the whole
//     Image Updater from starting. imageUpdaterWebhookConfigIssues reports it in a condition instead.
//  2. An environment variable of the same name set in .spec.imageUpdater.env.
//  3. The conventional key in argocd-image-updater-secret, marked optional.
func imageUpdaterWebhookEnv(cr *argoproj.ArgoCD, env []corev1.EnvVar) []corev1.EnvVar {
	if !cr.Spec.ImageUpdater.IsWebhookEnabled() {
		return env
	}

	env = argoutil.EnvSet(env, corev1.EnvVar{Name: "ENABLE_WEBHOOK", Value: "true"})
	if cr.Spec.ImageUpdater.WebhookServer.Insecure {
		env = argoutil.EnvSet(env, corev1.EnvVar{Name: "DISABLE_TLS", Value: "true"})
	} else {
		env = argoutil.EnvRemove(env, "DISABLE_TLS")
	}

	for _, reg := range imageUpdaterWebhookRegistries {
		if ref := reg.declaredRef(cr); ref != nil {
			env = argoutil.EnvSet(env, corev1.EnvVar{
				Name: reg.envName,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: ref.Name},
						Key:                  ref.Key,
						Optional:             new(true),
					},
				},
			})
			continue
		}
		if argoutil.EnvGet(env, reg.envName) != nil {
			continue
		}
		env = append(env, corev1.EnvVar{
			Name: reg.envName,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: ArgocdImageUpdaterSecret},
					Key:                  reg.fallbackKey,
					Optional:             new(true),
				},
			},
		})
	}

	return env
}

// usesImageUpdaterWebhookTLS returns true if the Image Updater webhook server is exposed and serves TLS.
func usesImageUpdaterWebhookTLS(cr *argoproj.ArgoCD) bool {
	return cr.Spec.ImageUpdater.IsWebhookEnabled() && !cr.Spec.ImageUpdater.WebhookServer.Insecure
}

// wantsImageUpdaterWebhookAutoTLS returns true if the webhook Service should request a serving certificate from the
// OpenShift service CA. Like the Argo CD server, this is only the case when the Route terminates TLS with reencrypt,
// which is the default. With any other termination, the service CA certificate would be presented to external
// clients that do not trust it, so the user provides the certificate instead.
func wantsImageUpdaterWebhookAutoTLS(cr *argoproj.ArgoCD) bool {
	if !usesImageUpdaterWebhookTLS(cr) {
		return false
	}
	route := cr.Spec.ImageUpdater.WebhookServer.Route
	return !route.Enabled || route.TLS == nil || route.TLS.Termination == "" || route.TLS.Termination == routev1.TLSTerminationReencrypt
}

// imageUpdaterWebhookTLSVolume returns the volume holding the webhook serving certificate. The volume is optional
// because the OpenShift service CA issues the certificate asynchronously after the Service is created. Until it
// exists, the webhook server falls back to a self-signed certificate, and the pod is rolled once it appears.
func imageUpdaterWebhookTLSVolume(cr *argoproj.ArgoCD) corev1.Volume {
	return corev1.Volume{
		Name: imageUpdaterWebhookTLSVolumeName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: imageUpdaterWebhookTLSSecretName(cr),
				Optional:   new(true),
			},
		},
	}
}

func imageUpdaterWebhookTLSVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      imageUpdaterWebhookTLSVolumeName,
		MountPath: imageUpdaterWebhookTLSMountPath,
		ReadOnly:  true,
	}
}

// fetchSecretKey returns the value of key in the named Secret, or nil if the Secret or key does not exist.
func (r *ReconcileArgoCD) fetchSecretKey(namespace, name, key string) ([]byte, error) {
	secret := &corev1.Secret{}
	if err := argoutil.FetchObject(r.Client, namespace, name, secret); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return secret.Data[key], nil
}

// imageUpdaterWebhookChecksum returns a checksum of everything the Image Updater webhook server reads at startup
// from Secrets: the serving certificate (in secure mode) and the webhook secrets the operator wires into the pod
// environment. Webhook secrets supplied through .spec.imageUpdater.env are managed by the user and not included.
func (r *ReconcileArgoCD) imageUpdaterWebhookChecksum(cr *argoproj.ArgoCD, userEnv []corev1.EnvVar) (string, error) {
	h := sha256.New()
	write := func(name string, value []byte) {
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write(value)
		h.Write([]byte{0})
	}

	if usesImageUpdaterWebhookTLS(cr) {
		secret := &corev1.Secret{}
		if err := argoutil.FetchObject(r.Client, cr.Namespace, imageUpdaterWebhookTLSSecretName(cr), secret); err != nil {
			if !errors.IsNotFound(err) {
				return "", err
			}
		}
		write(corev1.TLSCertKey, secret.Data[corev1.TLSCertKey])
		write(corev1.TLSPrivateKeyKey, secret.Data[corev1.TLSPrivateKeyKey])
	}

	for _, reg := range imageUpdaterWebhookRegistries {
		var value []byte
		var err error
		if ref := reg.declaredRef(cr); ref != nil {
			value, err = r.fetchSecretKey(cr.Namespace, ref.Name, ref.Key)
		} else if argoutil.EnvGet(userEnv, reg.envName) == nil {
			value, err = r.fetchSecretKey(cr.Namespace, ArgocdImageUpdaterSecret, reg.fallbackKey)
		}
		if err != nil {
			return "", err
		}
		write(reg.envName, value)
	}

	// Truncated so that it fits in a label value.
	return fmt.Sprintf("%x", h.Sum(nil))[:32], nil
}

// deleteImageUpdaterWebhookObject deletes a webhook Service, Route or Ingress, but only if this ArgoCD instance
// controls it. An object with the same name created by hand, for example to expose the webhook server before the
// operator supported it, is left in place.
func (r *ReconcileArgoCD) deleteImageUpdaterWebhookObject(cr *argoproj.ArgoCD, obj client.Object, explanation string) error {
	if !metav1.IsControlledBy(obj, cr) {
		log.Info(fmt.Sprintf("not deleting %T %s/%s: it is not managed by ArgoCD %s", obj, obj.GetNamespace(), obj.GetName(), cr.Name))
		return nil
	}
	argoutil.LogResourceDeletion(log, obj, explanation)
	return r.Delete(context.TODO(), obj)
}

// controlledByOther returns true if obj has a controller other than cr.
func controlledByOther(obj client.Object, cr *argoproj.ArgoCD) bool {
	controller := metav1.GetControllerOf(obj)
	return controller != nil && controller.UID != cr.UID
}

// argoCDTrackingMarker returns a description of the Argo CD tracking metadata on obj, or "" if there is none.
// Argo CD does not set owner references on the resources it syncs, so this metadata is the only sign that an
// Argo CD Application manages obj. Adopting such an object would make the operator and Argo CD revert each
// other's changes. The Argo CD instance managing obj may not be this one, so both the default tracking label key
// and this instance's custom key are checked.
func argoCDTrackingMarker(obj client.Object, cr *argoproj.ArgoCD) string {
	if _, ok := obj.GetAnnotations()[argoCDTrackingIDAnnotation]; ok {
		return fmt.Sprintf("annotation %s", argoCDTrackingIDAnnotation)
	}
	for _, key := range []string{common.ArgoCDDefaultApplicationInstanceLabelKey, cr.ApplicationInstanceLabelKey()} {
		if _, ok := obj.GetLabels()[key]; ok {
			return fmt.Sprintf("label %s", key)
		}
	}
	return ""
}

// adoptImageUpdaterWebhookObject makes this ArgoCD instance the controller of an existing webhook Service, Route
// or Ingress that was created by hand under the name the operator uses. It returns the list of changes made, for
// logging, and false if the object is controlled by something else or managed by Argo CD. Such an object is left
// untouched and reported by imageUpdaterWebhookConfigIssues, rather than failing the reconciliation of the whole
// instance.
func (r *ReconcileArgoCD) adoptImageUpdaterWebhookObject(cr *argoproj.ArgoCD, obj client.Object) ([]string, bool, error) {
	if metav1.IsControlledBy(obj, cr) {
		return nil, true, nil
	}
	if controlledByOther(obj, cr) {
		log.Info(fmt.Sprintf("not reconciling %T %s/%s: it is controlled by another owner", obj, obj.GetNamespace(), obj.GetName()))
		return nil, false, nil
	}
	if marker := argoCDTrackingMarker(obj, cr); marker != "" {
		log.Info(fmt.Sprintf("not reconciling %T %s/%s: it has the Argo CD tracking %s", obj, obj.GetNamespace(), obj.GetName(), marker))
		return nil, false, nil
	}
	if err := controllerutil.SetControllerReference(cr, obj, r.Scheme); err != nil {
		return nil, false, err
	}
	log.Info(fmt.Sprintf("adopting existing %T %s/%s for the image updater webhook", obj, obj.GetNamespace(), obj.GetName()))
	return []string{"owner reference"}, true, nil
}

// reconcileImageUpdaterWebhookService ensures the Service fronting the Image Updater webhook server exists while
// the webhook is exposed, and is deleted otherwise. In secure mode on OpenShift, the Service requests a serving
// certificate from the service CA, unless the Route uses a termination other than reencrypt.
func (r *ReconcileArgoCD) reconcileImageUpdaterWebhookService(cr *argoproj.ArgoCD) error {
	svc := newServiceWithName(imageUpdaterWebhookServiceName(cr), common.ArgoCDImageUpdaterControllerComponent, cr)
	existing := &corev1.Service{}
	found, err := argoutil.IsObjectFound(r.Client, cr.Namespace, svc.Name, existing)
	if err != nil {
		return err
	}

	if !cr.Spec.ImageUpdater.IsWebhookEnabled() {
		if found {
			return r.deleteImageUpdaterWebhookObject(cr, existing, "image updater webhook is disabled")
		}
		return nil
	}

	ports := []corev1.ServicePort{
		{
			Name:       imageUpdaterWebhookPortName,
			Port:       imageUpdaterWebhookPort,
			Protocol:   corev1.ProtocolTCP,
			TargetPort: intstr.FromInt32(imageUpdaterWebhookPort),
		},
	}
	selector := map[string]string{
		common.ArgoCDKeyName: nameWithSuffix(common.ArgoCDImageUpdaterControllerComponent, cr),
	}

	if !found {
		svc.Spec.Ports = ports
		svc.Spec.Selector = selector
		if _, err := ensureAutoTLSAnnotation(r.Client, svc, imageUpdaterWebhookTLSSecretName(cr), wantsImageUpdaterWebhookAutoTLS(cr)); err != nil {
			return err
		}
		if err := controllerutil.SetControllerReference(cr, svc, r.Scheme); err != nil {
			return err
		}
		argoutil.LogResourceCreation(log, svc)
		return r.Create(context.TODO(), svc)
	}

	changes, adopted, err := r.adoptImageUpdaterWebhookObject(cr, existing)
	if err != nil || !adopted {
		return err
	}
	if !reflect.DeepEqual(existing.Spec.Ports, ports) {
		existing.Spec.Ports = ports
		changes = append(changes, "ports")
	}
	if !reflect.DeepEqual(existing.Spec.Selector, selector) {
		existing.Spec.Selector = selector
		changes = append(changes, "selector")
	}
	updated, err := ensureAutoTLSAnnotation(r.Client, existing, imageUpdaterWebhookTLSSecretName(cr), wantsImageUpdaterWebhookAutoTLS(cr))
	if err != nil {
		return err
	}
	if updated {
		changes = append(changes, "auto TLS annotation")
	}
	if len(changes) > 0 {
		argoutil.LogResourceUpdate(log, existing, "updating", strings.Join(changes, ", "))
		return r.Update(context.TODO(), existing)
	}
	return nil
}

// reconcileImageUpdaterWebhookRoute ensures the Route for the Image Updater webhook matches the CR.
func (r *ReconcileArgoCD) reconcileImageUpdaterWebhookRoute(cr *argoproj.ArgoCD) error {
	route := newRouteWithName(imageUpdaterWebhookServiceName(cr), cr)
	existing := &routev1.Route{}
	found, err := argoutil.IsObjectFound(r.Client, cr.Namespace, route.Name, existing)
	if err != nil {
		return err
	}

	routeSpec := cr.Spec.ImageUpdater.WebhookServer.Route
	if !cr.Spec.ImageUpdater.Enabled || !routeSpec.Enabled {
		if found {
			return r.deleteImageUpdaterWebhookObject(cr, existing, "image updater webhook route is disabled")
		}
		return nil
	}

	if len(routeSpec.Labels) > 0 {
		maps.Copy(route.Labels, routeSpec.Labels)
	}
	route.Annotations = routeSpec.Annotations

	host := cr.Spec.ImageUpdater.WebhookServer.Host
	if host == "" {
		ingressConfig := &configv1.Ingress{}
		if err := r.Get(context.TODO(), client.ObjectKey{Name: "cluster"}, ingressConfig); err != nil {
			return err
		}
		host = fmt.Sprintf("%s-%s-%s.%s", argoutil.TruncateCRName(cr.Name), common.ImageUpdaterWebhookSuffix, cr.Namespace, ingressConfig.Spec.Domain)
	}
	hostLabels := strings.SplitN(host, ".", 2)
	if len(hostLabels[0]) > maxLabelLength {
		hostLabels[0] = hostLabels[0][:maxLabelLength]
	}
	host, err = shortenHostname(strings.Join(hostLabels, "."))
	if err != nil {
		return err
	}
	route.Spec.Host = host

	route.Spec.Port = &routev1.RoutePort{
		TargetPort: intstr.FromString(imageUpdaterWebhookPortName),
	}

	// The termination defaults follow what the webhook server serves: plain HTTP in insecure mode (edge),
	// TLS with a service CA certificate the router already trusts otherwise (reencrypt).
	defaultTermination := routev1.TLSTerminationReencrypt
	if cr.Spec.ImageUpdater.WebhookServer.Insecure {
		defaultTermination = routev1.TLSTerminationEdge
	}
	if routeSpec.TLS != nil {
		routeCopy := route.DeepCopy()
		if err := r.overrideRouteTLS(routeSpec.TLS, routeCopy, cr); err != nil {
			return err
		}
		tls := routeCopy.Spec.TLS
		if tls.Termination == "" {
			tls.Termination = defaultTermination
		}
		if tls.InsecureEdgeTerminationPolicy == "" {
			tls.InsecureEdgeTerminationPolicy = routev1.InsecureEdgeTerminationPolicyRedirect
		}
		route.Spec.TLS = tls
	} else {
		route.Spec.TLS = &routev1.TLSConfig{
			Termination:                   defaultTermination,
			InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
		}
	}

	// Passthrough Routes cannot be restricted to a path.
	if route.Spec.TLS.Termination != routev1.TLSTerminationPassthrough {
		route.Spec.Path = imageUpdaterWebhookPath
	}

	route.Spec.To = routev1.RouteTargetReference{
		Kind:   "Service",
		Name:   imageUpdaterWebhookServiceName(cr),
		Weight: new(int32(100)),
	}

	route.Spec.WildcardPolicy = routev1.WildcardPolicyNone
	if routeSpec.WildcardPolicy != nil && len(*routeSpec.WildcardPolicy) > 0 {
		route.Spec.WildcardPolicy = *routeSpec.WildcardPolicy
	}

	if !found {
		if err := controllerutil.SetControllerReference(cr, route, r.Scheme); err != nil {
			return err
		}
		argoutil.LogResourceCreation(log, route)
		return r.Create(context.TODO(), route)
	}

	changes, adopted, err := r.adoptImageUpdaterWebhookObject(cr, existing)
	if err != nil || !adopted {
		return err
	}
	if !reflect.DeepEqual(existing.Spec, route.Spec) {
		existing.Spec = route.Spec
		changes = append(changes, "spec")
	}
	if !reflect.DeepEqual(existing.Labels, route.Labels) {
		existing.Labels = route.Labels
		changes = append(changes, "labels")
	}
	if len(routeSpec.Annotations) > 0 && !reflect.DeepEqual(existing.Annotations, route.Annotations) {
		existing.Annotations = route.Annotations
		changes = append(changes, "annotations")
	}
	if len(changes) > 0 {
		argoutil.LogResourceUpdate(log, existing, "updating", strings.Join(changes, ", "))
		return r.Update(context.TODO(), existing)
	}
	return nil
}

// reconcileImageUpdaterWebhookIngress ensures the Ingress for the Image Updater webhook matches the CR.
func (r *ReconcileArgoCD) reconcileImageUpdaterWebhookIngress(cr *argoproj.ArgoCD) error {
	ingress := newIngressWithName(imageUpdaterWebhookServiceName(cr), cr)
	existing := &networkingv1.Ingress{}
	found, err := argoutil.IsObjectFound(r.Client, cr.Namespace, ingress.Name, existing)
	if err != nil {
		return err
	}

	ingressSpec := cr.Spec.ImageUpdater.WebhookServer.Ingress
	if !cr.Spec.ImageUpdater.Enabled || !ingressSpec.Enabled {
		if found {
			return r.deleteImageUpdaterWebhookObject(cr, existing, "image updater webhook ingress is disabled")
		}
		return nil
	}

	backendProtocol := "HTTPS"
	if cr.Spec.ImageUpdater.WebhookServer.Insecure {
		backendProtocol = "HTTP"
	}
	// Start from the annotations the operator needs, then apply the user's on top: replacing the whole map
	// would drop the backend protocol the webhook server relies on when TLS is enabled.
	ingress.Annotations = map[string]string{
		common.ArgoCDKeyIngressSSLRedirect:     "true",
		common.ArgoCDKeyIngressBackendProtocol: backendProtocol,
	}
	maps.Copy(ingress.Annotations, ingressSpec.Annotations)

	host := cr.Name
	if cr.Spec.ImageUpdater.WebhookServer.Host != "" {
		host, err = shortenHostname(cr.Spec.ImageUpdater.WebhookServer.Host)
		if err != nil {
			return err
		}
	}

	path := imageUpdaterWebhookPath
	if ingressSpec.Path != "" {
		path = ingressSpec.Path
	}

	pathType := networkingv1.PathTypeImplementationSpecific
	ingress.Spec.IngressClassName = ingressSpec.IngressClassName
	ingress.Spec.Rules = []networkingv1.IngressRule{
		{
			Host: host,
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{
						{
							Path:     path,
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: imageUpdaterWebhookServiceName(cr),
									Port: networkingv1.ServiceBackendPort{
										Name: imageUpdaterWebhookPortName,
									},
								},
							},
						},
					},
				},
			},
		},
	}
	ingress.Spec.TLS = ingressSpec.TLS

	if !found {
		if err := controllerutil.SetControllerReference(cr, ingress, r.Scheme); err != nil {
			return err
		}
		argoutil.LogResourceCreation(log, ingress)
		return r.Create(context.TODO(), ingress)
	}

	changes, adopted, err := r.adoptImageUpdaterWebhookObject(cr, existing)
	if err != nil || !adopted {
		return err
	}
	// The default IngressClass admission plugin may set the class on creation, so only reconcile it when set in the CR.
	if ingressSpec.IngressClassName != nil && !reflect.DeepEqual(existing.Spec.IngressClassName, ingressSpec.IngressClassName) {
		existing.Spec.IngressClassName = ingressSpec.IngressClassName
		changes = append(changes, "ingress class name")
	}
	if !reflect.DeepEqual(existing.Annotations, ingress.Annotations) {
		existing.Annotations = ingress.Annotations
		changes = append(changes, "annotations")
	}
	if !reflect.DeepEqual(existing.Spec.Rules, ingress.Spec.Rules) {
		existing.Spec.Rules = ingress.Spec.Rules
		changes = append(changes, "ingress rules")
	}
	if !reflect.DeepEqual(existing.Spec.TLS, ingress.Spec.TLS) {
		existing.Spec.TLS = ingress.Spec.TLS
		changes = append(changes, "ingress tls")
	}
	if len(changes) > 0 {
		argoutil.LogResourceUpdate(log, existing, "updating", strings.Join(changes, ", "))
		return r.Update(context.TODO(), existing)
	}
	return nil
}

// reconcileImageUpdaterWebhookNetworking reconciles the Service, Route and Ingress for the Image Updater webhook.
func (r *ReconcileArgoCD) reconcileImageUpdaterWebhookNetworking(cr *argoproj.ArgoCD) error {
	if err := r.reconcileImageUpdaterWebhookService(cr); err != nil {
		return err
	}
	return r.reconcileImageUpdaterWebhookExposure(cr)
}

// reconcileImageUpdaterWebhookExposure reconciles the Route and Ingress for the Image Updater webhook.
func (r *ReconcileArgoCD) reconcileImageUpdaterWebhookExposure(cr *argoproj.ArgoCD) error {
	if argoutil.IsRouteAPIAvailable() {
		if err := r.reconcileImageUpdaterWebhookRoute(cr); err != nil {
			return err
		}
	}
	return r.reconcileImageUpdaterWebhookIngress(cr)
}

// imageUpdaterWebhookConfigIssues returns a description of each Image Updater webhook setting that is unlikely
// to work as intended.
func (r *ReconcileArgoCD) imageUpdaterWebhookConfigIssues(cr *argoproj.ArgoCD) ([]string, error) {
	if !cr.Spec.ImageUpdater.IsWebhookEnabled() {
		return nil, nil
	}
	var issues []string
	webhookServer := cr.Spec.ImageUpdater.WebhookServer

	if webhookServer.Route.Enabled && webhookServer.Route.TLS != nil && webhookServer.Route.TLS.Termination != "" {
		termination := webhookServer.Route.TLS.Termination
		if webhookServer.Insecure && termination != routev1.TLSTerminationEdge {
			issues = append(issues, fmt.Sprintf("route TLS termination %q requires a TLS backend, but webhookServer.insecure is true; use edge termination", termination))
		}
		if !webhookServer.Insecure && termination == routev1.TLSTerminationEdge {
			issues = append(issues, "route TLS termination \"edge\" sends plain HTTP to a TLS backend; set webhookServer.insecure to true or use reencrypt termination")
		}
	}

	issues = append(issues, overriddenImageUpdaterWebhookEnv(cr)...)

	// Settings below are typically left over from exposing the webhook server by hand in earlier versions.
	if env := argoutil.EnvGet(cr.Spec.ImageUpdater.Env, "WEBHOOK_PORT"); env != nil && (env.ValueFrom != nil || env.Value != strconv.Itoa(imageUpdaterWebhookPort)) {
		issues = append(issues, fmt.Sprintf("WEBHOOK_PORT is set in .spec.imageUpdater.env, but the webhook Service and NetworkPolicy only use port %d; remove it", imageUpdaterWebhookPort))
	}
	if !webhookServer.Insecure {
		for _, name := range []string{"TLS_CERT_PATH", "TLS_KEY_PATH"} {
			if argoutil.EnvGet(cr.Spec.ImageUpdater.Env, name) != nil {
				issues = append(issues, fmt.Sprintf("%s is set in .spec.imageUpdater.env, so the webhook server does not use the certificate mounted at %s; remove it", name, imageUpdaterWebhookTLSMountPath))
			}
		}
		if version, ok := upstreamImageUpdaterVersion(selectImageUpdaterImage(cr)); ok && semver.Compare(version, minImageUpdaterWebhookTLSVersion) < 0 {
			issues = append(issues, fmt.Sprintf("Image Updater %s does not read the webhook certificate from %s; use %s or later, or set webhookServer.insecure to true", version, imageUpdaterWebhookTLSMountPath, minImageUpdaterWebhookTLSVersion))
		}
	}

	for _, reg := range imageUpdaterWebhookRegistries {
		ref := reg.declaredRef(cr)
		if ref == nil {
			continue
		}
		value, err := r.fetchSecretKey(cr.Namespace, ref.Name, ref.Key)
		if err != nil {
			return nil, err
		}
		if len(value) == 0 {
			issues = append(issues, fmt.Sprintf("key %q in Secret %q, referenced by webhookServer.secrets.%s, does not exist or is empty, so %s webhooks are rejected", ref.Key, ref.Name, reg.field, reg.field))
		}
	}

	conflicts, err := r.imageUpdaterWebhookOwnershipConflicts(cr)
	if err != nil {
		return nil, err
	}
	issues = append(issues, conflicts...)

	requireSecret := true
	if env := argoutil.EnvGet(cr.Spec.ImageUpdater.Env, "WEBHOOK_REQUIRE_SECRET"); env != nil {
		// A value sourced from elsewhere cannot be evaluated here, so assume the user configured it deliberately.
		if env.ValueFrom != nil {
			requireSecret = false
		} else if v, err := strconv.ParseBool(env.Value); err == nil {
			requireSecret = v
		}
	}
	if requireSecret {
		configured, err := r.hasImageUpdaterWebhookSecret(cr)
		if err != nil {
			return nil, err
		}
		if !configured {
			issues = append(issues, "no registry webhook secret is configured, so the webhook server rejects every request; "+
				"configure webhookServer.secrets, or set WEBHOOK_REQUIRE_SECRET to false in .spec.imageUpdater.env")
		}
	}

	return issues, nil
}

// overriddenImageUpdaterWebhookEnv reports environment variables in .spec.imageUpdater.env that the operator
// replaces with a different value while the webhook is exposed, so that the override is not silent. A variable
// replaced by the value it already has is not reported. Values sourced with valueFrom cannot be compared and are
// always reported.
func overriddenImageUpdaterWebhookEnv(cr *argoproj.ArgoCD) []string {
	userEnv := cr.Spec.ImageUpdater.Env
	var issues []string

	// differs returns true if env is set and does not hold the boolean value want.
	differs := func(env *corev1.EnvVar, want bool) bool {
		if env == nil {
			return false
		}
		if env.ValueFrom != nil {
			return true
		}
		v, err := strconv.ParseBool(env.Value)
		return err != nil || v != want
	}

	if differs(argoutil.EnvGet(userEnv, "ENABLE_WEBHOOK"), true) {
		issues = append(issues, "ENABLE_WEBHOOK in .spec.imageUpdater.env is overridden to true because the webhook is exposed; remove it")
	}
	if differs(argoutil.EnvGet(userEnv, "DISABLE_TLS"), cr.Spec.ImageUpdater.WebhookServer.Insecure) {
		issues = append(issues, fmt.Sprintf("DISABLE_TLS in .spec.imageUpdater.env is overridden by webhookServer.insecure (%t); remove it", cr.Spec.ImageUpdater.WebhookServer.Insecure))
	}
	for _, reg := range imageUpdaterWebhookRegistries {
		if reg.declaredRef(cr) != nil && argoutil.EnvGet(userEnv, reg.envName) != nil {
			issues = append(issues, fmt.Sprintf("%s in .spec.imageUpdater.env is overridden by webhookServer.secrets.%s; remove one of them", reg.envName, reg.field))
		}
	}
	return issues
}

// imageUpdaterWebhookOwnershipConflicts reports each webhook Service, Route or Ingress the operator should manage
// that already exists under the same name but is controlled by another owner or managed by Argo CD.
func (r *ReconcileArgoCD) imageUpdaterWebhookOwnershipConflicts(cr *argoproj.ArgoCD) ([]string, error) {
	name := imageUpdaterWebhookServiceName(cr)
	type candidate struct {
		kind string
		obj  client.Object
	}
	candidates := []candidate{{"Service", &corev1.Service{}}}
	if cr.Spec.ImageUpdater.WebhookServer.Route.Enabled && argoutil.IsRouteAPIAvailable() {
		candidates = append(candidates, candidate{"Route", &routev1.Route{}})
	}
	if cr.Spec.ImageUpdater.WebhookServer.Ingress.Enabled {
		candidates = append(candidates, candidate{"Ingress", &networkingv1.Ingress{}})
	}

	var conflicts []string
	for _, c := range candidates {
		found, err := argoutil.IsObjectFound(r.Client, cr.Namespace, name, c.obj)
		if err != nil {
			return nil, err
		}
		if !found || metav1.IsControlledBy(c.obj, cr) {
			continue
		}
		if controlledByOther(c.obj, cr) {
			controller := metav1.GetControllerOf(c.obj)
			conflicts = append(conflicts, fmt.Sprintf("%s %q is controlled by %s %q, so the operator does not manage it; rename or delete it",
				c.kind, name, controller.Kind, controller.Name))
		} else if marker := argoCDTrackingMarker(c.obj, cr); marker != "" {
			conflicts = append(conflicts, fmt.Sprintf("%s %q has the Argo CD tracking %s, so the operator does not manage it; remove it from the Argo CD Application, or rename or delete it",
				c.kind, name, marker))
		}
	}
	return conflicts, nil
}

// upstreamImageUpdaterVersion returns the version of image if it is an upstream Image Updater image referenced by a
// semantic version tag. Other images, such as product builds or digest references, cannot be mapped to an upstream
// version and return false.
func upstreamImageUpdaterVersion(image string) (string, bool) {
	repo, tag, ok := strings.Cut(image[strings.LastIndex(image, "/")+1:], ":")
	if !ok || strings.Contains(tag, "@") || image[:strings.LastIndex(image, "/")+1]+repo != DefaultImageUpdaterImage {
		return "", false
	}
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}
	if !semver.IsValid(tag) {
		return "", false
	}
	return tag, true
}

// hasImageUpdaterWebhookSecret returns true if at least one registry webhook secret is configured.
func (r *ReconcileArgoCD) hasImageUpdaterWebhookSecret(cr *argoproj.ArgoCD) (bool, error) {
	for _, reg := range imageUpdaterWebhookRegistries {
		if reg.declaredRef(cr) != nil || argoutil.EnvGet(cr.Spec.ImageUpdater.Env, reg.envName) != nil {
			return true, nil
		}
	}
	secret := &corev1.Secret{}
	if err := argoutil.FetchObject(r.Client, cr.Namespace, ArgocdImageUpdaterSecret, secret); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	for _, reg := range imageUpdaterWebhookRegistries {
		if len(secret.Data[reg.fallbackKey]) > 0 {
			return true, nil
		}
	}
	return false, nil
}

// reconcileImageUpdaterWebhookCondition sets or clears the Image Updater webhook configuration condition on the
// in-memory CR. The condition is persisted with the rest of the status at the end of the reconcile.
func (r *ReconcileArgoCD) reconcileImageUpdaterWebhookCondition(cr *argoproj.ArgoCD) error {
	issues, err := r.imageUpdaterWebhookConfigIssues(cr)
	if err != nil {
		return err
	}
	if len(issues) == 0 {
		removeCondition(&cr.Status.Conditions, argoproj.ArgoCDConditionImageUpdaterWebhookConfigurationError)
		return nil
	}
	_, cr.Status.Conditions = insertOrUpdateConditionsInSlice(metav1.Condition{
		Type:    argoproj.ArgoCDConditionImageUpdaterWebhookConfigurationError,
		Status:  metav1.ConditionTrue,
		Reason:  argoproj.ArgoCDConditionReasonImageUpdaterWebhookMisconfig,
		Message: strings.Join(issues, "; "),
	}, cr.Status.Conditions)
	return nil
}

// imageUpdaterWebhookSecretMapper maps a watch event on a Secret read by the Image Updater webhook server back to
// the ArgoCD instances using it. Besides the Secrets referenced under .spec.imageUpdater.webhookServer.secrets, this
// matches the fallback argocd-image-updater-secret and the webhook serving certificate. Those two may have been
// created outside the operator and therefore carry no owner reference, so the owned-object watch does not see them.
func (r *ReconcileArgoCD) imageUpdaterWebhookSecretMapper(ctx context.Context, o client.Object) []reconcile.Request {
	var result []reconcile.Request

	argocds := &argoproj.ArgoCDList{}
	if err := r.List(ctx, argocds, client.InNamespace(o.GetNamespace())); err != nil {
		log.Error(err, "unable to list ArgoCD instances")
		return result
	}

	for i := range argocds.Items {
		argocd := &argocds.Items[i]
		if !argocd.Spec.ImageUpdater.IsWebhookEnabled() {
			continue
		}
		if o.GetName() == ArgocdImageUpdaterSecret ||
			(usesImageUpdaterWebhookTLS(argocd) && o.GetName() == imageUpdaterWebhookTLSSecretName(argocd)) {
			result = append(result, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(argocd)})
			continue
		}
		for _, reg := range imageUpdaterWebhookRegistries {
			if ref := reg.declaredRef(argocd); ref != nil && ref.Name == o.GetName() {
				result = append(result, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(argocd)})
				break
			}
		}
	}
	return result
}
