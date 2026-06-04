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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
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

func TestWithAgentLabels_StampsAllPresentEnv(t *testing.T) {
	t.Setenv(kmv1.EnvNamespace, "ns-1")
	t.Setenv(kmv1.EnvAgentSetName, "set-1")
	t.Setenv(kmv1.EnvAgentDeployName, "deploy-1")

	core, recorded := observer.New(zapcore.InfoLevel)
	logger := WithAgentLabels(zap.New(core).Sugar())
	logger.Infow("hello")

	require.Equal(t, 1, recorded.Len())
	got := recorded.All()[0].ContextMap()
	assert.Equal(t, "ns-1", got["namespace"])
	assert.Equal(t, "set-1", got["agentSet"])
	assert.Equal(t, "deploy-1", got["agentDeploy"])
}

func TestWithAgentLabels_SkipsUnsetEnv(t *testing.T) {
	t.Setenv(kmv1.EnvNamespace, "ns-1")
	t.Setenv(kmv1.EnvAgentSetName, "")
	t.Setenv(kmv1.EnvAgentDeployName, "")

	core, recorded := observer.New(zapcore.InfoLevel)
	logger := WithAgentLabels(zap.New(core).Sugar())
	logger.Infow("hello")

	require.Equal(t, 1, recorded.Len())
	got := recorded.All()[0].ContextMap()
	assert.Equal(t, "ns-1", got["namespace"])
	_, hasSet := got["agentSet"]
	_, hasDeploy := got["agentDeploy"]
	assert.False(t, hasSet, "agentSet must not be stamped when env is empty")
	assert.False(t, hasDeploy, "agentDeploy must not be stamped when env is empty")
}

func TestWithAgentLabels_NoEnvReturnsSameLogger(t *testing.T) {
	t.Setenv(kmv1.EnvNamespace, "")
	t.Setenv(kmv1.EnvAgentSetName, "")
	t.Setenv(kmv1.EnvAgentDeployName, "")

	base := zap.NewNop().Sugar()
	got := WithAgentLabels(base)
	assert.Same(t, base, got, "no env labels — must return the input logger unchanged")
}

// TestWithAgentLabels_EnvNamesMatchAPIPackage guards against drift between
// the private env-name consts in this package and the canonical ones in
// pkg/apis/kynomesh/v1alpha1. Logging deliberately doesn't import v1alpha1,
// so the test catches a rename of either side.
func TestWithAgentLabels_EnvNamesMatchAPIPackage(t *testing.T) {
	assert.Equal(t, kmv1.EnvNamespace, envNamespace)
	assert.Equal(t, kmv1.EnvAgentSetName, envAgentSetName)
	assert.Equal(t, kmv1.EnvAgentDeployName, envAgentDeployName)
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
