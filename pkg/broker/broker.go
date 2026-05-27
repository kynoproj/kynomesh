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

// Package broker hosts the kynomesh A2A broker service. The broker exposes
// the Agent-to-Agent (A2A) protocol — see https://github.com/a2aproject/A2A —
// to upstream agents over every transport the a2a-go server supports
// (JSON-RPC, REST/HTTP-JSON, and gRPC), backed by a single transport-agnostic
// RequestHandler.
package broker

import (
	"context"
	"fmt"
	"iter"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// AgentName is the default Name advertised on the AgentCard. It can be
// overridden by the caller when building a card.
const AgentName = "kynomesh-broker"

// echoExecutor is the default a2asrv.AgentExecutor wired into the broker
// when no custom executor is supplied. It satisfies the A2A contract with
// a minimal, dependency-free implementation so the server can come up and
// be exercised end-to-end before real agent integrations are plugged in.
//
// The real implementation will route messages through to managed AgentDeploy
// pods; until then, echoing back to the caller keeps the protocol shape
// observable.
type echoExecutor struct{}

var _ a2asrv.AgentExecutor = (*echoExecutor)(nil)

// Execute returns an iterator that yields a single agent-role message
// echoing back the protocol's "hello" so callers can verify connectivity.
func (*echoExecutor) Execute(_ context.Context, _ *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("hello from kynomesh broker"))
		yield(msg, nil)
	}
}

// Cancel is a no-op for the echo executor — there's no long-running work
// to interrupt.
func (*echoExecutor) Cancel(_ context.Context, _ *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {}
}

// NewDefaultExecutor returns the broker's default a2asrv.AgentExecutor. It is
// safe to share across all transports because each Execute call returns a
// fresh iterator.
func NewDefaultExecutor() a2asrv.AgentExecutor {
	return &echoExecutor{}
}

// NewAgentCard builds the AgentCard the broker advertises at
// a2asrv.WellKnownAgentCardPath. The card lists every transport the broker
// serves so callers can negotiate JSON-RPC, REST, or gRPC against the same
// logical agent.
//
// jsonRPCURL / restURL / grpcURL should be the externally reachable
// addresses for each transport ("" disables that transport's interface
// entry — useful for tests).
func NewAgentCard(jsonRPCURL, restURL, grpcURL string) *a2a.AgentCard {
	var interfaces []*a2a.AgentInterface
	if jsonRPCURL != "" {
		interfaces = append(interfaces, a2a.NewAgentInterface(jsonRPCURL, a2a.TransportProtocolJSONRPC))
	}
	if restURL != "" {
		interfaces = append(interfaces, a2a.NewAgentInterface(restURL, a2a.TransportProtocolHTTPJSON))
	}
	if grpcURL != "" {
		interfaces = append(interfaces, a2a.NewAgentInterface(grpcURL, a2a.TransportProtocolGRPC))
	}

	return &a2a.AgentCard{
		Name:                AgentName,
		Description:         "Kynomesh A2A broker — multi-transport gateway into managed agents.",
		SupportedInterfaces: interfaces,
		DefaultInputModes:   []string{"text"},
		DefaultOutputModes:  []string{"text"},
		Capabilities:        a2a.AgentCapabilities{Streaming: true},
		Skills: []a2a.AgentSkill{
			{
				ID:          "echo",
				Name:        "echo",
				Description: "Echoes a fixed greeting; placeholder skill while routing to real agents is wired up.",
				Tags:        []string{"diagnostic"},
				Examples:    []string{"ping"},
			},
		},
	}
}

// JSONRPCEndpoint is the path the JSON-RPC transport is mounted at on the
// broker's HTTP listener.
const JSONRPCEndpoint = "/rpc"

// RESTEndpoint is the path prefix the REST/HTTP-JSON transport is mounted
// at on the broker's HTTP listener. The a2a-go REST handler serves its
// own /v2/... routes underneath, so this prefix is stripped before dispatch.
const RESTEndpoint = "/api"

// JSONRPCAddr / RESTAddr / GRPCAddr format the bind address into the URL
// shape each transport advertises on its AgentInterface entry. The broker
// terminates TLS on its single shared port using an in-process self-signed
// cert, so JSON-RPC and REST advertise https URLs and gRPC clients must
// dial with TLS credentials.
func JSONRPCAddr(host string, port int) string {
	return fmt.Sprintf("https://%s:%d%s", host, port, JSONRPCEndpoint)
}

func RESTAddr(host string, port int) string {
	return fmt.Sprintf("https://%s:%d%s", host, port, RESTEndpoint)
}

func GRPCAddr(host string, port int) string {
	return fmt.Sprintf("%s:%d", host, port)
}
