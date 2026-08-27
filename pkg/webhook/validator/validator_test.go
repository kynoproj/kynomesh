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

package validator

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

func TestGetValidator(t *testing.T) {
	t.Run("returns an AgentSet validator for the AgentSet kind", func(t *testing.T) {
		newBytes, err := json.Marshal(validAgentSet())
		require.NoError(t, err)

		v, err := GetValidator(context.Background(), nil, metav1.GroupVersionKind(kmv1.AgentSetGroupVersionKind), nil, newBytes)
		require.NoError(t, err)
		require.NotNil(t, v)

		resp := v.ValidateCreate(context.Background())
		require.NotNil(t, resp)
		assert.True(t, resp.Allowed)
	})

	t.Run("propagates old and new objects into the AgentSet validator", func(t *testing.T) {
		oldBytes, err := json.Marshal(validAgentSet())
		require.NoError(t, err)
		newBytes, err := json.Marshal(invalidAgentSet())
		require.NoError(t, err)

		v, err := GetValidator(context.Background(), nil, metav1.GroupVersionKind(kmv1.AgentSetGroupVersionKind), oldBytes, newBytes)
		require.NoError(t, err)
		require.NotNil(t, v)

		resp := v.ValidateUpdate(context.Background())
		require.NotNil(t, resp)
		assert.False(t, resp.Allowed)
	})

	t.Run("errors on malformed new object json", func(t *testing.T) {
		_, err := GetValidator(context.Background(), nil, metav1.GroupVersionKind(kmv1.AgentSetGroupVersionKind), nil, []byte("{not-json"))
		assert.Error(t, err)
	})

	t.Run("errors on malformed old object json", func(t *testing.T) {
		newBytes, err := json.Marshal(validAgentSet())
		require.NoError(t, err)
		_, err = GetValidator(context.Background(), nil, metav1.GroupVersionKind(kmv1.AgentSetGroupVersionKind), []byte("{not-json"), newBytes)
		assert.Error(t, err)
	})

	t.Run("errors on unrecognized kind", func(t *testing.T) {
		_, err := GetValidator(context.Background(), nil, metav1.GroupVersionKind{Group: "kynomesh.kyno.sh", Version: "v1alpha1", Kind: "NotAKind"}, nil, nil)
		assert.Error(t, err)
	})
}

func TestDeniedResponse(t *testing.T) {
	t.Run("without format args", func(t *testing.T) {
		resp := DeniedResponse("plain reason")
		require.NotNil(t, resp)
		assert.False(t, resp.Allowed)
		require.NotNil(t, resp.Result)
		assert.Equal(t, "plain reason", resp.Result.Message)
	})

	t.Run("with format args", func(t *testing.T) {
		resp := DeniedResponse("failed: %v", "boom")
		require.NotNil(t, resp)
		assert.False(t, resp.Allowed)
		require.NotNil(t, resp.Result)
		assert.Contains(t, resp.Result.Message, "boom")
	})
}

func TestAllowedResponse(t *testing.T) {
	resp := AllowedResponse()
	require.NotNil(t, resp)
	assert.True(t, resp.Allowed)
	assert.Nil(t, resp.Result)
}
