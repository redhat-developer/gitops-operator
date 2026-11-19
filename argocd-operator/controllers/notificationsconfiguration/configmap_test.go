package notificationsconfiguration

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"

	"github.com/redhat-developer/gitops-operator/argocd-operator/controllers/argoutil"
	aiconfig "github.com/redhat-developer/gitops-operator/argocd-operator/pkg/ai"

	promoter "github.com/argoproj-labs/gitops-promoter/api/v1alpha1"

	"github.com/redhat-developer/gitops-operator/argocd-operator/api/v1alpha1"
)

type notificationsOpts func(*v1alpha1.NotificationsConfiguration)

type SchemeOpt func(*runtime.Scheme) error

func makeTestNotificationsConfiguration(opts ...notificationsOpts) *v1alpha1.NotificationsConfiguration {
	a := &v1alpha1.NotificationsConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-notifications-configuration",
			Namespace: "default",
		},
	}
	for _, o := range opts {
		o(a)
	}
	return a
}

func managedAINotificationMeta(namespace string) metav1.ObjectMeta {
	controller := true
	return metav1.ObjectMeta{
		Name:      aiNotificationsConfigurationName,
		Namespace: namespace,
		Labels:    map[string]string{aiconfig.EnrollmentManagedByLabel: aiconfig.EnrollmentManager},
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: "argoproj.io/v1beta1",
			Kind:       "ArgoCD",
			Name:       "argocd",
			UID:        "argocd-uid",
			Controller: &controller,
		}},
	}
}

func makeTestReconcilerScheme(sOpts ...SchemeOpt) *runtime.Scheme {
	s := scheme.Scheme
	for _, opt := range sOpts {
		_ = opt(s)
	}

	return s
}

func makeTestReconciler(client client.Client, sch *runtime.Scheme) *NotificationsConfigurationReconciler {
	return &NotificationsConfigurationReconciler{
		Client: client,
		Scheme: sch,
	}
}

func makeTestReconcilerClient(sch *runtime.Scheme, resObjs, subresObjs []client.Object, runtimeObj []runtime.Object) client.Client {
	client := fake.NewClientBuilder().WithScheme(sch)
	if len(resObjs) > 0 {
		client = client.WithObjects(resObjs...)
	}
	if len(subresObjs) > 0 {
		client = client.WithStatusSubresource(subresObjs...)
	}
	if len(runtimeObj) > 0 {
		client = client.WithRuntimeObjects(runtimeObj...)
	}
	return client.Build()
}

func TestReconcileNotifications_CreateConfigMap(t *testing.T) {
	a := makeTestNotificationsConfiguration(func(a *v1alpha1.NotificationsConfiguration) {
	})

	resObjs := []client.Object{a}
	subresObjs := []client.Object{a}
	runtimeObjs := []runtime.Object{}
	sch := makeTestReconcilerScheme(v1alpha1.AddToScheme, promoter.AddToScheme)
	cl := makeTestReconcilerClient(sch, resObjs, subresObjs, runtimeObjs)
	r := makeTestReconciler(cl, sch)

	a.Spec = v1alpha1.NotificationsConfigurationSpec{
		// Add a default template for test
		Templates: map[string]string{
			"template.app-created": `email:
			subject: Application {{.app.metadata.name}} has been created.
		  message: Application {{.app.metadata.name}} has been created.
		  teams:
			title: Application {{.app.metadata.name}} has been created.`,
		},
		// Add a default template for test
		Triggers: map[string]string{
			"trigger.on-created": `- description: Application is created.
			oncePer: app.metadata.name
			send:
			- app-created
			when: "true"`,
		},
	}

	err := r.reconcileNotificationsConfigmap(a)
	assert.NoError(t, err)

	// Verify if the ConfigMap is created
	testCM := &corev1.ConfigMap{}
	assert.NoError(t, r.Get(
		context.TODO(),
		types.NamespacedName{
			Name:      ArgoCDNotificationsConfigMap,
			Namespace: a.Namespace,
		},
		testCM))
	assert.True(t, argoutil.IsTrackedByOperator(testCM.Labels))

	// Verify that the configmap has the default template
	assert.NotEqual(t, testCM.Data["template.app-created"], "")

	// Verify that the configmap has the default trigger
	assert.NotEqual(t, testCM.Data["trigger.on-created"], "")
}

func TestReconcileNotifications_UpdateConfigMap(t *testing.T) {
	a := makeTestNotificationsConfiguration(func(a *v1alpha1.NotificationsConfiguration) {
	})

	resObjs := []client.Object{a}
	subresObjs := []client.Object{a}
	runtimeObjs := []runtime.Object{}
	sch := makeTestReconcilerScheme(v1alpha1.AddToScheme, promoter.AddToScheme)
	cl := makeTestReconcilerClient(sch, resObjs, subresObjs, runtimeObjs)
	r := makeTestReconciler(cl, sch)

	a.Spec = v1alpha1.NotificationsConfigurationSpec{
		// Add a default template for test
		Templates: map[string]string{
			"template.app-created": `email:
			subject: Application {{.app.metadata.name}} has been created.
		  message: Application {{.app.metadata.name}} has been created.
		  teams:
			title: Application {{.app.metadata.name}} has been created.`,
		},
		// Add a default template for test
		Triggers: map[string]string{
			"trigger.on-created": `- description: Application is created.
			oncePer: app.metadata.name
			send:
			- app-created
			when: "true"`,
		},
	}

	err := r.reconcileNotificationsConfigmap(a)
	assert.NoError(t, err)

	// Verify if the ConfigMap is created
	testCM := &corev1.ConfigMap{}
	assert.NoError(t, r.Get(
		context.TODO(),
		types.NamespacedName{
			Name:      ArgoCDNotificationsConfigMap,
			Namespace: a.Namespace,
		},
		testCM))

	// Update the NotificationsConfiguration
	a.Spec.Triggers["trigger.on-sync-status-test"] = "- when: app.status.sync.status == 'Unknown' \n send: [my-custom-template]"

	err = r.reconcileNotificationsConfigmap(a)
	assert.NoError(t, err)

	testCM = &corev1.ConfigMap{}
	assert.NoError(t, r.Get(
		context.TODO(),
		types.NamespacedName{
			Name:      ArgoCDNotificationsConfigMap,
			Namespace: a.Namespace,
		},
		testCM))
	assert.True(t, argoutil.IsTrackedByOperator(testCM.Labels))
	// Verify that the updated configuration
	assert.Equal(t, testCM.Data["trigger.on-sync-status-test"],
		"- when: app.status.sync.status == 'Unknown' \n send: [my-custom-template]")
}

func TestReconcileNotifications_DeleteConfigMap(t *testing.T) {
	a := makeTestNotificationsConfiguration(func(a *v1alpha1.NotificationsConfiguration) {
	})

	resObjs := []client.Object{a}
	subresObjs := []client.Object{a}
	runtimeObjs := []runtime.Object{}
	sch := makeTestReconcilerScheme(v1alpha1.AddToScheme, promoter.AddToScheme)
	cl := makeTestReconcilerClient(sch, resObjs, subresObjs, runtimeObjs)
	r := makeTestReconciler(cl, sch)

	// Update the NotificationsConfiguration
	a.Spec = v1alpha1.NotificationsConfigurationSpec{
		Triggers: map[string]string{
			"trigger.on-sync-status-test": "- when: app.status.sync.status == 'Unknown' \n send: [my-custom-template]",
		},
	}

	err := r.reconcileNotificationsConfigmap(a)
	assert.NoError(t, err)

	// Delete the Notifications ConfigMap
	testCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ArgoCDNotificationsConfigMap,
			Namespace: a.Namespace,
		},
	}
	assert.NoError(t, r.Delete(
		context.TODO(), testCM))

	// Reconcile to check if the ConfigMap is recreated
	err = r.reconcileNotificationsConfigmap(a)
	assert.NoError(t, err)

	assert.NoError(t, r.Get(
		context.TODO(),
		types.NamespacedName{
			Name:      ArgoCDNotificationsConfigMap,
			Namespace: a.Namespace,
		},
		testCM))
	assert.True(t, argoutil.IsTrackedByOperator(testCM.Labels))
	// Verify if ConfigMap is created with required data
	assert.Equal(t, testCM.Data["trigger.on-sync-status-test"],
		"- when: app.status.sync.status == 'Unknown' \n send: [my-custom-template]")
}

func TestReconcileNotifications_MergesAINCCR(t *testing.T) {
	sch := makeTestReconcilerScheme(v1alpha1.AddToScheme, promoter.AddToScheme)

	defaultNC := makeTestNotificationsConfiguration(func(a *v1alpha1.NotificationsConfiguration) {
		a.Spec = v1alpha1.NotificationsConfigurationSpec{
			Triggers:      map[string]string{"trigger.on-sync-failed": "user-trigger"},
			Subscriptions: map[string]string{"subscriptions": "- recipients:\n  - slack:ops\n  triggers:\n  - on-sync-failed\n"},
		}
	})
	aiNC := &v1alpha1.NotificationsConfiguration{
		ObjectMeta: managedAINotificationMeta(defaultNC.Namespace),
		Spec: v1alpha1.NotificationsConfigurationSpec{
			Triggers:      map[string]string{"trigger.on-gitops-ai-diagnose": "ai-trigger"},
			Templates:     map[string]string{"template.gitops-ai-diagnose": "ai-template"},
			Services:      map[string]string{"service.webhook.gitops-ai-controller": "ai-service"},
			Subscriptions: map[string]string{"subscriptions": "- recipients:\n  - gitops-ai-controller\n  triggers:\n  - on-gitops-ai-diagnose\n"},
		},
	}

	cl := makeTestReconcilerClient(sch, []client.Object{defaultNC, aiNC}, nil, nil)
	r := makeTestReconciler(cl, sch)

	// Reconcile triggered by the default NC CR.
	if err := r.reconcileNotificationsConfigmap(defaultNC); err != nil {
		t.Fatal(err)
	}

	cm := &corev1.ConfigMap{}
	if err := r.Get(context.TODO(), types.NamespacedName{Name: ArgoCDNotificationsConfigMap, Namespace: defaultNC.Namespace}, cm); err != nil {
		t.Fatal(err)
	}

	// User trigger from default NC CR.
	if cm.Data["trigger.on-sync-failed"] != "user-trigger" {
		t.Errorf("user trigger missing or incorrect: %v", cm.Data)
	}
	// AI entries from AI NC CR.
	if cm.Data["trigger.on-gitops-ai-diagnose"] != "ai-trigger" {
		t.Errorf("AI trigger missing: %v", cm.Data)
	}
	if cm.Data["template.gitops-ai-diagnose"] != "ai-template" {
		t.Errorf("AI template missing: %v", cm.Data)
	}
	if cm.Data["service.webhook.gitops-ai-controller"] != "ai-service" {
		t.Errorf("AI service missing: %v", cm.Data)
	}
	// Subscriptions must contain both user and AI entries.
	subs := cm.Data["subscriptions"]
	if !strings.Contains(subs, "slack:ops") || !strings.Contains(subs, "gitops-ai-controller") {
		t.Errorf("subscriptions missing expected entries: %v", subs)
	}

	// Deleting the AI NC CR and reconciling via default must remove AI entries from CM.
	if err := r.Delete(context.TODO(), aiNC); err != nil {
		t.Fatal(err)
	}
	if err := r.reconcileNotificationsConfigmap(defaultNC); err != nil {
		t.Fatal(err)
	}
	if err := r.Get(context.TODO(), types.NamespacedName{Name: ArgoCDNotificationsConfigMap, Namespace: defaultNC.Namespace}, cm); err != nil {
		t.Fatal(err)
	}
	if cm.Data["trigger.on-gitops-ai-diagnose"] != "" {
		t.Errorf("AI trigger not removed after AI NC CR deletion: %v", cm.Data)
	}
	if strings.Contains(cm.Data["subscriptions"], "gitops-ai-controller") {
		t.Errorf("AI subscription not removed after AI NC CR deletion: %v", cm.Data["subscriptions"])
	}
	if !strings.Contains(cm.Data["subscriptions"], "slack:ops") {
		t.Errorf("user subscription lost after AI NC CR deletion: %v", cm.Data["subscriptions"])
	}
}

func Test_checkIfContextEquals(t *testing.T) {
	a := makeTestNotificationsConfiguration(func(a *v1alpha1.NotificationsConfiguration) {})
	a.Spec = v1alpha1.NotificationsConfigurationSpec{
		Context: map[string]string{
			"key1": "value1",
			"key2": "value2",
			"key4": "value4",
			"key3": "value3",
			"key6": "value6",
		},
	}

	testmap := []struct {
		testcase string
		cm       corev1.ConfigMap
		result   bool
	}{
		{
			"equal context",
			corev1.ConfigMap{Data: map[string]string{"context": "key4: value4\nkey2: value2\nkey6: value6\nkey1: value1\nkey3: value3\n"}},
			false,
		},
		{
			"context is not equal",
			corev1.ConfigMap{Data: map[string]string{"context": "key1: value1\nkey4: value4\nkey9: value9\nkey6: value6\n"}},
			true,
		},
		{
			"context of same length but not equal",
			corev1.ConfigMap{Data: map[string]string{"context": "key2: value2\nkey1: value1\nkey4: value4\nkey9: value9\nkey6: value6\n"}},
			true,
		},
	}
	for _, tt := range testmap {
		t.Run(tt.testcase, func(t *testing.T) {
			assert.Equal(t, tt.result, checkIfContextChanged(a, &tt.cm))
		})
	}
}

func TestReconcileNotifications_ManagedConfigurationWinsConflicts(t *testing.T) {
	sch := makeTestReconcilerScheme(v1alpha1.AddToScheme, promoter.AddToScheme)
	defaultNC := makeTestNotificationsConfiguration(func(a *v1alpha1.NotificationsConfiguration) {
		a.Spec = v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{
				"trigger.on-gitops-ai-diagnose": "user-ai-trigger",
				"trigger.on-sync-failed":        "user-trigger",
			},
			Templates: map[string]string{
				"template.gitops-ai-diagnose": "user-ai-template",
				"template.user":               "user-template",
			},
			Services: map[string]string{
				"service.webhook.gitops-ai-controller": "user-ai-service",
				"service.slack.gitops-ai-controller":   "user-shadow-service",
				"service.gitops-ai-controller":         "user-shadow-unnamed-service",
				"service.slack":                        "user-service",
				"service.slack.operations":             "user-named-service",
			},
			Context: map[string]string{"shared": "user-value", "user-only": "preserved"},
			Subscriptions: map[string]string{"subscriptions": `- recipients: [gitops-ai-controller]
  triggers: [on-gitops-ai-diagnose]
  selector: team=restricted
- recipients: [gitops-ai-controller, gitops-ai-controller:any, "gitops-ai-controller:", slack:ops]
  triggers: [on-sync-failed]
  selector: team=operations
- recipients: [gitops-ai-controller:another]
  triggers: [on-sync-failed]
  selector: team=restricted
- recipients: [slack:ai-observers]
  triggers: [on-gitops-ai-diagnose]
`},
		}
	})
	aiNC := &v1alpha1.NotificationsConfiguration{
		ObjectMeta: managedAINotificationMeta(defaultNC.Namespace),
		Spec: v1alpha1.NotificationsConfigurationSpec{
			Triggers:      map[string]string{"trigger.on-gitops-ai-diagnose": "ai-trigger"},
			Templates:     map[string]string{"template.gitops-ai-diagnose": "ai-template"},
			Services:      map[string]string{"service.webhook.gitops-ai-controller": "ai-service"},
			Context:       map[string]string{"shared": "operator-value", "operator-only": "managed"},
			Subscriptions: map[string]string{"subscriptions": "- recipients: [gitops-ai-controller]\n  triggers: [on-gitops-ai-diagnose]\n"},
		},
	}
	originalDefault := defaultNC.DeepCopy()
	originalAI := aiNC.DeepCopy()
	cl := makeTestReconcilerClient(sch, []client.Object{defaultNC, aiNC}, nil, nil)
	r := makeTestReconciler(cl, sch)
	require.NoError(t, r.reconcileNotificationsConfigmap(aiNC))

	cm := &corev1.ConfigMap{}
	cmKey := types.NamespacedName{Name: ArgoCDNotificationsConfigMap, Namespace: defaultNC.Namespace}
	require.NoError(t, r.Get(context.Background(), cmKey, cm))
	assert.Equal(t, "ai-trigger", cm.Data["trigger.on-gitops-ai-diagnose"])
	assert.Equal(t, "ai-template", cm.Data["template.gitops-ai-diagnose"])
	assert.Equal(t, "ai-service", cm.Data["service.webhook.gitops-ai-controller"])
	assert.NotContains(t, cm.Data, "service.slack.gitops-ai-controller")
	assert.NotContains(t, cm.Data, "service.gitops-ai-controller")
	assert.Equal(t, "user-trigger", cm.Data["trigger.on-sync-failed"])
	assert.Equal(t, "user-template", cm.Data["template.user"])
	assert.Equal(t, "user-service", cm.Data["service.slack"])
	assert.Equal(t, "user-named-service", cm.Data["service.slack.operations"])

	var actualContext map[string]string
	require.NoError(t, yaml.Unmarshal([]byte(cm.Data["context"]), &actualContext))
	assert.Equal(t, map[string]string{"shared": "operator-value", "user-only": "preserved", "operator-only": "managed"}, actualContext)
	var subscriptions []notificationSubscription
	require.NoError(t, yaml.Unmarshal([]byte(cm.Data["subscriptions"]), &subscriptions))
	assert.Equal(t, []notificationSubscription{
		{Recipients: []string{"slack:ops"}, Triggers: []string{"on-sync-failed"}, Selector: "team=operations"},
		{Recipients: []string{"slack:ai-observers"}, Triggers: []string{"on-gitops-ai-diagnose"}},
		{Recipients: []string{"gitops-ai-controller"}, Triggers: []string{"on-gitops-ai-diagnose"}},
	}, subscriptions)

	// The merger must not write its result back into either source CR.
	storedDefault := &v1alpha1.NotificationsConfiguration{}
	storedAI := &v1alpha1.NotificationsConfiguration{}
	require.NoError(t, r.Get(context.Background(), client.ObjectKeyFromObject(defaultNC), storedDefault))
	require.NoError(t, r.Get(context.Background(), client.ObjectKeyFromObject(aiNC), storedAI))
	assert.Equal(t, originalDefault.Spec, storedDefault.Spec)
	assert.Equal(t, originalAI.Spec, storedAI.Spec)
	assert.Equal(t, originalDefault.Spec, defaultNC.Spec)
	assert.Equal(t, originalAI.Spec, aiNC.Spec)

	// Disabling AI restores the original user choices, including subscriptions
	// that were superseded while the operator owned the AI recipient.
	require.NoError(t, r.Delete(context.Background(), aiNC))
	require.NoError(t, r.reconcileNotificationsConfigmap(defaultNC))
	require.NoError(t, r.Get(context.Background(), cmKey, cm))
	assert.Equal(t, "user-ai-trigger", cm.Data["trigger.on-gitops-ai-diagnose"])
	assert.Equal(t, "user-ai-template", cm.Data["template.gitops-ai-diagnose"])
	assert.Equal(t, "user-ai-service", cm.Data["service.webhook.gitops-ai-controller"])
	assert.Equal(t, "user-shadow-service", cm.Data["service.slack.gitops-ai-controller"])
	assert.Equal(t, "user-shadow-unnamed-service", cm.Data["service.gitops-ai-controller"])
	assert.Equal(t, originalDefault.Spec.Subscriptions["subscriptions"], cm.Data["subscriptions"])
	actualContext = nil
	require.NoError(t, yaml.Unmarshal([]byte(cm.Data["context"]), &actualContext))
	assert.Equal(t, originalDefault.Spec.Context, actualContext)
}

func TestReconcileNotifications_RemovesStaleKeysWithUnchangedContext(t *testing.T) {
	sch := makeTestReconcilerScheme(v1alpha1.AddToScheme, promoter.AddToScheme)
	defaultNC := makeTestNotificationsConfiguration(func(a *v1alpha1.NotificationsConfiguration) {
		a.Spec.Context = map[string]string{"argocdUrl": "https://argocd.example.com"}
		a.Spec.Triggers = map[string]string{"trigger.on-sync-failed": "user-trigger"}
	})
	aiNC := &v1alpha1.NotificationsConfiguration{
		ObjectMeta: managedAINotificationMeta(defaultNC.Namespace),
		Spec: v1alpha1.NotificationsConfigurationSpec{
			Triggers:      map[string]string{"trigger.on-gitops-ai-diagnose": "ai-trigger"},
			Templates:     map[string]string{"template.gitops-ai-diagnose": "ai-template"},
			Services:      map[string]string{"service.webhook.gitops-ai-controller": "ai-service"},
			Subscriptions: map[string]string{"subscriptions": "- recipients: [gitops-ai-controller]\n  triggers: [on-gitops-ai-diagnose]\n"},
		},
	}
	cl := makeTestReconcilerClient(sch, []client.Object{defaultNC, aiNC}, nil, nil)
	r := makeTestReconciler(cl, sch)
	require.NoError(t, r.reconcileNotificationsConfigmap(defaultNC))
	require.NoError(t, r.Delete(context.Background(), aiNC))
	require.NoError(t, r.reconcileNotificationsConfigmap(defaultNC))
	cm := &corev1.ConfigMap{}
	require.NoError(t, r.Get(context.Background(), types.NamespacedName{Name: ArgoCDNotificationsConfigMap, Namespace: defaultNC.Namespace}, cm))
	assert.Equal(t, map[string]string{
		"context":                "argocdUrl: https://argocd.example.com\n",
		"trigger.on-sync-failed": "user-trigger",
	}, cm.Data)
}

func TestReconcileNotifications_UnchangedConfigurationDoesNotUpdate(t *testing.T) {
	for _, contextData := range []map[string]string{nil, {}, {"first": "one", "second": "two"}} {
		t.Run(mapToString(contextData), func(t *testing.T) {
			sch := makeTestReconcilerScheme(v1alpha1.AddToScheme, promoter.AddToScheme)
			defaultNC := makeTestNotificationsConfiguration(func(a *v1alpha1.NotificationsConfiguration) {
				a.Spec.Context = contextData
			})
			cl := makeTestReconcilerClient(sch, []client.Object{defaultNC}, nil, nil)
			r := makeTestReconciler(cl, sch)
			require.NoError(t, r.reconcileNotificationsConfigmap(defaultNC))
			cm := &corev1.ConfigMap{}
			key := types.NamespacedName{Name: ArgoCDNotificationsConfigMap, Namespace: defaultNC.Namespace}
			require.NoError(t, r.Get(context.Background(), key, cm))
			version := cm.ResourceVersion
			require.NoError(t, r.reconcileNotificationsConfigmap(defaultNC))
			require.NoError(t, r.Get(context.Background(), key, cm))
			assert.Equal(t, version, cm.ResourceVersion)
		})
	}
}

func TestMergeSubscriptionYAMLRejectsMalformedConfiguration(t *testing.T) {
	valid := "- recipients: [gitops-ai-controller]\n  triggers: [on-gitops-ai-diagnose]\n"
	for _, subscriptions := range []string{"[", "recipients: [gitops-ai-controller]", "- recipients: not-a-list"} {
		t.Run(subscriptions, func(t *testing.T) {
			_, err := mergeSubscriptionYAML(subscriptions, valid, nil)
			require.ErrorContains(t, err, "parsing default subscriptions")
			_, err = mergeSubscriptionYAML(valid, subscriptions, nil)
			require.ErrorContains(t, err, "parsing managed subscriptions")
		})
	}
}

func TestMergeSubscriptionYAMLPreservesUnrelatedServiceRecipients(t *testing.T) {
	defaults := "- recipients: [slack:ai, slack:ops]\n  triggers: [on-sync-failed]\n  selector: team=operations\n"
	managed := "- recipients: [slack:ai]\n  triggers: [on-gitops-ai-diagnose]\n"
	merged, err := mergeSubscriptionYAML(defaults, managed, map[string]string{"service.slack": "operator-slack-service"})
	require.NoError(t, err)
	var subscriptions []notificationSubscription
	require.NoError(t, yaml.Unmarshal([]byte(merged), &subscriptions))
	assert.Equal(t, []notificationSubscription{
		{Recipients: []string{"slack:ops"}, Triggers: []string{"on-sync-failed"}, Selector: "team=operations"},
		{Recipients: []string{"slack:ai"}, Triggers: []string{"on-gitops-ai-diagnose"}},
	}, subscriptions)
}

func TestReconcileNotifications_IgnoresUnmanagedAIConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*v1alpha1.NotificationsConfiguration)
	}{
		{"missing management label", func(cr *v1alpha1.NotificationsConfiguration) { cr.Labels = nil }},
		{"another manager", func(cr *v1alpha1.NotificationsConfiguration) {
			cr.Labels[aiconfig.EnrollmentManagedByLabel] = "someone-else"
		}},
		{"no owner", func(cr *v1alpha1.NotificationsConfiguration) { cr.OwnerReferences = nil }},
		{"non-controller owner", func(cr *v1alpha1.NotificationsConfiguration) { cr.OwnerReferences[0].Controller = nil }},
		{"wrong owner kind", func(cr *v1alpha1.NotificationsConfiguration) { cr.OwnerReferences[0].Kind = "Application" }},
		{"wrong owner group", func(cr *v1alpha1.NotificationsConfiguration) { cr.OwnerReferences[0].APIVersion = "example.com/v1" }},
		{"missing owner UID", func(cr *v1alpha1.NotificationsConfiguration) { cr.OwnerReferences[0].UID = "" }},
		{"missing owner name", func(cr *v1alpha1.NotificationsConfiguration) { cr.OwnerReferences[0].Name = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sch := makeTestReconcilerScheme(v1alpha1.AddToScheme, promoter.AddToScheme)
			defaultNC := makeTestNotificationsConfiguration(func(a *v1alpha1.NotificationsConfiguration) {
				a.Spec.Triggers = map[string]string{"trigger.on-gitops-ai-diagnose": "user-trigger"}
			})
			aiNC := &v1alpha1.NotificationsConfiguration{
				ObjectMeta: managedAINotificationMeta(defaultNC.Namespace),
				Spec: v1alpha1.NotificationsConfigurationSpec{
					Triggers: map[string]string{"trigger.on-gitops-ai-diagnose": "unmanaged-trigger"},
				},
			}
			tt.mutate(aiNC)
			cl := makeTestReconcilerClient(sch, []client.Object{defaultNC, aiNC}, nil, nil)
			r := makeTestReconciler(cl, sch)
			require.NoError(t, r.reconcileNotificationsConfigmap(aiNC))
			cm := &corev1.ConfigMap{}
			require.NoError(t, r.Get(context.Background(), types.NamespacedName{Name: ArgoCDNotificationsConfigMap, Namespace: defaultNC.Namespace}, cm))
			assert.Equal(t, map[string]string{"trigger.on-gitops-ai-diagnose": "user-trigger"}, cm.Data)
		})
	}
}
