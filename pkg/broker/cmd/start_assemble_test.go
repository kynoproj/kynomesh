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

package cmd

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "ka-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// startStubAgentUDS serves cardBody on the well-known AgentCard path over a UDS in t.TempDir.
func startStubAgentUDS(t *testing.T, cardBody string, status int) string {
	t.Helper()
	sock := filepath.Join(shortTempDir(t), "agent.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc(a2asrv.WellKnownAgentCardPath, func(w http.ResponseWriter, _ *http.Request) {
		if status != http.StatusOK {
			http.Error(w, http.StatusText(status), status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(cardBody))
	})

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	t.Cleanup(func() {
		_ = srv.Close()
		<-serveErr
	})
	return sock
}

// withAgentSocket overrides agentSocketPath for the duration of the test.
func withAgentSocket(t *testing.T, path string) {
	t.Helper()
	prev := agentSocketPath
	agentSocketPath = path
	t.Cleanup(func() { agentSocketPath = prev })
}

// withInjectedAgentDeploy sets EnvAgentDeployObject to a synthetic AgentDeploy for the test.
func withInjectedAgentDeploy(t *testing.T, namespace, name string) {
	t.Helper()
	ad := &kmv1.AgentDeploy{}
	ad.Namespace = namespace
	ad.Name = name
	t.Setenv(kmv1.EnvAgentDeployObject, kmv1.EncodeAgentDeploy(ad))
}

func TestAssembleBroker_WithAgentCard(t *testing.T) {
	sock := startStubAgentUDS(t, `{"name":"agent-1","supportedInterfaces":[{"protocolBinding":"JSONRPC","url":"http://x/rpc"}]}`, http.StatusOK)
	withAgentSocket(t, sock)
	withInjectedAgentDeploy(t, "demo-ns", "demo-ad")

	stack, err := assembleBroker(zap.NewNop().Sugar(), 18080, 18081)
	require.NoError(t, err)
	require.NotNil(t, stack)
	require.NotNil(t, stack.rt)
	require.NotNil(t, stack.proxySrv)
	require.NotNil(t, stack.introspectionSrv)

	assert.Equal(t, ":18080", stack.proxySrv.Addr)
	assert.Equal(t, ":18081", stack.introspectionSrv.Addr)
	assert.True(t, stack.rt.enabled["JSONRPC"], "JSONRPC transport from card should be enabled")
	require.NotNil(t, stack.rt.agentDeploy, "injected AgentDeploy should be stashed on the runtime")
	assert.Equal(t, "demo-ad", stack.rt.agentDeploy.Name)
}

func TestAssembleBroker_NoAgentCard(t *testing.T) {
	sock := startStubAgentUDS(t, "", http.StatusNotFound)
	withAgentSocket(t, sock)
	withInjectedAgentDeploy(t, "demo-ns", "demo-ad")

	stack, err := assembleBroker(zap.NewNop().Sugar(), 18082, 18083)
	require.NoError(t, err)
	require.NotNil(t, stack)
	assert.Empty(t, stack.rt.enabled, "no card means no A2A transports advertised")
	assert.NotNil(t, stack.rt.passthrough)
}

func TestAssembleBroker_AgentUnreachable(t *testing.T) {
	withAgentSocket(t, filepath.Join(t.TempDir(), "missing.sock"))
	withInjectedAgentDeploy(t, "demo-ns", "demo-ad")

	stack, err := assembleBroker(zap.NewNop().Sugar(), 18084, 18085)
	require.Error(t, err)
	assert.Nil(t, stack)
	assert.Contains(t, err.Error(), "fetch AgentCard over UDS")
}

func TestAssembleBroker_NoInjectedAgentDeploy(t *testing.T) {
	withAgentSocket(t, filepath.Join(t.TempDir(), "missing.sock"))
	t.Setenv(kmv1.EnvAgentDeployObject, "")

	stack, err := assembleBroker(zap.NewNop().Sugar(), 18088, 18089)
	require.Error(t, err)
	assert.Nil(t, stack)
	assert.Contains(t, err.Error(), "no advertise host")
}

func TestAssembleBroker_MalformedAgentDeploy(t *testing.T) {
	withAgentSocket(t, filepath.Join(t.TempDir(), "missing.sock"))
	t.Setenv(kmv1.EnvAgentDeployObject, "not-valid-base64!!!")

	stack, err := assembleBroker(zap.NewNop().Sugar(), 18090, 18091)
	require.Error(t, err)
	assert.Nil(t, stack)
	assert.Contains(t, err.Error(), "decode "+kmv1.EnvAgentDeployObject)
}
