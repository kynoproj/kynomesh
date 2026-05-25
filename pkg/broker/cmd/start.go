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
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"

	"github.com/kynoproj/kynomesh/pkg/broker"
	"github.com/kynoproj/kynomesh/pkg/shared/logging"
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
// gRPC — share a single TCP port, multiplexed by an h2c-wrapped HTTP
// handler that dispatches HTTP/2 gRPC traffic to the gRPC server and
// everything else to the HTTP mux. The function blocks until SIGINT or
// SIGTERM is received, then gracefully shuts the listener down.
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

	httpSrv := newMultiplexedServer(port, requestHandler, agentCard, grpcSrv)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		logger.Infow("starting A2A transports on shared port",
			"port", port,
			"jsonrpcPath", broker.JSONRPCEndpoint,
		)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
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

// newMultiplexedServer builds the single *http.Server that fronts all three
// A2A transports. The h2c wrapper lets gRPC (HTTP/2 cleartext) share the
// listener with HTTP/1.1 JSON-RPC and REST traffic; the inner dispatch
// routes by Content-Type so the gRPC server only sees gRPC frames.
func newMultiplexedServer(
	port int,
	h a2asrv.RequestHandler,
	card *a2a.AgentCard,
	grpcSrv *grpc.Server,
) *http.Server {
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

	return &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           h2c.NewHandler(dispatch, &http2.Server{}),
		ReadHeaderTimeout: 10 * time.Second,
	}
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
