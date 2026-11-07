//go:build envtest
// +build envtest

package notificationsconfiguration

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/redhat-developer/gitops-operator/argocd-operator/api/v1alpha1"
	"github.com/redhat-developer/gitops-operator/argocd-operator/controllers/argoutil"
)

const (
	envtestTimeout  = 30 * time.Second
	envtestInterval = 250 * time.Millisecond
)

// setupControllerForTest mirrors production watches and allows the same controller
// name to be registered again when the test restarts the manager.
func setupControllerForTest(mgr ctrl.Manager) error {
	r := &NotificationsConfigurationReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}
	skip := true
	return ctrl.NewControllerManagedBy(mgr).
		Named("notificationsconfiguration").
		WithOptions(controller.Options{SkipNameValidation: &skip}).
		Watches(
			&v1alpha1.NotificationsConfiguration{},
			handler.EnqueueRequestsFromMapFunc(mapCRToConfigMapRequest),
		).
		Watches(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(mapConfigMapToReconcileRequest),
		).
		Complete(r)
}

func cmKeyForNS(ns string) types.NamespacedName {
	return types.NamespacedName{Namespace: ns, Name: ArgoCDNotificationsConfigMap}
}

// TestIntegration exercises watch-driven reconciliation with an envtest API server.
//
// Explicit test command:
//
//	KUBEBUILDER_ASSETS="$(setup-envtest use 1.26 --print path)" \
//	  go test -tags envtest -count=1 -v -timeout 300s \
//	  ./controllers/notificationsconfiguration/... -run TestIntegration
//
// Scenarios use separate namespaces and share one API server. The downtime
// scenario restarts the manager to check recovery from missed events.
func TestIntegration(t *testing.T) {
	logf.SetLogger(zap.New(zap.UseDevMode(true)))

	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := env.Start()
	require.NoError(t, err, "failed to start envtest")
	t.Cleanup(func() {
		require.NoError(t, env.Stop(), "failed to stop envtest")
	})

	require.NoError(t, v1alpha1.AddToScheme(scheme.Scheme))

	directClient, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)

	ensureNS := func(name string) {
		t.Helper()
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
		err := directClient.Create(context.TODO(), ns)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			require.NoError(t, err)
		}
	}

	startMgr := func() (context.CancelFunc, error) {
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: scheme.Scheme})
		if err != nil {
			return nil, err
		}
		if err := setupControllerForTest(mgr); err != nil {
			return nil, err
		}
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			if err := mgr.Start(ctx); err != nil {
				t.Logf("manager stopped: %v", err)
			}
		}()
		return cancel, nil
	}

	waitAbsent := func(ns string) {
		t.Helper()
		require.Eventually(t, func() bool {
			err := directClient.Get(context.TODO(), cmKeyForNS(ns), &corev1.ConfigMap{})
			return apierrors.IsNotFound(err)
		}, envtestTimeout, envtestInterval, "ConfigMap in %s should be absent", ns)
	}

	waitPresent := func(ns string) *corev1.ConfigMap {
		t.Helper()
		var found corev1.ConfigMap
		require.Eventually(t, func() bool {
			return directClient.Get(context.TODO(), cmKeyForNS(ns), &found) == nil
		}, envtestTimeout, envtestInterval, "ConfigMap in %s should be present", ns)
		return &found
	}

	waitData := func(ns, key, value string) *corev1.ConfigMap {
		t.Helper()
		var found corev1.ConfigMap
		require.Eventually(t, func() bool {
			if directClient.Get(context.TODO(), cmKeyForNS(ns), &found) != nil {
				return false
			}
			return found.Data[key] == value
		}, envtestTimeout, envtestInterval, "ConfigMap in %s: %s should be %s", ns, key, value)
		return &found
	}

	makeCR := func(ns, name string, triggers map[string]string) *v1alpha1.NotificationsConfiguration {
		t.Helper()
		cr := &v1alpha1.NotificationsConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec:       v1alpha1.NotificationsConfigurationSpec{Triggers: triggers},
		}
		require.NoError(t, directClient.Create(context.TODO(), cr))
		return cr
	}

	// ---------------------------------------------------------------
	// Pre-populate namespaces and initial state BEFORE the first manager
	// ---------------------------------------------------------------

	// Scenario 1: orphan cleanup — managed CM, no CRs
	const ns1 = "integ-orphan"
	ensureNS(ns1)
	orphanedCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: ArgoCDNotificationsConfigMap, Namespace: ns1},
		Data:       map[string]string{"trigger.stale": "should-be-cleaned"},
	}
	argoutil.AddTrackedByOperatorLabel(&orphanedCM.ObjectMeta)
	require.NoError(t, directClient.Create(context.TODO(), orphanedCM))

	// Scenario 2: startup creation — CRs exist, no CM
	const ns2 = "integ-create"
	ensureNS(ns2)
	makeCR(ns2, DefaultNotificationsConfigurationName, map[string]string{"trigger.startup": "when: true"})

	// Scenario 3: convergence — both CR and CM exist with matching data
	const ns3 = "integ-converge"
	ensureNS(ns3)
	makeCR(ns3, DefaultNotificationsConfigurationName, map[string]string{"trigger.x": "correct-value"})
	existingCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: ArgoCDNotificationsConfigMap, Namespace: ns3},
		Data:       map[string]string{"trigger.x": "correct-value"},
	}
	argoutil.AddTrackedByOperatorLabel(&existingCM.ObjectMeta)
	require.NoError(t, directClient.Create(context.TODO(), existingCM))
	var cmBefore corev1.ConfigMap
	require.NoError(t, directClient.Get(context.TODO(), cmKeyForNS(ns3), &cmBefore))
	originalUID := cmBefore.UID

	// Scenario 4: downtime — create CR and let manager process it first
	const ns4 = "integ-downtime"
	ensureNS(ns4)

	// Scenario 5: live events
	const ns5 = "integ-live"
	ensureNS(ns5)

	// ---------------------------------------------------------------
	// Start manager 1 — covers scenarios 1, 2, 3, 4-phase1, 5
	// ---------------------------------------------------------------
	cancel1, err := startMgr()
	require.NoError(t, err, "failed to start first manager")

	t.Run("Scenario1_StartupOrphanCleanup", func(t *testing.T) {
		waitAbsent(ns1)
	})

	t.Run("Scenario2_StartupCreation", func(t *testing.T) {
		cm := waitPresent(ns2)
		assert.Equal(t, "when: true", cm.Data["trigger.startup"])
		assert.True(t, argoutil.IsTrackedByOperator(cm.Labels))
		assert.Empty(t, cm.OwnerReferences)
	})

	t.Run("Scenario3_StartupConvergence_UIDUnchanged", func(t *testing.T) {
		time.Sleep(3 * time.Second)
		var cmAfter corev1.ConfigMap
		require.NoError(t, directClient.Get(context.TODO(), cmKeyForNS(ns3), &cmAfter))
		assert.Equal(t, originalUID, cmAfter.UID, "ConfigMap UID must remain unchanged")
		assert.Equal(t, "correct-value", cmAfter.Data["trigger.x"])
	})

	// Scenario 4 phase 1: let the manager create the CM, then stop it
	downtimeCR := makeCR(ns4, "downtime-cr", map[string]string{"trigger.x": "alive"})
	waitPresent(ns4)

	cancel1()
	time.Sleep(1 * time.Second)

	// While manager is stopped, delete the CR
	require.NoError(t, directClient.Delete(context.TODO(), downtimeCR))
	require.Eventually(t, func() bool {
		err := directClient.Get(context.TODO(), types.NamespacedName{
			Namespace: ns4, Name: "downtime-cr",
		}, &v1alpha1.NotificationsConfiguration{})
		return apierrors.IsNotFound(err)
	}, envtestTimeout, envtestInterval, "CR should be deleted")

	// ---------------------------------------------------------------
	// Start manager 2 — covers scenario 4-phase2 and scenario 5
	// ---------------------------------------------------------------
	cancel2, err := startMgr()
	require.NoError(t, err, "failed to start second manager")
	defer cancel2()

	t.Run("Scenario4_MissedDeletionDuringDowntime", func(t *testing.T) {
		waitAbsent(ns4)
	})

	t.Run("Scenario5_LiveEvents", func(t *testing.T) {
		// Create two CRs
		crA := makeCR(ns5, "cr-a", map[string]string{"trigger.a": "val-a"})
		cm := waitData(ns5, "trigger.a", "val-a")
		assert.True(t, argoutil.IsTrackedByOperator(cm.Labels))

		crB := makeCR(ns5, "cr-b", map[string]string{"trigger.b": "val-b"})
		require.Eventually(t, func() bool {
			var cm corev1.ConfigMap
			if directClient.Get(context.TODO(), cmKeyForNS(ns5), &cm) != nil {
				return false
			}
			return cm.Data["trigger.a"] == "val-a" && cm.Data["trigger.b"] == "val-b"
		}, envtestTimeout, envtestInterval, "both triggers present")

		// Update crA
		crA.Spec.Triggers["trigger.a"] = "updated-a"
		require.NoError(t, directClient.Update(context.TODO(), crA))
		waitData(ns5, "trigger.a", "updated-a")

		// Delete crA
		require.NoError(t, directClient.Delete(context.TODO(), crA))
		require.Eventually(t, func() bool {
			var cm corev1.ConfigMap
			if directClient.Get(context.TODO(), cmKeyForNS(ns5), &cm) != nil {
				return false
			}
			_, hasA := cm.Data["trigger.a"]
			return !hasA && cm.Data["trigger.b"] == "val-b"
		}, envtestTimeout, envtestInterval, "trigger.a gone, trigger.b remains")

		// Tamper with ConfigMap data → controller should correct it
		var cmObj corev1.ConfigMap
		require.NoError(t, directClient.Get(context.TODO(), cmKeyForNS(ns5), &cmObj))
		cmObj.Data["trigger.b"] = "tampered"
		require.NoError(t, directClient.Update(context.TODO(), &cmObj))
		waitData(ns5, "trigger.b", "val-b")

		// Delete ConfigMap while CRs remain → controller should recreate it
		require.NoError(t, directClient.Delete(context.TODO(), &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: ArgoCDNotificationsConfigMap, Namespace: ns5},
		}))
		cm = waitPresent(ns5)
		assert.Equal(t, "val-b", cm.Data["trigger.b"])

		// Delete last CR → ConfigMap should be cleaned up
		require.NoError(t, directClient.Delete(context.TODO(), crB))
		waitAbsent(ns5)
	})
}
