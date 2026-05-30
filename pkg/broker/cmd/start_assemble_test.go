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

// startStubAgentUDS brings up a tiny HTTP server bound to a Unix
// Domain Socket in the test's tempdir. It serves cardBody on the
// well-known AgentCard path with the given status. An empty cardBody
// with status 404 simulates the "no AgentCard" path.
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

// withAgentSocket overrides the package-level agentSocketPath for a
// single test so assembleBroker dials the stub UDS instead of the
// production /var/run path.
func withAgentSocket(t *testing.T, path string) {
	t.Helper()
	prev := agentSocketPath
	agentSocketPath = path
	t.Cleanup(func() { agentSocketPath = prev })
}

// withInjectedAgentDeploy stamps a synthetic AgentDeploy onto
// EnvAgentDeployObject so assembleBroker takes its in-cluster derivation
// path instead of erroring out on a missing advertise host.
func withInjectedAgentDeploy(t *testing.T, namespace, name string) {
	t.Helper()
	ad := &kmv1.AgentDeploy{}
	ad.Namespace = namespace
	ad.Name = name
	t.Setenv(kmv1.EnvAgentDeployObject, kmv1.EncodeAgentDeploy(ad))
}

// TestAssembleBroker_WithAgentCard exercises the happy path: a reachable
// agent that advertises one A2A transport. The returned stack should
// have a wired runtime, a multiplexed HTTP server, and an introspection
// server bound to the configured ports. The injected AgentDeploy drives
// advertiseHost derivation and gets stashed on the runtime.
func TestAssembleBroker_WithAgentCard(t *testing.T) {
	sock := startStubAgentUDS(t, `{"name":"agent-1","supportedInterfaces":[{"protocolBinding":"JSONRPC","url":"http://x/rpc"}]}`, http.StatusOK)
	withAgentSocket(t, sock)
	withInjectedAgentDeploy(t, "demo-ns", "demo-ad")

	stack, err := assembleBroker(zap.NewNop().Sugar(), 18080, 18081, "")
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

// TestAssembleBroker_NoAgentCard covers the entry-node-as-A2A-client
// case: the agent responds 404 to the well-known path, and assembly
// still succeeds with a passthrough-only runtime. The explicit
// advertiseHost override stands in for the reconciler-injected env.
func TestAssembleBroker_NoAgentCard(t *testing.T) {
	sock := startStubAgentUDS(t, "", http.StatusNotFound)
	withAgentSocket(t, sock)

	stack, err := assembleBroker(zap.NewNop().Sugar(), 18082, 18083, "broker.example.com")
	require.NoError(t, err)
	require.NotNil(t, stack)
	assert.Empty(t, stack.rt.enabled, "no card means no A2A transports advertised")
	assert.NotNil(t, stack.rt.passthrough)
}

// TestAssembleBroker_AgentUnreachable confirms a dial failure is
// surfaced as an error rather than swallowed: the broker must refuse to
// start when the agent is dead. We do not start a stub server, so the
// socket simply does not exist.
func TestAssembleBroker_AgentUnreachable(t *testing.T) {
	withAgentSocket(t, filepath.Join(t.TempDir(), "missing.sock"))
	withInjectedAgentDeploy(t, "demo-ns", "demo-ad")

	stack, err := assembleBroker(zap.NewNop().Sugar(), 18084, 18085, "")
	require.Error(t, err)
	assert.Nil(t, stack)
	assert.Contains(t, err.Error(), "fetch AgentCard over UDS")
}

// TestAssembleBroker_NoAdvertiseHostFails confirms the new contract:
// without either an explicit advertiseHost override or an injected
// AgentDeploy, assembly must refuse to proceed rather than advertise a
// host the broker cannot stand behind.
func TestAssembleBroker_NoAdvertiseHostFails(t *testing.T) {
	withAgentSocket(t, filepath.Join(t.TempDir(), "missing.sock"))
	t.Setenv(kmv1.EnvAgentDeployObject, "")

	stack, err := assembleBroker(zap.NewNop().Sugar(), 18088, 18089, "")
	require.Error(t, err)
	assert.Nil(t, stack)
	assert.Contains(t, err.Error(), "no advertise host")
}
