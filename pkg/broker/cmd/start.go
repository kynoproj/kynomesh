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

// Package cmd boots the kynomesh broker: it serves the JSON-RPC, REST,
// and gRPC A2A transports — plus the well-known AgentCard endpoint — over
// a single shared port, demultiplexing protocols at the HTTP handler layer.
// It blocks until the process is signalled to exit.
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
	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"golang.org/x/net/http2"
	"google.golang.org/grpc"

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

	grpcContentType = "application/grpc"
)

// Start boots the broker. All three A2A transports — JSON-RPC, REST, and
// gRPC — share a single TCP port served over TLS with a freshly minted
// self-signed cert. HTTP/2 is negotiated via ALPN, and gRPC requests are
// dispatched to the gRPC server based on Content-Type; HTTP/1.1 JSON-RPC
// and REST share the same handler chain. The function blocks until SIGINT
// or SIGTERM is received, then gracefully shuts the listener down.
//
// advertiseHost is what the published AgentCard tells clients to dial. If
// empty, AdvertiseHostDefault is used.
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

	agentCard := broker.NewAgentCard(
		broker.JSONRPCAddr(advertiseHost, port),
		broker.RESTAddr(advertiseHost, port),
		broker.GRPCAddr(advertiseHost, port),
	)

	requestHandler := a2asrv.NewHandler(
		broker.NewDefaultExecutor(),
		a2asrv.WithExtendedAgentCard(agentCard),
	)

	grpcSrv := grpc.NewServer()
	a2agrpc.NewHandler(requestHandler).RegisterWith(grpcSrv)

	cert, err := sharedtls.GenerateX509KeyPair()
	if err != nil {
		logger.Fatalw("failed to generate broker TLS certificate", "err", err)
	}

	httpSrv, err := newMultiplexedServer(port, requestHandler, agentCard, grpcSrv, cert)
	if err != nil {
		logger.Fatalw("failed to build broker server", "err", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		logger.Infow("starting A2A transports on shared port",
			"port", port,
			"jsonrpcPath", broker.JSONRPCEndpoint,
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
	grpcSrv.GracefulStop()

	if err := <-serveErr; err != nil {
		logger.Fatalw("broker exited with error", "err", err)
	}
	logger.Infow("broker stopped cleanly")
}

// newMultiplexedServer builds the single *http.Server that fronts all
// three A2A transports over TLS. HTTP/2 is negotiated via ALPN ("h2"),
// which lets gRPC ride the same listener as HTTP/1.1 JSON-RPC and REST.
// The handler dispatches by Content-Type so the gRPC server only sees
// gRPC frames.
func newMultiplexedServer(
	port int,
	h a2asrv.RequestHandler,
	card *a2a.AgentCard,
	grpcSrv *grpc.Server,
	cert *tls.Certificate,
) (*http.Server, error) {
	httpMux := http.NewServeMux()
	httpMux.Handle(broker.JSONRPCEndpoint, a2asrv.NewJSONRPCHandler(h))
	httpMux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(card))
	// REST mounts under RESTEndpoint ("/api/"). The a2a-go handler serves
	// absolute paths like /v2/..., so we strip the prefix before dispatch.
	// The trailing slash on the pattern makes http.ServeMux treat it as a
	// subtree root.
	httpMux.Handle(broker.RESTEndpoint+"/", http.StripPrefix(broker.RESTEndpoint, a2asrv.NewRESTHandler(h)))

	dispatch := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isGRPCRequest(r) {
			grpcSrv.ServeHTTP(w, r)
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
