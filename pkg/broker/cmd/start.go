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
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
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
	"github.com/kynoproj/kynomesh/pkg/broker/serverinfo"
	"github.com/kynoproj/kynomesh/pkg/shared/logging"
	sharedtls "github.com/kynoproj/kynomesh/pkg/shared/tls"
	"github.com/kynoproj/kynomesh/pkg/version"
)

// probeAgentCard fetches the AgentCard, returning (nil, nil) on 404 so
// callers can distinguish "no A2A surface" from "agent unreachable".
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
	// agentProbeTimeout caps the startup AgentCard fetch.
	agentProbeTimeout = 5 * time.Second

	grpcContentType = "application/grpc"

	// AdvertiseHostDefault is the AgentCard host used when the broker
	// runs outside a Kubernetes pod (local dev).
	AdvertiseHostDefault = "127.0.0.1"
)

const clusterDNSDomain = "cluster.local"

// DefaultLocalAgentAddr is the default agent target in local-dev mode.
const DefaultLocalAgentAddr = "127.0.0.1:8088"

// inClusterFn is a test seam; production reads POD_NAME from the downward API.
var inClusterFn = func() bool {
	return os.Getenv(kmv1.EnvPodName) != ""
}

// udsAgentPath is a test seam; production uses kmv1.BrokerSocketPath.
var udsAgentPath = kmv1.BrokerSocketPath

// tcpAgentAddr is a test seam; production uses DefaultLocalAgentAddr.
var tcpAgentAddr = DefaultLocalAgentAddr

// serverInfoFilePath is a test seam; production uses kmv1.ServerInfoFilePath.
var serverInfoFilePath = kmv1.ServerInfoFilePath

// publishAgentServerInfo loads the agent server-info file (in-cluster only),
// logs it, and registers it as a labeled gauge. Missing/unreadable files are
// tolerated — the agent may not have written one yet.
func publishAgentServerInfo(ctx context.Context, registry prometheus.Registerer) {
	if !inClusterFn() {
		return
	}
	logger := logging.FromContext(ctx)
	info, err := serverinfo.Load(serverInfoFilePath)
	if err != nil {
		logger.Warnw("Failed to read agent server-info; skipping",
			zap.String("path", serverInfoFilePath),
			zap.Error(err))
		return
	}
	if info == nil {
		logger.Infow("Agent server-info not present; skipping",
			zap.String("path", serverInfoFilePath))
		return
	}
	logger.Infow("Agent server-info loaded",
		zap.String("protocol", string(info.Protocol)),
		zap.String("language", string(info.Language)),
		zap.String("version", info.Version),
		zap.Any("metadata", info.Metadata))
	serverinfo.RegisterMetric(registry, *info)
}

// agentDial holds either the in-pod UDS path or the local-dev TCP host:port.
type agentDial struct {
	udsPath string
	tcpAddr string
}

func (d agentDial) isUDS() bool { return d.udsPath != "" }
func (d agentDial) target() string {
	if d.isUDS() {
		return d.udsPath
	}
	return d.tcpAddr
}

func resolveAgentDial() agentDial {
	if inClusterFn() {
		return agentDial{udsPath: udsAgentPath}
	}
	return agentDial{tcpAddr: tcpAgentAddr}
}

func newAgentHTTPClient(d agentDial) *http.Client {
	if d.isUDS() {
		return broker.NewUDSHTTPClient(d.udsPath)
	}
	return broker.NewTCPHTTPClient(d.tcpAddr)
}

// resolveAdvertiseHost returns the AgentCard host. In-cluster it must
// come from the injected AgentDeploy; local-dev falls back to the default.
func resolveAdvertiseHost(ctx context.Context, ad *kmv1.AgentDeploy, dial agentDial) (string, error) {
	logger := logging.FromContext(ctx)
	if dial.isUDS() {
		host := advertiseHostFor(ad)
		if host == "" {
			return "", fmt.Errorf("in-cluster broker requires %s; refusing to start", kmv1.EnvAgentDeployObject)
		}
		logger.Infow("Derived broker advertise host from injected AgentDeploy",
			zap.String("advertiseHost", host))
		return host, nil
	}
	logger.Infow("Local-dev mode: using default broker advertise host",
		zap.String("advertiseHost", AdvertiseHostDefault),
		zap.String("agent", dial.tcpAddr))
	return AdvertiseHostDefault, nil
}

// loadInjectedAgentDeploy returns (nil, nil) when the env var is unset.
func loadInjectedAgentDeploy() (*kmv1.AgentDeploy, error) {
	encoded := os.Getenv(kmv1.EnvAgentDeployObject)
	if encoded == "" {
		return nil, nil
	}
	return kmv1.DecodeAgentDeploy(encoded)
}

// advertiseHostFor returns the headless-service FQDN for ad, or "" if ad is nil.
func advertiseHostFor(ad *kmv1.AgentDeploy) string {
	if ad == nil {
		return ""
	}
	return fmt.Sprintf("%s.%s.svc.%s",
		ad.ServiceName(), ad.Namespace, clusterDNSDomain)
}

// brokerRuntime holds process-lifetime broker state.
type brokerRuntime struct {
	logger      *zap.SugaredLogger
	counters    *broker.Metrics
	agentDeploy *kmv1.AgentDeploy
	enabled     map[a2a.TransportProtocol]bool
	httpProxies map[a2a.TransportProtocol]http.Handler
	// passthrough is the catch-all reverse proxy for non-A2A traffic.
	passthrough http.Handler
	grpcServer  *grpc.Server
	grpcConn    *grpc.ClientConn
}

// brokerStack is the constructed-but-not-yet-serving result of assembleBroker.
type brokerStack struct {
	rt               *brokerRuntime
	proxySrv         *http.Server
	proxyLn          net.Listener
	introspectionSrv *http.Server
	// proxyServing is flipped true once the :8490 listener is being served;
	// /readyz reads it so readiness reflects the A2A port, not just the process.
	proxyServing *atomic.Bool
}

func assembleBroker(ctx context.Context, port, introspectionPort int) (*brokerStack, error) {
	logger := logging.FromContext(ctx)
	injectedAD, err := loadInjectedAgentDeploy()
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", kmv1.EnvAgentDeployObject, err)
	}

	dial := resolveAgentDial()
	advertiseHost, err := resolveAdvertiseHost(ctx, injectedAD, dial)
	if err != nil {
		return nil, err
	}

	agentHTTPClient := newAgentHTTPClient(dial)
	agentHTTPTransport := agentHTTPClient.Transport.(*http.Transport)

	probeCtx, probeCancel := context.WithTimeout(context.Background(), agentProbeTimeout)
	agentCard, err := probeAgentCard(probeCtx, agentHTTPClient, "http://"+broker.AgentBackendHost)
	probeCancel()
	if err != nil {
		return nil, fmt.Errorf("fetch AgentCard from agent at %q: %w", dial.target(), err)
	}
	if agentCard == nil {
		logger.Infow("Agent reachable but exposes no AgentCard — running passthrough-only",
			zap.String("agent", dial.target()))
	} else {
		logger.Infow("Agent reachable",
			zap.String("agent", dial.target()),
			zap.String("agentName", agentCard.Name))
	}

	metricsRegistry := prometheus.NewRegistry()
	publishAgentServerInfo(ctx, metricsRegistry)
	rt, err := buildRuntime(ctx, metricsRegistry, agentHTTPTransport, agentCard, injectedAD, dial)
	if err != nil {
		return nil, fmt.Errorf("build broker runtime: %w", err)
	}

	var cardProxy http.Handler
	if agentCard != nil {
		publicBaseURL := ""
		if injectedAD != nil {
			publicBaseURL = injectedAD.Spec.PublicBaseURL
		}
		cardProxy = broker.NewAgentCardProxy(
			agentHTTPClient, publicBaseURL, advertiseHost, port, rt.enabled)
		if publicBaseURL != "" {
			logger.Infow("AgentCard SupportedInterfaces will advertise PublicBaseURL",
				zap.String("publicBaseURL", publicBaseURL))
		}
	}

	cert, err := sharedtls.GenerateX509KeyPair()
	if err != nil {
		return nil, fmt.Errorf("generate broker TLS certificate: %w", err)
	}

	proxySrv, proxyLn, err := newMultiplexedServer(port, rt, cardProxy, cert)
	if err != nil {
		return nil, fmt.Errorf("build broker server: %w", err)
	}

	// proxyServing flips true once the :8490 A2A listener is being served.
	proxyServing := &atomic.Bool{}
	ready := func() error {
		if !proxyServing.Load() {
			return fmt.Errorf("broker A2A listener not accepting yet")
		}
		return nil
	}
	introspectionHandler := broker.NewIntrospectionHandler(ctx, metricsRegistry, ready)
	introspectionSrv := newIntrospectionServer(introspectionPort, introspectionHandler, cert)

	return &brokerStack{
		rt:               rt,
		proxySrv:         proxySrv,
		proxyLn:          proxyLn,
		introspectionSrv: introspectionSrv,
		proxyServing:     proxyServing,
	}, nil
}

// Start boots the broker.
func Start(port, introspectionPort int) {
	logger := logging.WithAgentLabels(logging.NewLogger().Named("broker"))

	v := version.GetVersion()
	logger.Infow("Starting kynomesh broker",
		zap.String("version", v.Version),
		zap.String("buildDate", v.BuildDate),
		zap.String("gitCommit", v.GitCommit),
		zap.String("gitTreeState", v.GitTreeState),
		zap.String("goVersion", v.GoVersion),
		zap.String("platform", v.Platform))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ctx = logging.WithLogger(ctx, logger)
	stack, err := assembleBroker(ctx, port, introspectionPort)
	if err != nil {
		logger.Fatalw("Failed to assemble broker — refusing to start", zap.Error(err))
	}

	if err := runServeLoop(ctx, stack, port, introspectionPort); err != nil {
		logger.Fatalw("Broker exited with error", zap.Error(err))
	}
	logger.Infow("Broker stopped cleanly")
}

// runServeLoop runs both TLS listeners until ctx is cancelled or one exits with an error.
func runServeLoop(ctx context.Context, stack *brokerStack, port, introspectionPort int) error {
	logger := logging.FromContext(ctx)
	rt, proxySrv, introspectionSrv := stack.rt, stack.proxySrv, stack.introspectionSrv
	defer func() {
		if rt.grpcConn != nil {
			_ = rt.grpcConn.Close()
		}
	}()

	mainServeErr := make(chan error, 1)
	go func() {
		logger.Infow("Starting broker on shared port",
			zap.Int("port", port),
			zap.Strings("enabledTransports", enabledTransportNames(rt.enabled)),
			zap.Bool("tls", true))
		// The listener is already bound (newMultiplexedServer); flip readiness now.
		if stack.proxyServing != nil {
			stack.proxyServing.Store(true)
		}
		if err := proxySrv.Serve(stack.proxyLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
			mainServeErr <- fmt.Errorf("broker server: %w", err)
			return
		}
		mainServeErr <- nil
	}()

	introspectionServeErr := make(chan error, 1)
	go func() {
		logger.Infow("Starting broker introspection listener",
			zap.Int("port", introspectionPort),
			zap.Bool("tls", true))
		if err := introspectionSrv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			introspectionServeErr <- fmt.Errorf("broker introspection server: %w", err)
			return
		}
		introspectionServeErr <- nil
	}()

	// Wait for either a shutdown signal or a fatal server exit on
	// either listener. Once one fires, gracefully shut everybody down.
	select {
	case <-ctx.Done():
		logger.Infow("Broker received shutdown signal, stopping transports")
	case err := <-mainServeErr:
		return err
	case err := <-introspectionServeErr:
		return err
	}

	// Shutdown.
	shutdownTimeout := resolveBudgets().Shutdown
	logger.Infow("Broker shutting down", zap.Duration("budget", shutdownTimeout))
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := proxySrv.Shutdown(shutdownCtx); err != nil {
		logger.Warnw("Broker server shutdown error", zap.Error(err))
	}
	if err := introspectionSrv.Shutdown(shutdownCtx); err != nil {
		logger.Warnw("Broker introspection shutdown error", zap.Error(err))
	}
	if rt.grpcServer != nil {
		stopGRPCWithin(rt.grpcServer, shutdownTimeout)
	}

	if err := <-mainServeErr; err != nil {
		return err
	}
	if err := <-introspectionServeErr; err != nil {
		return err
	}
	return nil
}

// stopGRPCWithin drains the gRPC server gracefully but no longer than budget.
func stopGRPCWithin(srv *grpc.Server, budget time.Duration) {
	done := make(chan struct{})
	go func() {
		srv.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(budget):
		srv.Stop()
	}
}

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

// buildRuntime wires per-transport proxies for the agent's advertised interfaces.
// A nil card yields a passthrough-only runtime.
func buildRuntime(ctx context.Context, registry *prometheus.Registry, agentTransport *http.Transport, card *a2a.AgentCard, agentDeploy *kmv1.AgentDeploy, dial agentDial) (*brokerRuntime, error) {
	logger := logging.FromContext(ctx)
	counters := broker.NewMetrics(registry)

	// One limiter shared across all A2A transports: the cap is per-agent, and a
	// single broker serves JSON-RPC, REST and gRPC for that agent. Passthrough
	// (non-A2A) traffic is intentionally not gated.
	maxInFlight := resolveMaxInFlight(agentDeploy)
	limiter := broker.NewLimiter(maxInFlight)
	if maxInFlight > 0 {
		logger.Infow("Broker rate limiting enabled", zap.Int("maxInFlight", maxInFlight))
	}

	rt := &brokerRuntime{
		logger:      logger,
		counters:    counters,
		agentDeploy: agentDeploy,
		enabled:     map[a2a.TransportProtocol]bool{},
		httpProxies: map[a2a.TransportProtocol]http.Handler{},
		passthrough: broker.NewPassthroughReverseProxy(agentTransport, counters),
	}

	if card == nil {
		return rt, nil
	}

	for _, iface := range card.SupportedInterfaces {
		switch iface.ProtocolBinding {
		case a2a.TransportProtocolJSONRPC:
			rt.httpProxies[iface.ProtocolBinding] = broker.NewJSONRPCReverseProxy(agentTransport, rt.counters, limiter)
			rt.enabled[iface.ProtocolBinding] = true
		case a2a.TransportProtocolHTTPJSON:
			rt.httpProxies[iface.ProtocolBinding] = broker.NewRESTReverseProxy(agentTransport, rt.counters, limiter)
			rt.enabled[iface.ProtocolBinding] = true
		case a2a.TransportProtocolGRPC:
			conn, err := dialAgentGRPC(dial)
			if err != nil {
				return nil, fmt.Errorf("dial agent gRPC at %q: %w", dial.target(), err)
			}
			rt.grpcConn = conn
			rt.grpcServer = grpc.NewServer(broker.GRPCPassthroughOptions(conn, rt.counters, limiter)...)
			rt.enabled[iface.ProtocolBinding] = true
		default:
			logger.Warnw("Ignoring AgentCard interface with unsupported ProtocolBinding",
				zap.String("protocolBinding", string(iface.ProtocolBinding)),
				zap.String("url", iface.URL))
		}
	}
	return rt, nil
}

// resolveMaxInFlight reads the per-agent max in-flight cap from the injected
// AgentDeploy spec. Returns 0 (unlimited) when unset — the nil-safe path for
// local-dev where no spec is injected.
func resolveMaxInFlight(ad *kmv1.AgentDeploy) int {
	if ad == nil || ad.Spec.RateLimit == nil || ad.Spec.RateLimit.MaxInFlight == nil {
		return 0
	}
	return int(*ad.Spec.RateLimit.MaxInFlight)
}

// dialAgentGRPC is a test seam.
var dialAgentGRPC = dialAgentGRPCDefault

func dialAgentGRPCDefault(d agentDial) (*grpc.ClientConn, error) {
	if d.isUDS() {
		return grpcNewClient("unix://" + d.udsPath)
	}
	return grpcNewClient(d.tcpAddr)
}

func grpcNewClient(target string) (*grpc.ClientConn, error) {
	return grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

// newMultiplexedServer builds the shared TLS *http.Server and its bound TLS
// listener. HTTP/2 is negotiated via ALPN so gRPC shares the listener with
// HTTP/1.1 traffic.
func newMultiplexedServer(
	port int,
	rt *brokerRuntime,
	cardHandler http.Handler,
	cert *tls.Certificate,
) (*http.Server, net.Listener, error) {
	httpMux := http.NewServeMux()
	if cardHandler != nil {
		httpMux.Handle(a2asrv.WellKnownAgentCardPath, cardHandler)
	}
	if h, ok := rt.httpProxies[a2a.TransportProtocolJSONRPC]; ok {
		httpMux.Handle(broker.JSONRPCEndpoint, h)
	}
	if h, ok := rt.httpProxies[a2a.TransportProtocolHTTPJSON]; ok {
		// Trailing slash makes ServeMux treat this as a subtree root.
		httpMux.Handle(broker.RESTEndpoint+"/", h)
	}
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
			// ALPN advertises h2 first so gRPC lands on HTTP/2.
			NextProtos: []string{"h2", "http/1.1"},
		},
	}
	if err := http2.ConfigureServer(srv, &http2.Server{}); err != nil {
		return nil, nil, fmt.Errorf("configure http/2: %w", err)
	}
	ln, err := tls.Listen("tcp", srv.Addr, srv.TLSConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("broker listen: %w", err)
	}
	return srv, ln, nil
}

// isGRPCRequest reports whether r is an HTTP/2 request with a gRPC Content-Type.
func isGRPCRequest(r *http.Request) bool {
	if r.ProtoMajor != 2 {
		return false
	}
	// HTTP/2 with a Content-Type of "application/grpc" (optionally followed
	// by a subtype such as "+proto").
	return strings.HasPrefix(r.Header.Get("Content-Type"), grpcContentType)
}

func enabledTransportNames(enabled map[a2a.TransportProtocol]bool) []string {
	out := make([]string, 0, len(enabled))
	for k := range enabled {
		out = append(out, string(k))
	}
	return out
}
