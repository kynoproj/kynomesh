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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

func validAgentSet() *kmv1.AgentSet {
	return &kmv1.AgentSet{
		ObjectMeta: metav1.ObjectMeta{Name: "greeter"},
		Spec: kmv1.AgentSetSpec{
			Pattern: kmv1.AgentPatternSupervisor,
			Entry:   "a",
			Agents: []kmv1.AbstractAgentDeploy{
				{Name: "a"},
				{Name: "b"},
			},
		},
	}
}

func invalidAgentSet() *kmv1.AgentSet {
	as := validAgentSet()
	as.Spec.Agents = nil
	return as
}

func TestAgentSetValidator_ValidateCreate(t *testing.T) {
	t.Run("allowed for a valid spec", func(t *testing.T) {
		v := NewAgentSetValidator(nil, validAgentSet())
		resp := v.ValidateCreate(context.Background())
		require.NotNil(t, resp)
		assert.True(t, resp.Allowed)
	})

	t.Run("denied for an invalid spec", func(t *testing.T) {
		v := NewAgentSetValidator(nil, invalidAgentSet())
		resp := v.ValidateCreate(context.Background())
		require.NotNil(t, resp)
		assert.False(t, resp.Allowed)
		require.NotNil(t, resp.Result)
		assert.Contains(t, resp.Result.Message, "at least one agent")
	})
}

func TestAgentSetValidator_ValidateUpdate(t *testing.T) {
	t.Run("allowed when new spec is valid", func(t *testing.T) {
		v := NewAgentSetValidator(validAgentSet(), validAgentSet())
		resp := v.ValidateUpdate(context.Background())
		require.NotNil(t, resp)
		assert.True(t, resp.Allowed)
	})

	t.Run("denied when new spec is invalid, even if old spec was valid", func(t *testing.T) {
		v := NewAgentSetValidator(validAgentSet(), invalidAgentSet())
		resp := v.ValidateUpdate(context.Background())
		require.NotNil(t, resp)
		assert.False(t, resp.Allowed)
		require.NotNil(t, resp.Result)
		assert.Contains(t, resp.Result.Message, "at least one agent")
	})
}
