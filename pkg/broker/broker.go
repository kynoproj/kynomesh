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
)

const JSONRPCEndpoint = "/rpc"
const RESTEndpoint = "/api"

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
