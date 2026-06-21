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

// Package cmd boots the per-AgentSet metrics daemon.
package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	"github.com/kynoproj/kynomesh/pkg/daemon/server/discovery"
	"github.com/kynoproj/kynomesh/pkg/daemon/server/rater"
	"github.com/kynoproj/kynomesh/pkg/daemon/server/scraper"
	"github.com/kynoproj/kynomesh/pkg/daemon/server/service"
	"github.com/kynoproj/kynomesh/pkg/shared/logging"
	sharedtls "github.com/kynoproj/kynomesh/pkg/shared/tls"
	"github.com/kynoproj/kynomesh/pkg/version"
)

const shutdownTimeout = 10 * time.Second

// daemonConfig is the resolved, validated boot configuration. It is
// derived from environment variables set by the controller when it
// provisions the daemon Deployment.
type daemonConfig struct {
	Namespace    string
	AgentSet     string
	AgentDeploys []string
	APIPort      int
	MetricsPort  int
}

// loadConfig reads env vars and validates them.
func loadConfig(apiPort, metricsPort int) (*daemonConfig, error) {
	namespace := os.Getenv(kmv1.EnvNamespace)
	if namespace == "" {
		return nil, fmt.Errorf("env var %s is required", kmv1.EnvNamespace)
	}
	agentSet := os.Getenv(kmv1.EnvAgentSetName)
	if agentSet == "" {
		return nil, fmt.Errorf("env var %s is required", kmv1.EnvAgentSetName)
	}
	raw := os.Getenv(kmv1.EnvAgentSetAgentDeploys)
	if raw == "" {
		return nil, fmt.Errorf("env var %s is required", kmv1.EnvAgentSetAgentDeploys)
	}
	var ads []string
	if err := json.Unmarshal([]byte(raw), &ads); err != nil {
		return nil, fmt.Errorf("parse %s as JSON string array: %w", kmv1.EnvAgentSetAgentDeploys, err)
	}
	if len(ads) == 0 {
		return nil, fmt.Errorf("env var %s must contain at least one AgentDeploy name", kmv1.EnvAgentSetAgentDeploys)
	}
	return &daemonConfig{
		Namespace:    namespace,
		AgentSet:     agentSet,
		AgentDeploys: ads,
		APIPort:      apiPort,
		MetricsPort:  metricsPort,
	}, nil
}

// Start boots the daemon. Returns only on fatal errors or after a
// clean shutdown via SIGINT/SIGTERM.
func Start(apiPort, metricsPort int) {
	logger := logging.WithAgentLabels(logging.NewLogger().Named("daemon"))
	v := version.GetVersion()
	logger.Infow("Starting kynomesh daemon",
		zap.String("version", v.Version),
		zap.String("buildDate", v.BuildDate),
		zap.String("gitCommit", v.GitCommit),
		zap.String("gitTreeState", v.GitTreeState),
		zap.String("goVersion", v.GoVersion),
		zap.String("platform", v.Platform))

	cfg, err := loadConfig(apiPort, metricsPort)
	if err != nil {
		logger.Fatalw("Invalid daemon configuration", zap.Error(err))
	}
	logger.Infow("Daemon configured",
		zap.String("namespace", cfg.Namespace),
		zap.String("agentSet", cfg.AgentSet),
		zap.Strings("agentDeploys", cfg.AgentDeploys),
		zap.Int("apiPort", cfg.APIPort),
		zap.Int("metricsPort", cfg.MetricsPort))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx = logging.WithLogger(ctx, logger)

	if err := run(ctx, cfg, logger); err != nil {
		logger.Fatalw("Daemon exited with error", zap.Error(err))
	}
	logger.Infow("Daemon stopped cleanly")
}

func run(ctx context.Context, cfg *daemonConfig, logger *zap.SugaredLogger) error {
	registry := prometheus.NewRegistry()
	selfMetrics := rater.NewSelfMetrics(registry)

	scr := scraper.New(rater.DefaultScrapeTimeout)
	discoverFn := func(ctx context.Context, ad string) ([]string, error) {
		return discovery.Discover(ctx, net.DefaultResolver, ad, cfg.Namespace)
	}

	r := rater.NewRater(rater.Options{
		AgentSet:     cfg.AgentSet,
		AgentDeploys: cfg.AgentDeploys,
		Namespace:    cfg.Namespace,
		Scraper:      scr,
		Discover:     discoverFn,
		Logger:       logger.Named("rater"),
	}).WithSelfMetrics(selfMetrics)

	cert, err := sharedtls.GenerateX509KeyPair()
	if err != nil {
		return fmt.Errorf("generate TLS certificate: %w", err)
	}

	svc := service.NewService(r)
	srv, err := service.New(ctx, service.Config{
		APIPort:     cfg.APIPort,
		MetricsPort: cfg.MetricsPort,
		Cert:        cert,
		Service:     svc,
		Registry:    registry,
		Logger:      logger.Named("server"),
	})
	if err != nil {
		return fmt.Errorf("build server: %w", err)
	}

	// Run rater + both servers concurrently. Any one returning an
	// error triggers shutdown of the others.
	raterDone := make(chan struct{})
	go func() {
		r.Start(ctx)
		close(raterDone)
	}()

	apiDone := make(chan error, 1)
	metricsDone := make(chan error, 1)
	go func() {
		logger.Infow("Starting daemon API listener",
			zap.Int("port", cfg.APIPort), zap.Bool("tls", true))
		if err := srv.APIServer.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			apiDone <- fmt.Errorf("api server: %w", err)
			return
		}
		apiDone <- nil
	}()
	go func() {
		logger.Infow("Starting daemon metrics listener",
			zap.Int("port", cfg.MetricsPort), zap.Bool("tls", true))
		if err := srv.MetricsServer.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			metricsDone <- fmt.Errorf("metrics server: %w", err)
			return
		}
		metricsDone <- nil
	}()

	select {
	case <-ctx.Done():
		logger.Infow("Received shutdown signal")
	case err := <-apiDone:
		return err
	case err := <-metricsDone:
		return err
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	srv.GracefulStop(shutCtx)
	<-raterDone

	if err := <-apiDone; err != nil {
		return err
	}
	if err := <-metricsDone; err != nil {
		return err
	}
	return nil
}
