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
	"os"
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

// probeAgentCard fetches the AgentCard over UDS. It returns a nil card
// with a nil error when the agent responds 404 to the well-known path —
// the agent is reachable but exposes no A2A surface (e.g., an A2A-client
// node that serves only its own non-A2A REST APIs). Every other error
// (dial failure, timeout, 5xx, malformed JSON) is propagated so the
// liveness gate at startup still rejects a dead or broken agent.
func probeAgentCard(ctx context.Context, client *http.Client, baseURL string) (*a2a.AgentCard, error) {
	card, err := agentcard.NewResolver(client).Resolve(ctx, baseURL)
	if err == nil {
		return card, nil
	}
	var statusErr *agentcard.ErrStatusNotOK
	if errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	return nil, err
}

const (
	shutdownTimeout = 10 * time.Second

	// agentProbeTimeout caps the startup AgentCard fetch.
	agentProbeTimeout = 5 * time.Second

	grpcContentType = "application/grpc"
)

// For testing overwrite.
var agentSocketPath = kmv1.BrokerSocketPath

// clusterDNSDomain is the suffix appended to in-cluster headless-service
// records. Every standard Kubernetes install uses "cluster.local"; the
// few outliers that don't (custom kubelet --cluster-domain) will need to
// fall back to the explicit advertiseHost flag.
const clusterDNSDomain = "cluster.local"

// loadInjectedAgentDeploy reads the AgentDeploy blob the reconciler
// stamps onto the broker container via EnvAgentDeployObject and decodes
// it. Returns nil with a nil error when the env var is unset — that's
// the local-dev path. Returns an error when the blob is configured but
// malformed, so misconfiguration fails visibly rather than silently
// degrading to the fallback.
func loadInjectedAgentDeploy() (*kmv1.AgentDeploy, error) {
	encoded := os.Getenv(kmv1.EnvAgentDeployObject)
	if encoded == "" {
		return nil, nil
	}
	return kmv1.DecodeAgentDeploy(encoded)
}

// advertiseHostFor returns the broker's in-cluster FQDN for the given
// AgentDeploy. The host is the headless Service DNS —
// "<svc>.<ns>.svc.<domain>" — so a single AgentCard URL resolves to
// every replica behind the service. Clients reconnect by picking any A
// record the resolver returns; the broker doesn't claim a specific
// pod's identity. Returns "" when ad is nil so the caller can choose
// the fallback.
func advertiseHostFor(ad *kmv1.AgentDeploy) string {
	if ad == nil {
		return ""
	}
	return fmt.Sprintf("%s.%s.svc.%s",
		ad.HeadlessServiceName(), ad.Namespace, clusterDNSDomain)
}

// brokerRuntime aggregates the pieces of broker state that live for the
// process's whole lifetime.
type brokerRuntime struct {
	logger   *zap.SugaredLogger
	counters *broker.Counters
	// agentDeploy is the AgentDeploy this broker was provisioned to
	// front, decoded once at startup from EnvAgentDeployObject. nil
	// when the broker is run outside a reconciler-managed pod (local
	// dev, manual invocations). Downstream code that needs the
	// AgentDeploy's identity (namespace, name, AgentSet membership,
	// container spec, etc.) should read it from here rather than
	// re-decoding the env var.
	agentDeploy *kmv1.AgentDeploy
	enabled     map[a2a.TransportProtocol]bool
	httpProxies map[a2a.TransportProtocol]http.Handler
	// passthrough is the catch-all HTTP reverse proxy used for any
	// non-canonical route.
	passthrough http.Handler
	grpcServer  *grpc.Server
	grpcConn    *grpc.ClientConn // nil if gRPC isn't enabled
}

// brokerStack groups the constructed-but-not-yet-serving pieces of the
// broker.
type brokerStack struct {
	rt               *brokerRuntime
	proxySrv         *http.Server
	introspectionSrv *http.Server
}

// assembleBroker performs every step required to bring the broker up to
// the point of serving traffic: probe the agent over UDS, decide which
// transports to enable, and wire both the multiplexed traffic server and
// the introspection server.
func assembleBroker(logger *zap.SugaredLogger, port, introspectionPort int, advertiseHost string) (*brokerStack, error) {
	injectedAD, err := loadInjectedAgentDeploy()
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", kmv1.EnvAgentDeployObject, err)
	}

	if advertiseHost == "" {
		advertiseHost = advertiseHostFor(injectedAD)
		if advertiseHost == "" {
			return nil, fmt.Errorf("no advertise host: %s not injected and no override supplied", kmv1.EnvAgentDeployObject)
		}
		logger.Infow("Derived broker advertise host from injected AgentDeploy",
			"advertiseHost", advertiseHost)
	}

	agentHTTPClient := broker.NewUDSHTTPClient(agentSocketPath)
	agentHTTPTransport := agentHTTPClient.Transport.(*http.Transport)

	probeCtx, probeCancel := context.WithTimeout(context.Background(), agentProbeTimeout)
	agentCard, err := probeAgentCard(probeCtx, agentHTTPClient, "http://"+broker.AgentBackendHost)
	probeCancel()
	if err != nil {
		return nil, fmt.Errorf("fetch AgentCard over UDS at %q: %w", agentSocketPath, err)
	}
	if agentCard == nil {
		logger.Infow("Agent reachable over UDS but exposes no AgentCard — running passthrough-only",
			"socket", agentSocketPath)
	} else {
		logger.Infow("Agent reachable over UDS",
			"socket", agentSocketPath, "agentName", agentCard.Name)
	}

	metricsRegistry := prometheus.NewRegistry()
	rt, err := buildRuntime(logger, metricsRegistry, agentHTTPTransport, agentCard, injectedAD)
	if err != nil {
		return nil, fmt.Errorf("build broker runtime: %w", err)
	}

	var cardProxy http.Handler
	if agentCard != nil {
		cardProxy = broker.NewAgentCardProxy(agentHTTPClient, advertiseHost, port, rt.enabled)
	}

	cert, err := sharedtls.GenerateX509KeyPair()
	if err != nil {
		return nil, fmt.Errorf("generate broker TLS certificate: %w", err)
	}

	proxySrv, err := newMultiplexedServer(port, rt, cardProxy, cert)
	if err != nil {
		return nil, fmt.Errorf("build broker server: %w", err)
	}

	ready := func() error { return nil }
	introspectionHandler := broker.NewIntrospectionHandler(metricsRegistry, ready)
	introspectionSrv := newIntrospectionServer(introspectionPort, introspectionHandler, cert)

	return &brokerStack{
		rt:               rt,
		proxySrv:         proxySrv,
		introspectionSrv: introspectionSrv,
	}, nil
}

// Start boots the broker. The advertised AgentCard host is derived from
// the AgentDeploy blob the reconciler injects via EnvAgentDeployObject;
// callers that need to override (tests, manual local runs) can call
// assembleBroker directly with an explicit advertiseHost.
func Start(port, introspectionPort int) {
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

	stack, err := assembleBroker(logger, port, introspectionPort, "")
	if err != nil {
		logger.Fatalw("Failed to assemble broker — refusing to start", "err", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := runServeLoop(ctx, logger, stack, port, introspectionPort); err != nil {
		logger.Fatalw("Broker exited with error", "err", err)
	}
	logger.Infow("Broker stopped cleanly")
}

// runServeLoop owns the two TLS listeners' lifetimes. It returns nil on
// a clean shutdown (either ctx cancelled or a listener returned
// http.ErrServerClosed) and a wrapped error if a listener failed to
// start or exited abnormally.
func runServeLoop(ctx context.Context, logger *zap.SugaredLogger, stack *brokerStack, port, introspectionPort int) error {
	rt, proxySrv, introspectionSrv := stack.rt, stack.proxySrv, stack.introspectionSrv
	defer func() {
		if rt.grpcConn != nil {
			_ = rt.grpcConn.Close()
		}
	}()

	mainServeErr := make(chan error, 1)
	go func() {
		logger.Infow("Starting broker on shared port",
			"port", port,
			"enabledTransports", enabledTransportNames(rt.enabled),
			"tls", true,
		)
		if err := proxySrv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			mainServeErr <- fmt.Errorf("broker server: %w", err)
			return
		}
		mainServeErr <- nil
	}()

	introspectionServeErr := make(chan error, 1)
	go func() {
		logger.Infow("Starting broker introspection listener",
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
		logger.Infow("Broker received shutdown signal, stopping transports")
	case err := <-mainServeErr:
		return err
	case err := <-introspectionServeErr:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := proxySrv.Shutdown(shutdownCtx); err != nil {
		logger.Warnw("Broker server shutdown error", "err", err)
	}
	if err := introspectionSrv.Shutdown(shutdownCtx); err != nil {
		logger.Warnw("Broker introspection shutdown error", "err", err)
	}
	if rt.grpcServer != nil {
		rt.grpcServer.GracefulStop()
	}

	if err := <-mainServeErr; err != nil {
		return err
	}
	if err := <-introspectionServeErr; err != nil {
		return err
	}
	return nil
}

// newIntrospectionServer builds a minimal *http.Server for the broker's
// secondary listener.
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
//   - A nil card means the agent exposes no A2A surface (e.g., an
//     A2A-client node serving only non-A2A REST). The runtime keeps the
//     catch-all passthrough so the agent's own HTTP surface remains
//     reachable, and registers no per-transport A2A proxies.
func buildRuntime(logger *zap.SugaredLogger, registry *prometheus.Registry, udsTransport *http.Transport, card *a2a.AgentCard, agentDeploy *kmv1.AgentDeploy) (*brokerRuntime, error) {
	counters := broker.NewCounters(registry)
	rt := &brokerRuntime{
		logger:      logger,
		counters:    counters,
		agentDeploy: agentDeploy,
		enabled:     map[a2a.TransportProtocol]bool{},
		httpProxies: map[a2a.TransportProtocol]http.Handler{},
		passthrough: broker.NewPassthroughReverseProxy(udsTransport, counters),
	}

	if card == nil {
		return rt, nil
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
			conn, err := dialAgentGRPC(agentSocketPath)
			if err != nil {
				return nil, fmt.Errorf("dial agent gRPC over UDS at %q: %w", agentSocketPath, err)
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

// dialAgentGRPC is the seam used by buildRuntime to open the broker's
// long-lived gRPC connection to the user agent. For testing overwrite
// so that it does not depend on kmv1.BrokerSocketPath being present on disk.
var dialAgentGRPC = dialAgentGRPCOverUDS

// dialAgentGRPCOverUDS opens a connection to the agent's gRPC
// server over the shared Unix Domain Socket.
func dialAgentGRPCOverUDS(socketPath string) (*grpc.ClientConn, error) {
	return grpc.NewClient("unix://"+socketPath, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

// newMultiplexedServer builds the single *http.Server that fronts every
// enabled transport over TLS. HTTP/2 is negotiated via ALPN ("h2"),
// which lets gRPC share the listener with HTTP/1.1 JSON-RPC and REST.
func newMultiplexedServer(
	port int,
	rt *brokerRuntime,
	cardHandler http.Handler,
	cert *tls.Certificate,
) (*http.Server, error) {
	httpMux := http.NewServeMux()
	if cardHandler != nil {
		httpMux.Handle(a2asrv.WellKnownAgentCardPath, cardHandler)
	}
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
