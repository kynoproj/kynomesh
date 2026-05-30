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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

// TestLoadInjectedAgentDeploy_RoundTrip covers the happy path: the
// reconciler stamped an AgentDeploy blob into env, the broker decodes
// it, and the resulting struct carries the expected identity. This
// blob is later stashed on brokerRuntime so downstream consumers can
// read it without re-decoding.
func TestLoadInjectedAgentDeploy_RoundTrip(t *testing.T) {
	original := &kmv1.AgentDeploy{}
	original.Namespace = "demo-ns"
	original.Name = "demo-ad"
	encoded := kmv1.EncodeAgentDeploy(original)
	t.Setenv(kmv1.EnvAgentDeployObject, encoded)

	ad, err := loadInjectedAgentDeploy()
	require.NoError(t, err)
	require.NotNil(t, ad)
	assert.Equal(t, "demo-ad", ad.Name)
	assert.Equal(t, "demo-ns", ad.Namespace)
}

// TestLoadInjectedAgentDeploy_EnvUnset is the local-dev path. The env
// var is missing; loadInjectedAgentDeploy must return (nil, nil) so
// assembleBroker can transparently fall back without treating
// out-of-cluster invocations as an error.
func TestLoadInjectedAgentDeploy_EnvUnset(t *testing.T) {
	t.Setenv(kmv1.EnvAgentDeployObject, "")

	ad, err := loadInjectedAgentDeploy()
	require.NoError(t, err)
	assert.Nil(t, ad)
}

// TestLoadInjectedAgentDeploy_EnvMalformed exercises a configured-but-
// broken env. The broker must surface the decode error rather than
// silently treating misconfiguration as "no AgentDeploy injected" —
// otherwise an operator mistake would manifest as a wrong-looking
// AgentCard URL with no obvious signal.
func TestLoadInjectedAgentDeploy_EnvMalformed(t *testing.T) {
	t.Setenv(kmv1.EnvAgentDeployObject, "not-valid-base64!!!")

	ad, err := loadInjectedAgentDeploy()
	assert.Error(t, err)
	assert.Nil(t, ad)
}

// TestAdvertiseHostFor formats the headless-Service FQDN that the
// AgentCard advertises. The host is service-scoped, not pod-scoped:
// "<ad>-headless.<ns>.svc.cluster.local" resolves to every replica
// behind the headless Service via DNS A-record list.
func TestAdvertiseHostFor(t *testing.T) {
	cases := []struct {
		name string
		ad   *kmv1.AgentDeploy
		want string
	}{
		{
			name: "nil AgentDeploy yields empty so caller picks fallback",
			ad:   nil,
			want: "",
		},
		{
			name: "headless-service FQDN",
			ad: func() *kmv1.AgentDeploy {
				ad := &kmv1.AgentDeploy{}
				ad.Namespace = "demo-ns"
				ad.Name = "demo-ad"
				return ad
			}(),
			want: "demo-ad-headless.demo-ns.svc.cluster.local",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, advertiseHostFor(tc.ad))
		})
	}
}
