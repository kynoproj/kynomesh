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

	zap "go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger constants
const (
	InfoLevel  = "info"
	DebugLevel = "debug"
	ErrorLevel = "error"
)

// NewLogger returns a new zap.SugaredLogger
func NewLogger() *zap.SugaredLogger {
	var config zap.Config
	logLevel, _ := os.LookupEnv("LOG_LEVEL")
	config = configureLogLevelLogger(logLevel)
	// Config customization goes here if any
	config.EncoderConfig.EncodeTime = zapcore.RFC3339NanoTimeEncoder
	config.OutputPaths = []string{"stdout"}
	logger, err := config.Build()
	if err != nil {
		panic(err)
	}
	return logger.Named("kyno-mesh").Sugar()
}

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
	return NewLogger()
}

// Returns logger conifg depending on the log level
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
