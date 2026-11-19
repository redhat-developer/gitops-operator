package controller

import (
	"context"
	"errors"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/workqueue"
)

const maxDiagnosisQueueRetries = 15

func newDiagnosisQueue() workqueue.TypedRateLimitingInterface[string] {
	return workqueue.NewTypedRateLimitingQueueWithConfig(
		workqueue.DefaultTypedControllerRateLimiter[string](),
		workqueue.TypedRateLimitingQueueConfig[string]{Name: "gitops-ai-controller-applications"},
	)
}

func diagnosisQueueKey(namespace, name, instanceUID string) string {
	return namespace + "/" + name + "/" + instanceUID
}

// Keep the verified instance identity in the work item itself. A request that
// finishes after its instance was replaced cannot overwrite newer queued work.
func (s *TriggerService) enqueueApplication(namespace, name, instanceUID string) {
	if s.queue != nil && namespace != "" && name != "" && instanceUID != "" {
		s.queue.Add(diagnosisQueueKey(namespace, name, instanceUID))
	}
}

// The queue is an event hint. Every attempt reads the current Application.
func (s *TriggerService) processNextWorkItem(ctx context.Context) bool {
	key, shutdown := s.queue.Get()
	if shutdown {
		return false
	}
	defer s.queue.Done(key)

	parts := strings.Split(key, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		s.logger.Info("Discarding invalid Application queue key", "key", key)
		s.queue.Forget(key)
		return true
	}
	_, reason, wait, requeue, err := s.reconcileDiagnosis(ctx, parts[0], parts[1], parts[2])
	if err != nil {
		if ctx.Err() != nil {
			s.queue.Forget(key)
			return true
		}
		var terminal *terminalDiagnosisError
		if errors.As(err, &terminal) {
			if !apierrors.IsNotFound(err) {
				s.logger.Error(err, "Application diagnosis cannot proceed until persisted state or ownership changes", "app", key)
			}
			s.queue.Forget(key)
			return true
		}
		if retries := s.queue.NumRequeues(key); retries >= maxDiagnosisQueueRetries {
			s.logger.Error(err, "Dropping Application after maximum workqueue retries", "app", key, "retries", retries)
			s.queue.Forget(key)
			return true
		}
		if apierrors.IsConflict(err) {
			s.logger.V(1).Info("Application update conflicted; requeuing", "app", key, "error", err)
		} else {
			s.logger.Error(err, "Temporary diagnosis reconciliation error; retrying", "app", key, "retry", s.queue.NumRequeues(key)+1)
		}
		s.queue.AddRateLimited(key)
		return true
	}
	s.queue.Forget(key)
	if requeue {
		s.queue.Add(key)
		return true
	}
	if wait > 0 {
		s.logger.V(1).Info("Scheduling Application diagnosis check", "app", key, "after", wait, "reason", reason)
		s.queue.AddAfter(key, wait)
	}
	return true
}

func (s *TriggerService) runWorker(ctx context.Context) {
	go func() {
		<-ctx.Done()
		s.queue.ShutDown()
	}()
	for s.processNextWorkItem(ctx) {
	}
}
