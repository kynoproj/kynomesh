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

// AgentBackendHost is the synthetic Host header value the broker uses
// when dialling the agent over a Unix Domain Socket. UDS has no real
// host; net/http still requires a non-empty URL.Host for outgoing
// requests, so we plant a fixed token the agent will never act on.
const AgentBackendHost = "kynomesh-agent"

// NewUDSHTTPTransport returns an *http.Transport whose connections are
// dialled to the given Unix Domain Socket path instead of TCP. The
// transport ignores the request URL's host/port — every request, no
// matter what URL it carries, ends up at socketPath.
//
// This is the building block for both the AgentCard fetch path (used by
// agentcard.Resolver) and the HTTP reverse proxies that forward
// JSON-RPC and REST.
func NewUDSHTTPTransport(socketPath string) *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		},
		// MaxIdleConnsPerHost on a UDS isn't a real concern since there's
		// one server, but lifting the default (2) avoids serialising
		// bursts of small requests.
		MaxIdleConnsPerHost: 16,
	}
}

// NewUDSHTTPClient wraps NewUDSHTTPTransport in an *http.Client. The
// caller is expected to use any URL it wants (typically "http://kynomesh-agent/...");
// the transport rewires the actual dial to the socket regardless.
func NewUDSHTTPClient(socketPath string) *http.Client {
	return &http.Client{Transport: NewUDSHTTPTransport(socketPath)}
}

// NewTCPHTTPTransport returns an *http.Transport that pins every dial to
// tcpAddr (host:port) regardless of the request URL's host. It mirrors
// NewUDSHTTPTransport's contract: callers continue to use URLs with the
// synthetic AgentBackendHost; the transport intercepts the dial. This
// lets the broker run outside Kubernetes (no UDS available) by pointing
// at a TCP-listening agent on the developer's machine.
func NewTCPHTTPTransport(tcpAddr string) *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp", tcpAddr)
		},
		MaxIdleConnsPerHost: 16,
	}
}

// NewTCPHTTPClient is the TCP counterpart to NewUDSHTTPClient.
func NewTCPHTTPClient(tcpAddr string) *http.Client {
	return &http.Client{Transport: NewTCPHTTPTransport(tcpAddr)}
}
