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

package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEncodeDecodeAgentDeploy_RoundTrip is the load-bearing contract:
// whatever the reconciler stamps onto the broker container must come
// back identical when the broker decodes it at startup.
func TestEncodeDecodeAgentDeploy_RoundTrip(t *testing.T) {
	original := &AgentDeploy{
		Spec: AgentDeploySpec{
			AbstractAgentDeploy: AbstractAgentDeploy{
				Name: "inner-name",
			},
			AgentSetName: "demo-set",
		},
	}
	original.Namespace = "demo-ns"
	original.Name = "demo-ad"

	encoded := EncodeAgentDeploy(original)
	require.NotEmpty(t, encoded)

	decoded, err := DecodeAgentDeploy(encoded)
	require.NoError(t, err)
	require.NotNil(t, decoded)
	assert.Equal(t, "demo-ad", decoded.Name)
	assert.Equal(t, "demo-ns", decoded.Namespace)
	assert.Equal(t, "demo-set", decoded.Spec.AgentSetName)
	assert.Equal(t, "inner-name", decoded.Spec.Name)
}

func TestDecodeAgentDeploy_RejectsEmpty(t *testing.T) {
	ad, err := DecodeAgentDeploy("")
	assert.Error(t, err)
	assert.Nil(t, ad)
}

func TestDecodeAgentDeploy_RejectsInvalidBase64(t *testing.T) {
	ad, err := DecodeAgentDeploy("not-valid-base64!!!")
	assert.Error(t, err)
	assert.Nil(t, ad)
}

func TestDecodeAgentDeploy_RejectsBadJSON(t *testing.T) {
	// Valid base64 of non-JSON content — DecodeAgentDeploy should
	// surface the JSON unmarshalling failure rather than silently
	// returning a zero-valued AgentDeploy.
	ad, err := DecodeAgentDeploy("bm90LWpzb24=") // "not-json"
	assert.Error(t, err)
	assert.Nil(t, ad)
}
