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
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddrHelpers(t *testing.T) {
	assert.Equal(t, "http://broker.local:9100/rpc", JSONRPCAddr("broker.local", 9100))
	assert.Equal(t, "http://broker.local:9101/api", RESTAddr("broker.local", 9101))
	assert.Equal(t, "broker.local:9102", GRPCAddr("broker.local", 9102))
}

func TestNewAgentCard_AllTransports(t *testing.T) {
	card := NewAgentCard(
		"http://h:1/rpc",
		"http://h:2",
		"h:3",
	)
	require.NotNil(t, card)
	assert.Equal(t, AgentName, card.Name)
	assert.NotEmpty(t, card.Description)
	assert.True(t, card.Capabilities.Streaming)
	assert.Equal(t, []string{"text"}, card.DefaultInputModes)
	assert.Equal(t, []string{"text"}, card.DefaultOutputModes)

	require.Len(t, card.SupportedInterfaces, 3)
	gotProtocols := make([]a2a.TransportProtocol, 0, len(card.SupportedInterfaces))
	for _, iface := range card.SupportedInterfaces {
		gotProtocols = append(gotProtocols, iface.ProtocolBinding)
	}
	assert.Contains(t, gotProtocols, a2a.TransportProtocolJSONRPC)
	assert.Contains(t, gotProtocols, a2a.TransportProtocolHTTPJSON)
	assert.Contains(t, gotProtocols, a2a.TransportProtocolGRPC)

	require.NotEmpty(t, card.Skills)
	assert.Equal(t, "echo", card.Skills[0].ID)
}

func TestNewAgentCard_OmitsEmptyTransports(t *testing.T) {
	// Empty URLs should drop the corresponding AgentInterface — useful in
	// tests where only one transport is exercised.
	tests := []struct {
		name              string
		jsonRPC, rest, gp string
		wantCount         int
		want              a2a.TransportProtocol
	}{
		{name: "only JSON-RPC", jsonRPC: "http://h/rpc", wantCount: 1, want: a2a.TransportProtocolJSONRPC},
		{name: "only REST", rest: "http://h", wantCount: 1, want: a2a.TransportProtocolHTTPJSON},
		{name: "only gRPC", gp: "h:1", wantCount: 1, want: a2a.TransportProtocolGRPC},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			card := NewAgentCard(tc.jsonRPC, tc.rest, tc.gp)
			require.Len(t, card.SupportedInterfaces, tc.wantCount)
			assert.Equal(t, tc.want, card.SupportedInterfaces[0].ProtocolBinding)
		})
	}
}

func TestNewAgentCard_NoTransports(t *testing.T) {
	card := NewAgentCard("", "", "")
	assert.Empty(t, card.SupportedInterfaces)
}

func TestNewDefaultExecutor_EchoEmitsAgentMessage(t *testing.T) {
	exec := NewDefaultExecutor()
	require.NotNil(t, exec)

	events := exec.Execute(context.Background(), nil)
	require.NotNil(t, events)

	var got []a2a.Event
	for ev, err := range events {
		require.NoError(t, err)
		got = append(got, ev)
	}
	require.Len(t, got, 1, "echo executor yields exactly one event")

	msg, ok := got[0].(*a2a.Message)
	require.True(t, ok, "expected event to be *a2a.Message, got %T", got[0])
	assert.Equal(t, a2a.MessageRoleAgent, msg.Role)
	require.NotEmpty(t, msg.Parts)
}

func TestNewDefaultExecutor_CancelIsNoOp(t *testing.T) {
	exec := NewDefaultExecutor()

	events := exec.Cancel(context.Background(), nil)
	require.NotNil(t, events)

	count := 0
	for _, err := range events {
		require.NoError(t, err)
		count++
	}
	assert.Zero(t, count, "Cancel should yield no events")
}
