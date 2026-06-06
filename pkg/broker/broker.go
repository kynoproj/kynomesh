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

// Package broker hosts the kynomesh A2A broker service. The broker
// terminates the A2A protocol on a TLS-fronted port (JSON-RPC, REST, and
// gRPC sharing the same listener) and forwards every request to the
// user's agent container running in the same pod. AgentCard requests are
// proxied per-request from the agent, with the SupportedInterfaces URLs
// rewritten to point at the broker's external endpoint.
package broker

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

const JSONRPCEndpoint = "/a2a/jsonrpc"
const RESTEndpoint = "/a2a/rest"

// JSONRPCAddr / RESTAddr / GRPCAddr format the URL each transport
// advertises on its AgentInterface entry. The broker terminates TLS on
// a shared port; JSON-RPC and REST are https, gRPC is host:port.
func JSONRPCAddr(host string, port int) string {
	return fmt.Sprintf("https://%s:%d%s", host, port, JSONRPCEndpoint)
}

func RESTAddr(host string, port int) string {
	return fmt.Sprintf("https://%s:%d%s", host, port, RESTEndpoint)
}

func GRPCAddr(host string, port int) string {
	return fmt.Sprintf("%s:%d", host, port)
}

// AdvertisedURL returns the URL the broker should publish for transport
// on its AgentCard. When publicBaseURL is non-empty it overrides the
// in-cluster host:port — JSON-RPC and REST append the well-known
// endpoint path, gRPC reduces it to a scheme-less host:port. Otherwise
// the JSONRPCAddr/RESTAddr/GRPCAddr helpers are used as before.
func AdvertisedURL(publicBaseURL, host string, port int, transport a2a.TransportProtocol) string {
	if publicBaseURL == "" {
		switch transport {
		case a2a.TransportProtocolJSONRPC:
			return JSONRPCAddr(host, port)
		case a2a.TransportProtocolHTTPJSON:
			return RESTAddr(host, port)
		case a2a.TransportProtocolGRPC:
			return GRPCAddr(host, port)
		}
		return ""
	}
	trimmed := strings.TrimRight(publicBaseURL, "/")
	switch transport {
	case a2a.TransportProtocolJSONRPC:
		return trimmed + JSONRPCEndpoint
	case a2a.TransportProtocolHTTPJSON:
		return trimmed + RESTEndpoint
	case a2a.TransportProtocolGRPC:
		return grpcAddrFromBaseURL(trimmed)
	}
	return ""
}

// grpcAddrFromBaseURL converts a public base URL into the host:port form
// gRPC clients expect. Falls back to returning the input unchanged when
// it can't be parsed — preserving operator intent over guessing.
func grpcAddrFromBaseURL(baseURL string) string {
	if !strings.Contains(baseURL, "://") {
		return baseURL
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return baseURL
	}
	if _, _, splitErr := net.SplitHostPort(u.Host); splitErr == nil {
		return u.Host
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return u.Host + ":443"
	case "http":
		return u.Host + ":80"
	}
	return u.Host
}
