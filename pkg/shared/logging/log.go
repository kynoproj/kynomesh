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

package logging

import (
	"context"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger constants
const (
	InfoLevel  = "info"
	DebugLevel = "debug"
	ErrorLevel = "error"
)

// Env var names read by WithAgentLabels. Kept private and duplicated from
// pkg/apis/kynomesh/v1alpha1 to avoid logging depending on the API package.
const (
	envNamespace       = "NAMESPACE"
	envAgentSetName    = "KYNOMESH_AGENTSET_NAME"
	envAgentDeployName = "KYNOMESH_AGENTDEPLOY_NAME"
)

// NewLogger returns a new zap.SugaredLogger
func NewLogger() *zap.SugaredLogger {
	logLevel, _ := os.LookupEnv("LOG_LEVEL")
	config := configureLogLevelLogger(logLevel)
	config.EncoderConfig.EncodeTime = zapcore.RFC3339NanoTimeEncoder
	config.OutputPaths = []string{"stdout"}
	logger, err := config.Build()
	if err != nil {
		panic(err)
	}
	return logger.Named("kynomesh").Sugar()
}

var defaultLogger = NewLogger()

type loggerKey struct{}

// WithLogger returns a copy of parent context in which the
// value associated with logger key is the supplied logger.
func WithLogger(ctx context.Context, logger *zap.SugaredLogger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// FromContext returns the logger in the context.
func FromContext(ctx context.Context) *zap.SugaredLogger {
	if logger, ok := ctx.Value(loggerKey{}).(*zap.SugaredLogger); ok {
		return logger
	}
	return defaultLogger
}

// WithAgentLabels binds NAMESPACE, AGENTSET_NAME, and AGENTDEPLOY_NAME from
// the pod environment as persistent log fields so every log line emitted by
// the returned logger carries them. Missing env vars are skipped; if none are
// set, the input logger is returned unchanged.
func WithAgentLabels(logger *zap.SugaredLogger) *zap.SugaredLogger {
	labels := []any{}
	for _, kv := range []struct{ key, env string }{
		{"namespace", envNamespace},
		{"agentSet", envAgentSetName},
		{"agentDeploy", envAgentDeployName},
	} {
		if v := os.Getenv(kv.env); v != "" {
			labels = append(labels, zap.String(kv.key, v))
		}
	}
	if len(labels) == 0 {
		return logger
	}
	return logger.With(labels...)
}

// Returns logger config depending on the log level
func configureLogLevelLogger(logLevel string) zap.Config {
	logConfig := zap.NewProductionConfig()
	switch logLevel {
	case InfoLevel:
		logConfig.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	case ErrorLevel:
		logConfig.Level = zap.NewAtomicLevelAt(zap.ErrorLevel)
	case DebugLevel:
		logConfig.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
	default:
		logConfig.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	}
	return logConfig
}
