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

package broker

import (
	"io"
	"net"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewUDSHTTPClient_PinsDialToSocketRegardlessOfURLHost(t *testing.T) {
	sock := filepath.Join(shortSocketDir(t), "agent.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc("/probe", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("uds-ok"))
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	client := NewUDSHTTPClient(sock)
	resp, err := client.Get("http://" + AgentBackendHost + "/probe")
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "uds-ok", string(body))
}

func TestNewTCPHTTPClient_PinsDialToAddrRegardlessOfURLHost(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc("/probe", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("tcp-ok"))
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	client := NewTCPHTTPClient(ln.Addr().String())
	// The URL host is the synthetic AgentBackendHost; the transport must
	// ignore it and dial ln.Addr instead.
	resp, err := client.Get("http://" + AgentBackendHost + "/probe")
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "tcp-ok", string(body))
}

func TestNewTCPHTTPClient_DialFailureSurfaces(t *testing.T) {
	// 127.0.0.1:1 has nothing listening; the dial must surface as an error.
	client := NewTCPHTTPClient("127.0.0.1:1")
	_, err := client.Get("http://" + AgentBackendHost + "/probe")
	assert.Error(t, err)
}
