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
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kynoproj/kynomesh/pkg/broker"
	sharedtls "github.com/kynoproj/kynomesh/pkg/shared/tls"
)

// buildLoopStack constructs a minimal brokerStack with both http.Servers
// bound to ":0" (the OS picks ephemeral ports) and a passthrough-only
// runtime. The loop goroutines can ListenAndServeTLS against these
// without privileged ports or upstream agent dependencies.
func buildLoopStack(t *testing.T) *brokerStack {
	t.Helper()
	cert, err := sharedtls.GenerateX509KeyPair()
	require.NoError(t, err)

	rt := &brokerRuntime{
		counters:    &broker.Counters{},
		enabled:     map[a2a.TransportProtocol]bool{},
		httpProxies: map[a2a.TransportProtocol]http.Handler{},
		passthrough: stubOKHandler("ok"),
	}

	mainSrv, err := newMultiplexedServer(0, rt, nil, cert)
	require.NoError(t, err)
	mainSrv.Addr = "127.0.0.1:0"

	introSrv := newIntrospectionServer(0, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), cert)
	introSrv.Addr = "127.0.0.1:0"

	return &brokerStack{rt: rt, proxySrv: mainSrv, introspectionSrv: introSrv}
}

// TestRunServeLoop_CleanShutdownOnContextCancel verifies the happy path:
// when the supplied ctx is cancelled, both listeners are gracefully
// shut down and runServeLoop returns nil.
func TestRunServeLoop_CleanShutdownOnContextCancel(t *testing.T) {
	stack := buildLoopStack(t)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runServeLoop(ctx, stack, 0, 0)
	}()

	// Give the goroutines a beat to actually call ListenAndServeTLS so
	// the select inside the loop is waiting on a real signal, not the
	// initial "haven't started yet" race window.
	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err, "ctx cancel should produce a clean shutdown")
	case <-time.After(5 * time.Second):
		t.Fatal("runServeLoop did not return after ctx cancel")
	}
}

// TestRunServeLoop_MainListenerError surfaces the error from the main
// HTTP listener when it fails to start. We force the failure by
// pointing the server at an invalid bind address that net.Listen
// rejects synchronously.
func TestRunServeLoop_MainListenerError(t *testing.T) {
	stack := buildLoopStack(t)
	stack.proxySrv.Addr = "not-a-valid-address"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- runServeLoop(ctx, stack, 0, 0)
	}()

	select {
	case err := <-done:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "broker server")
	case <-time.After(5 * time.Second):
		t.Fatal("runServeLoop did not return on listener error")
	}
}

// TestRunServeLoop_IntrospectionListenerError mirrors the main-listener
// test but corrupts the introspection address. Either listener
// surfacing first is acceptable — the contract is just that the error
// propagates.
func TestRunServeLoop_IntrospectionListenerError(t *testing.T) {
	stack := buildLoopStack(t)
	stack.introspectionSrv.Addr = "not-a-valid-address"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- runServeLoop(ctx, stack, 0, 0)
	}()

	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("runServeLoop did not return on listener error")
	}
}

// TestRunServeLoop_NilGRPCConnNoOps confirms the deferred close path is
// safe when no gRPC transport was enabled (the common non-gRPC node
// case). The grpcConn is nil; the close goroutine must not panic.
func TestRunServeLoop_NilGRPCConnNoOps(t *testing.T) {
	stack := buildLoopStack(t)
	stack.rt.grpcConn = nil // explicit: no gRPC transport

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runServeLoop(ctx, stack, 0, 0)
	}()

	time.Sleep(150 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("runServeLoop did not return")
	}
}
