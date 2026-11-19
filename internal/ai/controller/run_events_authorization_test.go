package controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	aiconfig "github.com/redhat-developer/gitops-operator/argocd-operator/pkg/ai"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestAgenticRunEventSkipsForbiddenInstanceLookup(t *testing.T) {
	for _, lookup := range []string{"ArgoCD list", "enrollment ConfigMap"} {
		t.Run(lookup, func(t *testing.T) {
			s, dyn, _ := incidentFixture(t)
			run := reconcileApp(t, s)
			require.NotNil(t, run)

			deny := true
			calls := 0
			reject := func(ktesting.Action) (bool, runtime.Object, error) {
				calls++
				if !deny {
					return false, nil, nil
				}
				resource, name := argocdGVR.GroupResource(), ""
				if lookup == "enrollment ConfigMap" {
					resource = schema.GroupResource{Resource: "configmaps"}
					name = aiconfig.EnrollmentConfigMapName
				}
				return true, nil, apierrors.NewForbidden(resource, name, fmt.Errorf("access denied"))
			}
			if lookup == "ArgoCD list" {
				dyn.PrependReactor("list", "argocds", reject)
			} else {
				s.kubeClient.(*kubefake.Clientset).PrependReactor("get", "configmaps", reject)
			}

			// resolveManagingInstance wraps both errors. A denied event must
			// return without retrying or waiting for handler shutdown.
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			s.enqueueRunOwner(ctx, run)
			require.Equal(t, 1, calls, "Forbidden is permanent for this event")
			require.NoError(t, ctx.Err())
			require.Zero(t, s.queue.Len())

			// Later authorized events still reach the application workqueue.
			deny = false
			s.enqueueRunOwner(context.Background(), run)
			require.Equal(t, 2, calls)
			require.Equal(t, 1, s.queue.Len())
		})
	}
}

func TestAgenticRunEventRetriesTemporaryInstanceLookupFailure(t *testing.T) {
	s, dyn, _ := incidentFixture(t)
	run := reconcileApp(t, s)
	require.NotNil(t, run)
	calls := 0
	dyn.PrependReactor("list", "argocds", func(ktesting.Action) (bool, runtime.Object, error) {
		calls++
		if calls == 1 {
			return true, nil, apierrors.NewServiceUnavailable("temporary ArgoCD API failure")
		}
		return false, nil, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s.enqueueRunOwner(ctx, run)
	require.NoError(t, ctx.Err())
	require.Equal(t, 2, calls)
	require.Equal(t, 1, s.queue.Len())
}
