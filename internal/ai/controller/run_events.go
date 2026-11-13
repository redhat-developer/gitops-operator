package controller

import (
	"context"
	"strings"
	"time"

	"github.com/redhat-developer/gitops-operator/internal/ai/utils"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
)

// AgenticRun events only identify an Application that may need reconciliation.
// All diagnosis and retry decisions remain in reconcileDiagnosis.
func (s *TriggerService) enqueueRunOwner(ctx context.Context, obj any) {
	run, ok := obj.(*unstructured.Unstructured)
	if !ok {
		if tombstone, found := obj.(cache.DeletedFinalStateUnknown); found {
			run, ok = tombstone.Obj.(*unstructured.Unstructured)
		}
	}
	if !ok || run.GetNamespace() != s.config.RunNamespace || run.GetLabels()[utils.ManagedBy] != utils.Manager {
		return
	}
	name := run.GetAnnotations()[utils.AppName]
	namespace := run.GetLabels()[utils.AppNamespace]
	uid := run.GetLabels()[utils.AppUID]
	if name == "" || namespace == "" || uid == "" {
		return
	}
	// Retain this event during a temporary Application API failure. Without
	// polling, dropping the only terminal-status event could lose attempt 2.
	backoff := 100 * time.Millisecond
	for {
		app, err := s.getApplication(ctx, namespace, name)
		if apierrors.IsNotFound(err) || apierrors.IsForbidden(err) || ctx.Err() != nil {
			return
		}
		if err == nil {
			if string(app.GetUID()) == uid {
				s.enqueueApplication(namespace, name)
			}
			return
		}
		s.logger.Error(err, "Could not validate AgenticRun owner; retrying event", "run", run.GetName())
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if backoff < 5*time.Second {
			backoff *= 2
		}
	}
}

// waitForCRD blocks until the agenticRunGVR CRD is available in the cluster,
// logging once and then polling with exponential backoff. Returns false if ctx
// is cancelled before the CRD appears.
func (s *TriggerService) waitForCRD(ctx context.Context) bool {
	s.logger.Info("Waiting for CRD to become available", "resource", agenticRunGVR.Resource, "group", agenticRunGVR.Group)
	backoff := 5 * time.Second
	for {
		_, err := s.dynClient.Resource(agenticRunGVR).Namespace(s.config.RunNamespace).List(ctx, metav1.ListOptions{Limit: 1})
		if err == nil {
			return true
		}
		if apierrors.IsNotFound(err) || isNoResourceError(err) {
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return false
			case <-timer.C:
			}
			if backoff < 5*time.Minute {
				backoff *= 2
			}
			continue
		}
		// unexpected error — log and keep retrying
		s.logger.Error(err, "Unexpected error waiting for CRD", "resource", agenticRunGVR.Resource)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}

// isNoResourceError returns true for the "server could not find the requested resource"
// error that the dynamic client returns when a CRD is not installed.
func isNoResourceError(err error) bool {
	if err == nil {
		return false
	}
	return apierrors.ReasonForError(err) == metav1.StatusReasonNotFound ||
		strings.Contains(err.Error(), "could not find the requested resource")
}

// The initial informer list is needed after restart: it replays existing run
// events so persisted RetryAt deadlines can be scheduled again. Resync is zero.
func (s *TriggerService) runAgenticRunEvents(ctx context.Context) {
	if !s.waitForCRD(ctx) {
		return
	}
	s.logger.Info("CRD available, starting AgenticRun watcher", "resource", agenticRunGVR.Resource)
	selector := utils.ManagedBy + "=" + utils.Manager
	lw := &cache.ListWatch{
		ListWithContextFunc: func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
			options.LabelSelector = selector
			return s.dynClient.Resource(agenticRunGVR).Namespace(s.config.RunNamespace).List(ctx, options)
		},
		WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
			options.LabelSelector = selector
			return s.dynClient.Resource(agenticRunGVR).Namespace(s.config.RunNamespace).Watch(ctx, options)
		},
	}
	informer := cache.NewSharedIndexInformer(
		cache.ToListWatcherWithWatchListSemantics(lw, s.dynClient),
		&unstructured.Unstructured{}, 0, nil,
	)
	_, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj any) { s.enqueueRunOwner(ctx, obj) },
		UpdateFunc: func(_, obj any) { s.enqueueRunOwner(ctx, obj) },
		DeleteFunc: func(obj any) { s.enqueueRunOwner(ctx, obj) },
	})
	if err != nil {
		s.logger.Error(err, "Could not register AgenticRun event handler")
		return
	}
	informer.Run(ctx.Done())
}
