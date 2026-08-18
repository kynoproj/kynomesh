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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
	return &kmv1.AgentSet{
		ObjectMeta: metav1.ObjectMeta{Name: "greeter"},
		Spec:       spec,
	}
}

func TestValidateAgentSet(t *testing.T) {
	tests := []struct {
		name    string
		agents  []string
		wantErr string
	}{
		{name: "no agents", agents: nil, wantErr: "at least one agent"},
		{name: "ok", agents: []string{"a", "b"}},
		{name: "empty name", agents: []string{""}, wantErr: "non-empty"},
		{name: "duplicate", agents: []string{"a", "a"}, wantErr: "duplicate"},
		{name: "reserved name ingress", agents: []string{kmv1.EntryServiceSuffix}, wantErr: "reserved"},
		{name: "reserved name daemon", agents: []string{kmv1.DaemonSuffix}, wantErr: "reserved"},
		{name: "agent name with uppercase", agents: []string{"Alpha"}, wantErr: "DNS-1035"},
		{name: "agent name with underscore", agents: []string{"alpha_one"}, wantErr: "DNS-1035"},
		{name: "agent name starts with digit", agents: []string{"1alpha"}, wantErr: "DNS-1035"},
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

func TestValidateAgentSet_InvalidAgentSetName(t *testing.T) {
	tests := []struct {
		name    string
		setName string
		wantErr string
	}{
		{name: "starts with digit", setName: "1set", wantErr: "DNS-1035"},
		{name: "has uppercase", setName: "MySet", wantErr: "DNS-1035"},
		{name: "has underscore", setName: "my_set", wantErr: "DNS-1035"},
		{name: "empty", setName: "", wantErr: "DNS-1035"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			as := agentSet("alpha")
			as.Name = tc.setName
			err := ValidateAgentSet(as)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestValidateAgentSet_CombinedNameLength(t *testing.T) {
	// MaxChildAgentDeployNameLen is 50: "<set>-<agent>" must fit.
	// Pick lengths so the sum + 1 (dash) just crosses the boundary.
	t.Run("at limit", func(t *testing.T) {
		as := agentSet("alpha")                            // override below
		as.Name = "abcdefghijklmnopqrstuvwxyz"             // 26 chars
		as.Spec.Agents[0].Name = "abcdefghijklmnopqrstuvw" // 23 chars → 26+1+23 = 50
		as.Spec.Entry = as.Spec.Agents[0].Name
		assert.NoError(t, ValidateAgentSet(as))
	})
	t.Run("just over limit", func(t *testing.T) {
		as := agentSet("alpha")
		as.Name = "abcdefghijklmnopqrstuvwxyz"              // 26 chars
		as.Spec.Agents[0].Name = "abcdefghijklmnopqrstuvwx" // 24 chars → 26+1+24 = 51
		as.Spec.Entry = as.Spec.Agents[0].Name
		err := ValidateAgentSet(as)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds")
		assert.Contains(t, err.Error(), "50")
	})
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

func TestValidateAgentSet_ExternalAgents(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(as *kmv1.AgentSet)
		wantErr string
	}{
		{
			name: "valid external agent",
			mutate: func(as *kmv1.AgentSet) {
				as.Spec.Pattern = kmv1.AgentPatternHandoff
				as.Spec.ExternalAgents = []kmv1.ExternalAgentRef{{Name: "ext", URL: "https://ext.example.com"}}
			},
		},
		{
			name: "empty name rejected",
			mutate: func(as *kmv1.AgentSet) {
				as.Spec.ExternalAgents = []kmv1.ExternalAgentRef{{Name: "", URL: "https://ext.example.com"}}
			},
			wantErr: "non-empty",
		},
		{
			name: "invalid DNS-1035 name rejected",
			mutate: func(as *kmv1.AgentSet) {
				as.Spec.ExternalAgents = []kmv1.ExternalAgentRef{{Name: "Ext", URL: "https://ext.example.com"}}
			},
			wantErr: "DNS-1035",
		},
		{
			name: "reserved name rejected",
			mutate: func(as *kmv1.AgentSet) {
				as.Spec.ExternalAgents = []kmv1.ExternalAgentRef{{Name: kmv1.DaemonSuffix, URL: "https://ext.example.com"}}
			},
			wantErr: "reserved",
		},
		{
			name: "name colliding with a managed agent rejected",
			mutate: func(as *kmv1.AgentSet) {
				as.Spec.ExternalAgents = []kmv1.ExternalAgentRef{{Name: "a", URL: "https://ext.example.com"}}
			},
			wantErr: "duplicate",
		},
		{
			name: "duplicate external agent names rejected",
			mutate: func(as *kmv1.AgentSet) {
				as.Spec.Pattern = kmv1.AgentPatternHandoff
				as.Spec.ExternalAgents = []kmv1.ExternalAgentRef{
					{Name: "ext", URL: "https://ext1.example.com"},
					{Name: "ext", URL: "https://ext2.example.com"},
				}
			},
			wantErr: "duplicate",
		},
		{
			name: "empty URL rejected",
			mutate: func(as *kmv1.AgentSet) {
				as.Spec.ExternalAgents = []kmv1.ExternalAgentRef{{Name: "ext", URL: ""}}
			},
			wantErr: "url must be non-empty",
		},
		{
			name: "malformed URL rejected",
			mutate: func(as *kmv1.AgentSet) {
				as.Spec.ExternalAgents = []kmv1.ExternalAgentRef{{Name: "ext", URL: "not-a-url"}}
			},
			wantErr: "absolute URL",
		},
		{
			name: "URL without scheme rejected",
			mutate: func(as *kmv1.AgentSet) {
				as.Spec.ExternalAgents = []kmv1.ExternalAgentRef{{Name: "ext", URL: "ext.example.com"}}
			},
			wantErr: "absolute URL",
		},
		{
			name: "entry cannot be an external agent",
			mutate: func(as *kmv1.AgentSet) {
				as.Spec.Pattern = kmv1.AgentPatternHandoff
				as.Spec.ExternalAgents = []kmv1.ExternalAgentRef{{Name: "ext", URL: "https://ext.example.com"}}
				as.Spec.Entry = "ext"
			},
			wantErr: "must be a managed agent",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			as := agentSet("a", "b")
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

func TestValidateAgentSet_SequentialExternalAgentLimit(t *testing.T) {
	t.Run("one external agent allowed", func(t *testing.T) {
		as := agentSet("a", "b")
		as.Spec.Pattern = kmv1.AgentPatternSequential
		as.Spec.ExternalAgents = []kmv1.ExternalAgentRef{{Name: "ext", URL: "https://ext.example.com"}}
		assert.NoError(t, ValidateAgentSet(as))
	})

	t.Run("two external agents rejected", func(t *testing.T) {
		as := agentSet("a", "b")
		as.Spec.Pattern = kmv1.AgentPatternSequential
		as.Spec.ExternalAgents = []kmv1.ExternalAgentRef{
			{Name: "ext1", URL: "https://ext1.example.com"},
			{Name: "ext2", URL: "https://ext2.example.com"},
		}
		err := ValidateAgentSet(as)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at most 1 external agent")
	})

	t.Run("two external agents allowed under Handoff", func(t *testing.T) {
		as := agentSet("a", "b")
		as.Spec.Pattern = kmv1.AgentPatternHandoff
		as.Spec.ExternalAgents = []kmv1.ExternalAgentRef{
			{Name: "ext1", URL: "https://ext1.example.com"},
			{Name: "ext2", URL: "https://ext2.example.com"},
		}
		assert.NoError(t, ValidateAgentSet(as))
	})
}

func TestValidateAgentSet_TargetSaturationPercentage(t *testing.T) {
	as := agentSet("a")
	set := func(v *uint32) { as.Spec.Agents[0].Scale.TargetSaturationPercentage = v }

	over := uint32(150)
	set(&over)
	err := ValidateAgentSet(as)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "targetSaturationPercentage")

	ok := uint32(80)
	set(&ok)
	assert.NoError(t, ValidateAgentSet(as))

	hundred := uint32(100)
	set(&hundred)
	assert.NoError(t, ValidateAgentSet(as), "exactly 100 is allowed")

	set(nil)
	assert.NoError(t, ValidateAgentSet(as), "unset defers to controller default")
}
