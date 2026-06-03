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

package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

const (
	ModeGRPC = "grpc"
	ModeHTTP = "http"
)

// Config captures every input the probe needs.
type Config struct {
	Mode     string
	Socket   string
	Service  string
	HTTPPath string
	Timeout  time.Duration
}

// Run dispatches to the configured probe mode.
func Run(ctx context.Context, cfg Config) error {
	if cfg.Socket == "" {
		return fmt.Errorf("--socket is required")
	}
	switch cfg.Mode {
	case ModeGRPC:
		return probeGRPC(ctx, cfg.Socket, cfg.Service)
	case ModeHTTP:
		return probeHTTP(ctx, cfg.Socket, cfg.HTTPPath)
	default:
		return fmt.Errorf("unknown --mode %q (want %q or %q)", cfg.Mode, ModeGRPC, ModeHTTP)
	}
}

func probeGRPC(ctx context.Context, socket, service string) error {
	conn, err := grpc.NewClient(
		"unix://"+socket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("grpc dial %q: %w", socket, err)
	}
	defer func() { _ = conn.Close() }()

	resp, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{Service: service})
	if err != nil {
		return fmt.Errorf("grpc health check: %w", err)
	}
	if resp.Status != healthpb.HealthCheckResponse_SERVING {
		return fmt.Errorf("grpc health status: %s", resp.Status)
	}
	return nil
}

func probeHTTP(ctx context.Context, socket, path string) error {
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socket)
			},
		},
	}
	url := "http://kynomesh-agent" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build http request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http get %q over %q: %w", path, socket, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http status %d", resp.StatusCode)
	}
	return nil
}
