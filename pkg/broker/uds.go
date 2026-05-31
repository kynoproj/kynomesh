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
	"context"
	"net"
	"net/http"
)

// AgentBackendHost is the synthetic URL.Host the broker uses when
// dialling the agent. net/http requires a non-empty Host; the transport
// rewires the actual dial.
const AgentBackendHost = "kynomesh-agent"

// NewUDSHTTPTransport returns a Transport that pins every dial to socketPath.
func NewUDSHTTPTransport(socketPath string) *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		},
		MaxIdleConnsPerHost: 16,
	}
}

func NewUDSHTTPClient(socketPath string) *http.Client {
	return &http.Client{Transport: NewUDSHTTPTransport(socketPath)}
}

// NewTCPHTTPTransport returns a Transport that pins every dial to tcpAddr.
func NewTCPHTTPTransport(tcpAddr string) *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp", tcpAddr)
		},
		MaxIdleConnsPerHost: 16,
	}
}

func NewTCPHTTPClient(tcpAddr string) *http.Client {
	return &http.Client{Transport: NewTCPHTTPTransport(tcpAddr)}
}
