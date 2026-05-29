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

// Package cmd boots the kynomesh broker. The broker reads the user
// agent's AgentCard at startup, determines which A2A transports the
// agent supports, and brings up the matching pass-through proxies.
package cmd

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"golang.org/x/net/http2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	"github.com/kynoproj/kynomesh/pkg/broker"
	"github.com/kynoproj/kynomesh/pkg/shared/logging"
	sharedtls "github.com/kynoproj/kynomesh/pkg/shared/tls"
	"github.com/kynoproj/kynomesh/pkg/version"
)

const (
	// AdvertiseHostDefault is the host the AgentCard advertises when the
	// operator does not provide an externally reachable address. It is
	// deliberately localhost — production deployments should set
	// KYNOMESH_BROKER_ADVERTISE_HOST to the Service / ingress hostname.
	AdvertiseHostDefault = "127.0.0.1"

	shutdownTimeout = 10 * time.Second

	// agentProbeTimeout caps the startup AgentCard fetch.
	agentProbeTimeout = 5 * time.Second

	grpcContentType = "application/grpc"
)

// brokerRuntime aggregates the pieces of broker state that live for the
// process's whole lifetime.
type brokerRuntime struct {
	logger      *zap.SugaredLogger
	counters    *broker.Counters
	enabled     map[a2a.TransportProtocol]bool
	httpProxies map[a2a.TransportProtocol]http.Handler
	// passthrough is the catch-all HTTP reverse proxy used for any
	// non-canonical route.
	passthrough http.Handler
	grpcServer  *grpc.Server
	grpcConn    *grpc.ClientConn // nil if gRPC isn't enabled
}

// Start boots the broker.
func Start(port, introspectionPort int, advertiseHost string) {
	logger := logging.NewLogger().Named("broker")

	v := version.GetVersion()
	logger.Infow("Starting kynomesh broker",
		"version", v.Version,
		"buildDate", v.BuildDate,
		"gitCommit", v.GitCommit,
		"gitTreeState", v.GitTreeState,
		"goVersion", v.GoVersion,
		"platform", v.Platform,
	)

	if advertiseHost == "" {
		advertiseHost = AdvertiseHostDefault
	}

	agentHTTPClient := broker.NewUDSHTTPClient(kmv1.BrokerSocketPath)
	agentHTTPTransport := agentHTTPClient.Transport.(*http.Transport)

	probeCtx, probeCancel := context.WithTimeout(context.Background(), agentProbeTimeout)
	agentCard, err := agentcard.NewResolver(agentHTTPClient).Resolve(probeCtx, "http://"+broker.AgentBackendHost)
	probeCancel()
	if err != nil {
		logger.Fatalw("failed to fetch AgentCard over UDS — refusing to start",
			"socket", kmv1.BrokerSocketPath, "err", err)
	}
	logger.Infow("agent reachable over UDS",
		"socket", kmv1.BrokerSocketPath, "agentName", agentCard.Name)

	// Single registry per broker process. Counters register their
	// gauge on it; the introspection handler scrapes from the same
	// registry. Using a private registry (not the global) keeps test
	// runs isolated and avoids inheriting unrelated Go-runtime
	// metrics we don't want exposed here.
	metricsRegistry := prometheus.NewRegistry()
	rt, err := buildRuntime(logger, metricsRegistry, agentHTTPTransport, agentCard)
	if err != nil {
		logger.Fatalw("failed to build broker runtime", "err", err)
	}
	if len(rt.enabled) == 0 {
		logger.Fatalw("agent AgentCard advertised no transports the broker can proxy — refusing to start",
			"socket", kmv1.BrokerSocketPath)
	}
	defer func() {
		if rt.grpcConn != nil {
			_ = rt.grpcConn.Close()
		}
	}()

	cardProxy := broker.NewAgentCardProxy(agentHTTPClient, advertiseHost, port, rt.enabled)

	cert, err := sharedtls.GenerateX509KeyPair()
	if err != nil {
		logger.Fatalw("failed to generate broker TLS certificate", "err", err)
	}

	httpSrv, err := newMultiplexedServer(port, rt, cardProxy, cert)
	if err != nil {
		logger.Fatalw("failed to build broker server", "err", err)
	}

	// Introspection listener — separate TLS port, same cert. Carries
	// /metrics, /healthz, /readyz; never user traffic. Ready is true
	// once the AgentCard probe succeeded and the runtime is wired,
	// which is the point we've reached here, so the gate is a closure
	// over rt that returns nil on every call until process exit.
	ready := func() error { return nil }
	introspectionHandler := broker.NewIntrospectionHandler(metricsRegistry, ready)
	introspectionSrv := newIntrospectionServer(introspectionPort, introspectionHandler, cert)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	mainServeErr := make(chan error, 1)
	go func() {
		logger.Infow("starting broker on shared port",
			"port", port,
			"enabledTransports", enabledTransportNames(rt.enabled),
			"tls", true,
		)
		// Empty cert/key paths → use httpSrv.TLSConfig.Certificates.
		if err := httpSrv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			mainServeErr <- fmt.Errorf("broker server: %w", err)
			return
		}
		mainServeErr <- nil
	}()

	introspectionServeErr := make(chan error, 1)
	go func() {
		logger.Infow("starting broker introspection listener",
			"port", introspectionPort, "tls", true)
		if err := introspectionSrv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			introspectionServeErr <- fmt.Errorf("broker introspection server: %w", err)
			return
		}
		introspectionServeErr <- nil
	}()

	// Wait for either a shutdown signal or a fatal server exit on
	// either listener. Once one fires, gracefully shut both down.
	select {
	case <-ctx.Done():
		logger.Infow("broker received shutdown signal, stopping transports")
	case err := <-mainServeErr:
		if err != nil {
			logger.Fatalw("broker exited with error", "err", err)
		}
		logger.Infow("broker stopped cleanly")
		return
	case err := <-introspectionServeErr:
		if err != nil {
			logger.Fatalw("broker introspection exited with error", "err", err)
		}
		logger.Infow("broker introspection stopped cleanly")
		return
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Warnw("broker server shutdown error", "err", err)
	}
	if err := introspectionSrv.Shutdown(shutdownCtx); err != nil {
		logger.Warnw("broker introspection shutdown error", "err", err)
	}
	if rt.grpcServer != nil {
		rt.grpcServer.GracefulStop()
	}

	if err := <-mainServeErr; err != nil {
		logger.Fatalw("broker exited with error", "err", err)
	}
	if err := <-introspectionServeErr; err != nil {
		logger.Fatalw("broker introspection exited with error", "err", err)
	}
	logger.Infow("broker stopped cleanly")
}

// newIntrospectionServer builds a minimal *http.Server for the broker's
// secondary listener: same cert as the main port, no HTTP/2 magic, no
// gRPC dispatch. /metrics, /healthz, /readyz only.
func newIntrospectionServer(port int, handler http.Handler, cert *tls.Certificate) *http.Server {
	return &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{*cert},
			MinVersion:   tls.VersionTLS12,
		},
	}
}

// buildRuntime inspects the agent's card and provisions the per-transport
// proxies that match the advertised SupportedInterfaces.
//
//   - JSON-RPC and REST share udsTransport, forwarded by an
//     httputil.ReverseProxy that targets a synthetic
//     http://kynomesh-agent host.
//   - gRPC opens a long-lived *grpc.ClientConn against
//     "unix://<BrokerSocketPath>" and installs an UnknownServiceHandler
//     that forwards every method to that conn.
//   - Unknown ProtocolBindings are skipped with a warning.
func buildRuntime(logger *zap.SugaredLogger, registry *prometheus.Registry, udsTransport *http.Transport, card *a2a.AgentCard) (*brokerRuntime, error) {
	counters := broker.NewCounters(registry)
	rt := &brokerRuntime{
		logger:      logger,
		counters:    counters,
		enabled:     map[a2a.TransportProtocol]bool{},
		httpProxies: map[a2a.TransportProtocol]http.Handler{},
		passthrough: broker.NewPassthroughReverseProxy(udsTransport, counters),
	}

	for _, iface := range card.SupportedInterfaces {
		switch iface.ProtocolBinding {
		case a2a.TransportProtocolJSONRPC:
			rt.httpProxies[iface.ProtocolBinding] = broker.NewJSONRPCReverseProxy(udsTransport, rt.counters)
			rt.enabled[iface.ProtocolBinding] = true
		case a2a.TransportProtocolHTTPJSON:
			rt.httpProxies[iface.ProtocolBinding] = broker.NewRESTReverseProxy(udsTransport, rt.counters)
			rt.enabled[iface.ProtocolBinding] = true
		case a2a.TransportProtocolGRPC:
			conn, err := dialAgentGRPCOverUDS(kmv1.BrokerSocketPath)
			if err != nil {
				return nil, fmt.Errorf("dial agent gRPC over UDS at %q: %w", kmv1.BrokerSocketPath, err)
			}
			rt.grpcConn = conn
			rt.grpcServer = grpc.NewServer(broker.GRPCPassthroughOptions(conn, rt.counters)...)
			rt.enabled[iface.ProtocolBinding] = true
		default:
			logger.Warnw("ignoring AgentCard interface with unsupported ProtocolBinding",
				"protocolBinding", iface.ProtocolBinding, "url", iface.URL)
		}
	}
	return rt, nil
}

// dialAgentGRPCOverUDS opens a connection to the agent's gRPC
// server over the shared Unix Domain Socket. gRPC-Go natively resolves
// "unix:///path/to/sock" targets.
func dialAgentGRPCOverUDS(socketPath string) (*grpc.ClientConn, error) {
	return grpc.NewClient("unix://"+socketPath, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

// newMultiplexedServer builds the single *http.Server that fronts every
// enabled transport over TLS. HTTP/2 is negotiated via ALPN ("h2"),
// which lets gRPC share the listener with HTTP/1.1 JSON-RPC and REST.
// The handler dispatches by Content-Type so the gRPC server only sees
// gRPC frames.
func newMultiplexedServer(
	port int,
	rt *brokerRuntime,
	cardHandler http.Handler,
	cert *tls.Certificate,
) (*http.Server, error) {
	httpMux := http.NewServeMux()
	httpMux.Handle(a2asrv.WellKnownAgentCardPath, cardHandler)
	if h, ok := rt.httpProxies[a2a.TransportProtocolJSONRPC]; ok {
		httpMux.Handle(broker.JSONRPCEndpoint, h)
	}
	if h, ok := rt.httpProxies[a2a.TransportProtocolHTTPJSON]; ok {
		// REST mounts under RESTEndpoint ("/api/"). The trailing slash
		// on the pattern makes http.ServeMux treat it as a subtree
		// root. The downstream agent serves the same path layout, so
		// no prefix stripping is needed.
		httpMux.Handle(broker.RESTEndpoint+"/", h)
	}
	// Catch-all: any HTTP request not matching a more-specific A2A
	// route falls through to the agent's HTTP surface.
	if rt.passthrough != nil {
		httpMux.Handle("/", rt.passthrough)
	}

	dispatch := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isGRPCRequest(r) && rt.grpcServer != nil {
			rt.grpcServer.ServeHTTP(w, r)
			return
		}
		httpMux.ServeHTTP(w, r)
	})

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           dispatch,
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{*cert},
			MinVersion:   tls.VersionTLS12,
			// ALPN advertises h2 first so gRPC clients land on HTTP/2;
			// http/1.1 stays for the JSON-RPC and REST transports.
			NextProtos: []string{"h2", "http/1.1"},
		},
	}
	// Wire the http2 server into srv so ServeTLS will dispatch H2 frames
	// to net/http's HTTP/2 stack — which then calls our Handler with
	// r.ProtoMajor == 2 for gRPC traffic.
	if err := http2.ConfigureServer(srv, &http2.Server{}); err != nil {
		return nil, fmt.Errorf("configure http/2: %w", err)
	}
	return srv, nil
}

// isGRPCRequest reports whether r looks like a gRPC call. gRPC speaks
// HTTP/2 with a Content-Type of "application/grpc" (optionally followed
// by a subtype such as "+proto"); anything else is REST or JSON-RPC.
func isGRPCRequest(r *http.Request) bool {
	if r.ProtoMajor != 2 {
		return false
	}
	return strings.HasPrefix(r.Header.Get("Content-Type"), grpcContentType)
}

// enabledTransportNames returns a stable string slice for logging.
func enabledTransportNames(enabled map[a2a.TransportProtocol]bool) []string {
	out := make([]string, 0, len(enabled))
	for k := range enabled {
		out = append(out, string(k))
	}
	return out
}
