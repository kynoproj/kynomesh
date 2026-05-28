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
// agent supports, and brings up the matching pass-through proxies on a
// single TLS-fronted port. The broker itself is protocol-agnostic at
// the message layer — JSON-RPC and REST traffic is forwarded by
// httputil.ReverseProxy, and gRPC traffic by a hand-rolled
// UnknownServiceHandler that shuttles raw frames. It blocks until the
// process is signalled to exit.
package cmd

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
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

	// AgentEndpointDefault is the URL the broker dials to reach the
	// user's agent container. The agent runs in the same pod, so
	// localhost is the natural default. Override via
	// KYNOMESH_AGENT_ENDPOINT for testing or unusual topologies.
	AgentEndpointDefault = "http://localhost:8000"

	shutdownTimeout = 10 * time.Second

	// agentProbeTimeout caps the startup AgentCard fetch. The broker
	// refuses to come up if the agent isn't reachable within this window
	// — the alternative is to start healthy and 502 every request, which
	// hides the misconfiguration from CrashLoopBackoff and the operator.
	agentProbeTimeout = 5 * time.Second

	grpcContentType = "application/grpc"
)

// brokerRuntime aggregates the pieces of broker state that live for the
// process's whole lifetime — handed around between Start and its helpers
// to keep their signatures compact.
type brokerRuntime struct {
	logger       *zap.SugaredLogger
	counters     *broker.Counters
	enabled      map[a2a.TransportProtocol]bool
	httpProxies  map[a2a.TransportProtocol]http.Handler
	grpcServer   *grpc.Server
	grpcConn     *grpc.ClientConn // nil if gRPC isn't enabled
}

// Start boots the broker. It reads the user agent's AgentCard, brings up
// only the transports the agent advertises (each via its own dumb
// proxy), and serves them all on one TLS port. The function blocks
// until SIGINT or SIGTERM is received, then gracefully shuts down.
//
// advertiseHost is what the published AgentCard tells external clients
// to dial. If empty, AdvertiseHostDefault is used.
func Start(port int, advertiseHost string) {
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

	agentEndpoint := os.Getenv(kmv1.EnvAgentEndpoint)
	if agentEndpoint == "" {
		agentEndpoint = AgentEndpointDefault
	}

	// Startup probe: fail fast if the user's agent isn't reachable.
	// Also tells us which transports it speaks so we can mount the
	// right proxies — sidecar mode (where the agent is brought up
	// after the broker) is a future concern.
	probeCtx, probeCancel := context.WithTimeout(context.Background(), agentProbeTimeout)
	agentCard, err := agentcard.NewResolver(http.DefaultClient).Resolve(probeCtx, agentEndpoint)
	probeCancel()
	if err != nil {
		logger.Fatalw("failed to fetch AgentCard from agent endpoint — refusing to start",
			"agentEndpoint", agentEndpoint, "err", err)
	}
	logger.Infow("agent endpoint reachable", "agentEndpoint", agentEndpoint, "agentName", agentCard.Name)

	rt, err := buildRuntime(logger, agentEndpoint, agentCard)
	if err != nil {
		logger.Fatalw("failed to build broker runtime", "err", err)
	}
	if len(rt.enabled) == 0 {
		logger.Fatalw("agent AgentCard advertised no transports the broker can proxy — refusing to start",
			"agentEndpoint", agentEndpoint)
	}
	defer func() {
		if rt.grpcConn != nil {
			_ = rt.grpcConn.Close()
		}
	}()

	cardProxy := broker.NewAgentCardProxy(agentEndpoint, advertiseHost, port, rt.enabled)

	cert, err := sharedtls.GenerateX509KeyPair()
	if err != nil {
		logger.Fatalw("failed to generate broker TLS certificate", "err", err)
	}

	httpSrv, err := newMultiplexedServer(port, rt, cardProxy, cert)
	if err != nil {
		logger.Fatalw("failed to build broker server", "err", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		logger.Infow("starting broker on shared port",
			"port", port,
			"enabledTransports", enabledTransportNames(rt.enabled),
			"tls", true,
		)
		// Empty cert/key paths → use httpSrv.TLSConfig.Certificates.
		if err := httpSrv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- fmt.Errorf("broker server: %w", err)
			return
		}
		serveErr <- nil
	}()

	select {
	case <-ctx.Done():
		logger.Infow("broker received shutdown signal, stopping transports")
	case err := <-serveErr:
		if err != nil {
			logger.Fatalw("broker exited with error", "err", err)
		}
		logger.Infow("broker stopped cleanly")
		return
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Warnw("broker server shutdown error", "err", err)
	}
	if rt.grpcServer != nil {
		rt.grpcServer.GracefulStop()
	}

	if err := <-serveErr; err != nil {
		logger.Fatalw("broker exited with error", "err", err)
	}
	logger.Infow("broker stopped cleanly")
}

// buildRuntime inspects the agent's card and provisions the per-transport
// proxies that match the advertised SupportedInterfaces. For HTTP-based
// transports (JSON-RPC, REST) the broker builds a httputil.ReverseProxy
// pointing at the agent's HTTP base URL. For gRPC it dials a long-lived
// *grpc.ClientConn and installs an UnknownServiceHandler that forwards
// every method to that conn. Unknown ProtocolBindings are skipped.
func buildRuntime(logger *zap.SugaredLogger, agentEndpoint string, card *a2a.AgentCard) (*brokerRuntime, error) {
	agentURL, err := url.Parse(agentEndpoint)
	if err != nil {
		return nil, fmt.Errorf("parse agent endpoint %q: %w", agentEndpoint, err)
	}

	rt := &brokerRuntime{
		logger:      logger,
		counters:    &broker.Counters{},
		enabled:     map[a2a.TransportProtocol]bool{},
		httpProxies: map[a2a.TransportProtocol]http.Handler{},
	}

	for _, iface := range card.SupportedInterfaces {
		switch iface.ProtocolBinding {
		case a2a.TransportProtocolJSONRPC:
			rt.httpProxies[iface.ProtocolBinding] = broker.NewJSONRPCReverseProxy(agentURL, rt.counters)
			rt.enabled[iface.ProtocolBinding] = true
		case a2a.TransportProtocolHTTPJSON:
			rt.httpProxies[iface.ProtocolBinding] = broker.NewRESTReverseProxy(agentURL, rt.counters)
			rt.enabled[iface.ProtocolBinding] = true
		case a2a.TransportProtocolGRPC:
			conn, err := dialAgentGRPC(iface.URL)
			if err != nil {
				return nil, fmt.Errorf("dial agent gRPC at %q: %w", iface.URL, err)
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

// dialAgentGRPC opens an insecure connection to the agent's gRPC
// endpoint. The agent shares the broker's pod, so plaintext is fine —
// TLS terminates externally at the broker, and same-pod traffic is
// trusted by definition.
func dialAgentGRPC(grpcURL string) (*grpc.ClientConn, error) {
	// gRPC target syntax accepts bare host:port — strip any scheme the
	// AgentCard happened to include (the spec doesn't standardise one).
	target := strings.TrimPrefix(strings.TrimPrefix(grpcURL, "grpc://"), "http://")
	target = strings.TrimPrefix(target, "https://")
	return grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
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
