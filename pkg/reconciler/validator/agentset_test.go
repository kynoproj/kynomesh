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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

// agentSet builds a minimal AgentSet for validator tests.
func agentSet(agents ...string) *kmv1.AgentSet {
	spec := kmv1.AgentSetSpec{
		Pattern: kmv1.AgentPatternSupervisor,
	}
	for _, a := range agents {
		spec.Agents = append(spec.Agents, kmv1.AbstractAgentDeploy{Name: a})
	}
	if len(agents) > 0 {
		spec.Entry = agents[0]
	}
	return &kmv1.AgentSet{Spec: spec}
}

func TestValidateAgentSet(t *testing.T) {
	tests := []struct {
		name    string
		agents  []string
		wantErr string
	}{
		{name: "no agents", agents: nil},
		{name: "ok", agents: []string{"a", "b"}},
		{name: "empty name", agents: []string{""}, wantErr: "non-empty"},
		{name: "duplicate", agents: []string{"a", "a"}, wantErr: "duplicate"},
		{name: "reserved name ingress", agents: []string{kmv1.EntryServiceSuffix}, wantErr: "reserved"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateAgentSet(agentSet(tc.agents...))
			if tc.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestValidateAgentSet_PatternAndEntry(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(as *kmv1.AgentSet)
		wantErr string
	}{
		{
			name:   "Supervisor with single agent is valid",
			mutate: func(*kmv1.AgentSet) {},
		},
		{
			name: "Handoff with single agent rejected",
			mutate: func(as *kmv1.AgentSet) {
				as.Spec.Pattern = kmv1.AgentPatternHandoff
			},
			wantErr: "requires at least 2 agents",
		},
		{
			name: "Sequential with single agent rejected",
			mutate: func(as *kmv1.AgentSet) {
				as.Spec.Pattern = kmv1.AgentPatternSequential
			},
			wantErr: "requires at least 2 agents",
		},
		{
			name: "Entry referencing missing agent rejected",
			mutate: func(as *kmv1.AgentSet) {
				as.Spec.Entry = "ghost"
			},
			wantErr: "does not name any agent",
		},
		{
			name: "unknown Pattern rejected",
			mutate: func(as *kmv1.AgentSet) {
				as.Spec.Pattern = kmv1.AgentPattern("Mystery")
			},
			wantErr: "unsupported pattern",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			as := agentSet("a")
			tc.mutate(as)
			err := ValidateAgentSet(as)
			if tc.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestValidateAgentSet_HandoffAndSequentialMultiAgent(t *testing.T) {
	t.Run("Handoff with >=2 agents valid", func(t *testing.T) {
		as := agentSet("a", "b", "c")
		as.Spec.Pattern = kmv1.AgentPatternHandoff
		assert.NoError(t, ValidateAgentSet(as))
	})

	t.Run("Sequential entry must be first agent", func(t *testing.T) {
		as := agentSet("a", "b", "c")
		as.Spec.Pattern = kmv1.AgentPatternSequential
		as.Spec.Entry = "b"
		err := ValidateAgentSet(as)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "first agent")
	})

	t.Run("Sequential entry == agents[0] valid", func(t *testing.T) {
		as := agentSet("a", "b", "c")
		as.Spec.Pattern = kmv1.AgentPatternSequential
		// agentSet already sets Entry to agents[0] = "a"
		assert.NoError(t, ValidateAgentSet(as))
	})
}
