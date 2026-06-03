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

// Command probe is a tiny static health-check binary copied onto the
// shared kynomesh-run tmpfs by the init-runtime container and invoked
// from the agent container's readiness/liveness exec probes.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	cfg := Config{}
	flag.StringVar(&cfg.Mode, "mode", "grpc", "Health protocol: grpc or http")
	flag.StringVar(&cfg.Socket, "socket", "", "Path to the unix domain socket to probe (required)")
	flag.StringVar(&cfg.Service, "service", "", "gRPC service name to check (grpc mode only; empty means overall server)")
	flag.StringVar(&cfg.HTTPPath, "path", "/healthz", "HTTP path to GET (http mode only)")
	flag.DurationVar(&cfg.Timeout, "timeout", 2*time.Second, "Overall probe timeout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	if err := Run(ctx, cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
