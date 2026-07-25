package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/mykyta-kravchenko98/Kurama/internal/runner"
)

type metricsServer struct {
	server  *http.Server
	address string
	done    <-chan error
}

type readinessChecker interface {
	Ready(context.Context) error
}

const readinessCheckTimeout = 750 * time.Millisecond

func startMetricsServer(
	address string,
	gatherer prometheus.Gatherer,
	readiness readinessChecker,
) (*metricsServer, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("listen on %q: %w", address, err)
	}
	mux := http.NewServeMux()
	mux.Handle(runner.MetricsPath, promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{}))
	mux.HandleFunc(runner.HealthPath, healthy)
	mux.HandleFunc(runner.ReadinessPath, ready(readiness))
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(listener)
	}()
	return &metricsServer{server: server, address: listener.Addr().String(), done: done}, nil
}

func healthy(writer http.ResponseWriter, _ *http.Request) {
	writer.WriteHeader(http.StatusOK)
}

func ready(checker readinessChecker) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		ctx, cancel := context.WithTimeout(request.Context(), readinessCheckTimeout)
		defer cancel()

		if err := checker.Ready(ctx); err != nil {
			http.Error(writer, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusOK)
	}
}

func (s *metricsServer) Shutdown(ctx context.Context) error {
	shutdownErr := s.server.Shutdown(ctx)
	var closeErr error
	if shutdownErr != nil {
		closeErr = s.server.Close()
	}
	serveErr := <-s.done
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	return errors.Join(shutdownErr, closeErr, serveErr)
}
