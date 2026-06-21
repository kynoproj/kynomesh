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

// Package validator contains structural validation logic for kynomesh
// user-facing custom resources.
package validator

import (
	"errors"
	"fmt"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

// ValidateAgentSet validates the spec.
func ValidateAgentSet(as *kmv1.AgentSet) error {
	if len(as.Spec.Agents) == 0 {
		return errors.New("spec.agents must contain at least one agent")
	}

	seen := make(map[string]struct{}, len(as.Spec.Agents))
	for _, a := range as.Spec.Agents {
		if a.Name == "" {
			return errors.New("agent name must be non-empty")
		}
		if a.Name == kmv1.EntryServiceSuffix {
			return fmt.Errorf("agent name %q is reserved; it collides with the AgentSet entry service", a.Name)
		}
		if _, dup := seen[a.Name]; dup {
			return fmt.Errorf("duplicate agent name %q", a.Name)
		}
		seen[a.Name] = struct{}{}
	}

	if _, ok := seen[as.Spec.Entry]; !ok {
		return fmt.Errorf("entry %q does not name any agent in spec.agents", as.Spec.Entry)
	}

	switch as.Spec.Pattern {
	case kmv1.AgentPatternSupervisor:
		// Supervisor with a single agent is degenerate but not invalid —
		// the entry just has no peers. Allow it.
	case kmv1.AgentPatternHandoff:
		if len(as.Spec.Agents) < 2 {
			return fmt.Errorf("pattern %q requires at least 2 agents; got %d",
				as.Spec.Pattern, len(as.Spec.Agents))
		}
	case kmv1.AgentPatternSequential:
		if len(as.Spec.Agents) < 2 {
			return fmt.Errorf("pattern %q requires at least 2 agents; got %d",
				as.Spec.Pattern, len(as.Spec.Agents))
		}
		if as.Spec.Entry != as.Spec.Agents[0].Name {
			return fmt.Errorf("pattern %q requires entry to be the first agent %q; got %q",
				as.Spec.Pattern, as.Spec.Agents[0].Name, as.Spec.Entry)
		}
	default:
		return fmt.Errorf("unsupported pattern %q (allowed: Supervisor, Handoff, Sequential)", as.Spec.Pattern)
	}
	return nil
}
