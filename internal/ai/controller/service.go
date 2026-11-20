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
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
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

func (s *TriggerService) graceClock(key, appUID, revision string, eligibilityWait time.Duration) time.Duration {
	if s.graceStarted == nil {
		s.graceStarted = map[string]graceEntry{}
	}
	if e, ok := s.graceStarted[key]; ok && e.appUID == appUID && e.revision == revision {
		if remaining := s.config.AutoDiagnosisGracePeriod - s.currentTime().Sub(e.start); remaining > 0 {
			return remaining
		}
		return 0
	}
	// Discard the old object's timer even if the replacement already qualifies.
	s.clearHealthTimers(key)
	if eligibilityWait > 0 {
		// Back-calculate when grace started from Eligibility's remaining wait.
		s.graceStarted[key] = graceEntry{
			start:    s.currentTime().Add(eligibilityWait - s.config.AutoDiagnosisGracePeriod),
			appUID:   appUID,
			revision: revision,
		}
	}
	return eligibilityWait
}

func (s *TriggerService) clearHealthTimers(key string) {
	delete(s.graceStarted, key)
	delete(s.recoveryStarted, key)
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
	certificateWatcherDone := make(chan struct{})
	if tlsEnabled {
		certificateWatcher, err := certwatcher.New(tlsCert, tlsKey)
		if err != nil {
			s.logger.Error(err, "unable to load serving certificate")
			os.Exit(1)
		}
		// Serving certificates rotate in the mounted Secret without restarting
		// the Pod. Keep the last valid pair while the replacement is being
		// written, then use the new pair for subsequent TLS handshakes.
		server.TLSConfig = &tls.Config{
			MinVersion:     tls.VersionTLS12,
			GetCertificate: certificateWatcher.GetCertificate,
		}
		go func() {
			defer close(certificateWatcherDone)
			if err := certificateWatcher.Start(ctx); err != nil && ctx.Err() == nil {
				s.logger.Error(err, "unable to watch serving certificate")
				os.Exit(1)
			}
		}()
	} else {
		close(certificateWatcherDone)
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
			if err := server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
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
	<-certificateWatcherDone
	<-watcherDone
	<-workerDone
}
