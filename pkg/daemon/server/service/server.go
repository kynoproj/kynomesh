/*
Copyright 2026 The Kynoproj Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package server hosts the daemon's gRPC + REST API on a single TLS
// port (ALPN-dispatch like the broker) and its own /metrics surface
// on a separate TLS port.
package service

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"golang.org/x/net/http2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/kynoproj/kynomesh/pkg/apis/proto/daemon"
)

const (
	// readHeaderTimeout caps the time clients have to send headers,
	// matching the broker.
	readHeaderTimeout = 10 * time.Second
)

// Servers bundles the two HTTP servers the daemon runs so callers
// can shut both down together.
type Servers struct {
	APIServer     *http.Server
	MetricsServer *http.Server

	grpcServer *grpc.Server
}

// Config carries the inputs needed to build the daemon's servers.
type Config struct {
	APIPort     int
	MetricsPort int
	Cert        *tls.Certificate
	Service     pb.DaemonServiceServer
	Registry    *prometheus.Registry
	Logger      *zap.SugaredLogger
}

// New constructs the daemon's two servers but does not start them.
// Callers invoke ListenAndServeTLS on each, typically in goroutines.
func New(ctx context.Context, cfg Config) (*Servers, error) {
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop().Sugar()
	}

	apiSrv, grpcSrv, err := newAPIServer(ctx, cfg)
	if err != nil {
		return nil, err
	}
	metricsSrv := newMetricsServer(cfg)

	return &Servers{
		APIServer:     apiSrv,
		MetricsServer: metricsSrv,
		grpcServer:    grpcSrv,
	}, nil
}

// GracefulStop stops the gRPC server cleanly and shuts down both
// HTTP servers within the deadline implied by ctx.
func (s *Servers) GracefulStop(ctx context.Context) {
	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
	}
	if s.APIServer != nil {
		_ = s.APIServer.Shutdown(ctx)
	}
	if s.MetricsServer != nil {
		_ = s.MetricsServer.Shutdown(ctx)
	}
}

// newAPIServer builds the multiplexed TLS server on cfg.APIPort.
// HTTP/2 is negotiated via ALPN so gRPC shares the listener with
// HTTP/1.1 REST traffic. This mirrors pkg/broker/cmd/start.go.
func newAPIServer(ctx context.Context, cfg Config) (*http.Server, *grpc.Server, error) {
	grpcServer := grpc.NewServer()
	pb.RegisterDaemonServiceServer(grpcServer, cfg.Service)

	// Mount the REST gateway on the HTTP side, dialing the gRPC
	// server in-process via a loopback connection. The gateway calls
	// go through the gRPC server's normal handlers so interceptors
	// and stats stay unified across REST + gRPC. The loopback dials
	// back through the API server's TLS listener with skip-verify —
	// the self-signed cert isn't trust-anchored and the loopback
	// can't be observed off-host.
	gwMux := runtime.NewServeMux()
	loopbackCreds := credentials.NewTLS(&tls.Config{
		InsecureSkipVerify: true, //nolint:gosec
		MinVersion:         tls.VersionTLS12,
	})
	loopback, err := grpc.NewClient(
		fmt.Sprintf("127.0.0.1:%d", cfg.APIPort),
		grpc.WithTransportCredentials(loopbackCreds),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("loopback gRPC client: %w", err)
	}
	if err := pb.RegisterDaemonServiceHandler(ctx, gwMux, loopback); err != nil {
		return nil, nil, fmt.Errorf("register gateway: %w", err)
	}

	httpMux := http.NewServeMux()
	httpMux.Handle("/api/", gwMux)

	dispatch := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isGRPCRequest(r) {
			grpcServer.ServeHTTP(w, r)
			return
		}
		httpMux.ServeHTTP(w, r)
	})

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.APIPort),
		Handler:           dispatch,
		ReadHeaderTimeout: readHeaderTimeout,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{*cfg.Cert},
			MinVersion:   tls.VersionTLS12,
			// ALPN advertises h2 first so gRPC lands on HTTP/2.
			NextProtos: []string{"h2", "http/1.1"},
		},
	}
	if err := http2.ConfigureServer(srv, &http2.Server{}); err != nil {
		return nil, nil, fmt.Errorf("configure http/2: %w", err)
	}
	return srv, grpcServer, nil
}

// newMetricsServer builds the TLS /metrics + /healthz server on
// cfg.MetricsPort.
func newMetricsServer(cfg Config) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(cfg.Registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
	return &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.MetricsPort),
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{*cfg.Cert},
			MinVersion:   tls.VersionTLS12,
		},
	}
}

// isGRPCRequest matches the broker's dispatch heuristic: HTTP/2 +
// gRPC content-type.
func isGRPCRequest(r *http.Request) bool {
	if r.ProtoMajor != 2 {
		return false
	}
	return strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc")
}
