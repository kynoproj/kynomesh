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

package discovery

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubResolver struct {
	ips []string
	err error
}

func (s stubResolver) LookupHost(_ context.Context, _ string) ([]string, error) {
	return s.ips, s.err
}

func TestDiscover_NormalCase(t *testing.T) {
	r := stubResolver{ips: []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}}
	hosts, err := Discover(context.Background(), r, "my-agentset", "greeter", "default")
	require.NoError(t, err)
	assert.Equal(t, []string{
		"my-agentset-greeter-0.my-agentset-greeter-headless.default.svc",
		"my-agentset-greeter-1.my-agentset-greeter-headless.default.svc",
		"my-agentset-greeter-2.my-agentset-greeter-headless.default.svc",
	}, hosts)
}

func TestDiscover_NoReadyPods_NotAnError(t *testing.T) {
	r := stubResolver{err: &net.DNSError{Err: "no such host", IsNotFound: true}}
	hosts, err := Discover(context.Background(), r, "my-agentset", "greeter", "default")
	require.NoError(t, err)
	assert.Empty(t, hosts)
}

func TestDiscover_GenuineError(t *testing.T) {
	r := stubResolver{err: errors.New("network unreachable")}
	_, err := Discover(context.Background(), r, "my-agentset", "greeter", "default")
	require.Error(t, err)
}

func TestDiscover_EmptyResultNoError(t *testing.T) {
	r := stubResolver{ips: nil}
	hosts, err := Discover(context.Background(), r, "my-agentset", "greeter", "default")
	require.NoError(t, err)
	assert.Empty(t, hosts)
}
