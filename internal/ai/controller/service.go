package controller

import (
	"context"
	"crypto/tls"
	"net/http"
	"os"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// New creates a TriggerService ready to start.
func New(cfg ServiceConfig, kube kubernetes.Interface, dyn dynamic.Interface, logger logr.Logger) *TriggerService {
	return &TriggerService{
		kubeClient:   kube,
		dynClient:    dyn,
		config:       cfg,
		logger:       logger,
		queue:        newDiagnosisQueue(),
		graceStarted: map[string]graceEntry{},
	}
}

func (s *TriggerService) graceClock(key, revision string, eligibilityWait time.Duration) time.Duration {
	if s.graceStarted == nil {
		s.graceStarted = map[string]graceEntry{}
	}
	if e, ok := s.graceStarted[key]; ok && e.revision == revision {
		if remaining := s.config.AutoDiagnosisGracePeriod - s.currentTime().Sub(e.start); remaining > 0 {
			return remaining
		}
		return 0
	}
	if eligibilityWait > 0 {
		// Back-calculate when grace started from Eligibility's remaining wait.
		s.graceStarted[key] = graceEntry{
			start:    s.currentTime().Add(eligibilityWait - s.config.AutoDiagnosisGracePeriod),
			revision: revision,
		}
	}
	return eligibilityWait
}

func (s *TriggerService) clearGrace(key string) {
	if s.graceStarted != nil {
		delete(s.graceStarted, key)
	}
}

// Run starts the HTTP server, worker queue, and AgenticRun watcher.
// It blocks until ctx is cancelled.
func (s *TriggerService) Run(ctx context.Context, addr, tlsCert, tlsKey string, tlsEnabled bool) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/diagnose", s.HandleDiagnose)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Bound how long an unauthenticated client can hold a connection while
	// sending its request; TokenReview only runs once the request is read.
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
	}
	if tlsEnabled {
		server.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		s.runWorker(ctx)
	}()
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		s.runAgenticRunEvents(ctx)
	}()

	go func() {
		if tlsEnabled {
			s.logger.Info("starting gitops-ai-controller service with TLS", "addr", addr)
			if err := server.ListenAndServeTLS(tlsCert, tlsKey); err != nil && err != http.ErrServerClosed {
				s.logger.Error(err, "server error")
				os.Exit(1)
			}
		} else {
			s.logger.Info("starting gitops-ai-controller service without TLS (plain HTTP)", "addr", addr)
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				s.logger.Error(err, "server error")
				os.Exit(1)
			}
		}
	}()

	<-ctx.Done()
	s.logger.Info("shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
	<-watcherDone
	<-workerDone
}
