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
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestNewLogger(t *testing.T) {
	t.Setenv("LOG_LEVEL", "")
	logger := NewLogger()
	if logger == nil {
		t.Fatal("NewLogger() returned nil")
	}
}

func TestWithLogger(t *testing.T) {
	ctx := context.Background()
	logger := NewLogger()
	ctx = WithLogger(ctx, logger)

	got := FromContext(ctx)
	if got != logger {
		t.Error("FromContext should return the logger stored by WithLogger")
	}
}

func TestFromContext_EmptyContext(t *testing.T) {
	ctx := context.Background()
	got := FromContext(ctx)
	if got == nil {
		t.Fatal("FromContext() on empty context should return a new logger, not nil")
	}
	// Should be a valid logger that can be used
	got.Info("test message")
	_ = got.Sync()
}

func TestFromContext_InvalidValueInContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), loggerKey{}, "not-a-logger")
	got := FromContext(ctx)
	if got == nil {
		t.Fatal("FromContext() with wrong type in context should return a new logger, not nil")
	}
}

func TestConfigureLogLevelLogger(t *testing.T) {
	tests := []struct {
		name     string
		logLevel string
		want     zapcore.Level
	}{
		{"info", InfoLevel, zap.InfoLevel},
		{"debug", DebugLevel, zap.DebugLevel},
		{"error", ErrorLevel, zap.ErrorLevel},
		{"empty default", "", zap.InfoLevel},
		{"unknown default", "warn", zap.InfoLevel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := configureLogLevelLogger(tt.logLevel)
			if cfg.Level.Level() != tt.want {
				t.Errorf("configureLogLevelLogger(%q).Level = %v, want %v",
					tt.logLevel, cfg.Level.Level(), tt.want)
			}
		})
	}
}
