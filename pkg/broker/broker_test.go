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
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/assert"
)

func TestAddrHelpers(t *testing.T) {
	assert.Equal(t, "https://broker.local:9100"+JSONRPCEndpoint, JSONRPCAddr("broker.local", 9100))
	assert.Equal(t, "https://broker.local:9101"+RESTEndpoint, RESTAddr("broker.local", 9101))
	assert.Equal(t, "broker.local:9102", GRPCAddr("broker.local", 9102))
}

// AdvertisedURL prefers PublicBaseURL when non-empty and falls back to
// the in-cluster advertiseHost:port otherwise. gRPC drops the URL scheme
// to satisfy the host:port convention.
func TestAdvertisedURL(t *testing.T) {
	cases := []struct {
		name          string
		publicBaseURL string
		host          string
		port          int
		transport     a2a.TransportProtocol
		want          string
	}{
		{
			name:      "no public base — JSON-RPC falls back to in-cluster",
			host:      "broker.local",
			port:      9100,
			transport: a2a.TransportProtocolJSONRPC,
			want:      "https://broker.local:9100" + JSONRPCEndpoint,
		},
		{
			name:      "no public base — REST falls back to in-cluster",
			host:      "broker.local",
			port:      9100,
			transport: a2a.TransportProtocolHTTPJSON,
			want:      "https://broker.local:9100" + RESTEndpoint,
		},
		{
			name:      "no public base — gRPC falls back to host:port",
			host:      "broker.local",
			port:      9102,
			transport: a2a.TransportProtocolGRPC,
			want:      "broker.local:9102",
		},
		{
			name:          "public base — JSON-RPC appends endpoint",
			publicBaseURL: "https://agent.example.com",
			host:          "broker.local",
			port:          9100,
			transport:     a2a.TransportProtocolJSONRPC,
			want:          "https://agent.example.com" + JSONRPCEndpoint,
		},
		{
			name:          "public base — REST appends endpoint",
			publicBaseURL: "https://agent.example.com",
			host:          "broker.local",
			port:          9100,
			transport:     a2a.TransportProtocolHTTPJSON,
			want:          "https://agent.example.com" + RESTEndpoint,
		},
		{
			name:          "public base — trailing slash is collapsed",
			publicBaseURL: "https://agent.example.com/",
			host:          "broker.local",
			port:          9100,
			transport:     a2a.TransportProtocolJSONRPC,
			want:          "https://agent.example.com" + JSONRPCEndpoint,
		},
		{
			name:          "public base — gRPC strips https scheme and uses 443",
			publicBaseURL: "https://agent.example.com",
			host:          "broker.local",
			port:          9102,
			transport:     a2a.TransportProtocolGRPC,
			want:          "agent.example.com:443",
		},
		{
			name:          "public base — gRPC keeps explicit port",
			publicBaseURL: "https://agent.example.com:8443",
			host:          "broker.local",
			port:          9102,
			transport:     a2a.TransportProtocolGRPC,
			want:          "agent.example.com:8443",
		},
		{
			name:          "public base — gRPC honors http scheme port 80",
			publicBaseURL: "http://agent.example.com",
			host:          "broker.local",
			port:          9102,
			transport:     a2a.TransportProtocolGRPC,
			want:          "agent.example.com:80",
		},
		{
			name:          "public base — gRPC accepts scheme-less host:port",
			publicBaseURL: "agent.example.com:50051",
			host:          "broker.local",
			port:          9102,
			transport:     a2a.TransportProtocolGRPC,
			want:          "agent.example.com:50051",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AdvertisedURL(tc.publicBaseURL, tc.host, tc.port, tc.transport)
			assert.Equal(t, tc.want, got)
		})
	}
}
