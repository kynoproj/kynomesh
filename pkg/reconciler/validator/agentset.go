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
	"net/url"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

// reservedAgentNames maps agent-name strings to a human-readable
// label of the controller-owned object they collide with. Adding a
// name here rejects any AgentSet that uses it.
var reservedAgentNames = map[string]string{
	kmv1.EntryServiceSuffix: "AgentSet entry service",
	kmv1.DaemonSuffix:       "AgentSet daemon service",
}

// MaxChildAgentDeployNameLen caps the AgentSet+agent combined name
// length so the derived pod object names ("<set>-<agent>-<replica>-
// <rand5>") stay under Kubernetes' 63-char DNS label limit even for
// six-digit replica counts. 63 − 1 (dash) − 1 (dash) − 6 (replica) −
// 5 (random suffix) = 50.
const MaxChildAgentDeployNameLen = 50

// ValidateAgentSet validates the spec.
func ValidateAgentSet(as *kmv1.AgentSet) error {
	if errs := validation.IsDNS1035Label(as.Name); len(errs) > 0 {
		return fmt.Errorf("invalid AgentSet name %q (must satisfy DNS-1035 for Service-name compatibility): %s",
			as.Name, strings.Join(errs, "; "))
	}

	if len(as.Spec.Agents) == 0 {
		return errors.New("spec.agents must contain at least one agent")
	}

	seen := make(map[string]struct{}, len(as.Spec.Agents))
	for _, a := range as.Spec.Agents {
		if a.Name == "" {
			return errors.New("agent name must be non-empty")
		}
		if errs := validation.IsDNS1035Label(a.Name); len(errs) > 0 {
			return fmt.Errorf("invalid agent name %q (must satisfy DNS-1035 for Service-name compatibility): %s",
				a.Name, strings.Join(errs, "; "))
		}
		if collidesWith, ok := reservedAgentNames[a.Name]; ok {
			return fmt.Errorf("agent name %q is reserved; it collides with the %s", a.Name, collidesWith)
		}
		if _, dup := seen[a.Name]; dup {
			return fmt.Errorf("duplicate agent name %q", a.Name)
		}
		seen[a.Name] = struct{}{}
		if name := as.ChildAgentDeployName(a.Name); len(name) > MaxChildAgentDeployNameLen {
			return fmt.Errorf(
				"combined AgentSet+agent name %q (%d chars) exceeds the %d-char limit; "+
					"shorten the AgentSet name or the agent name",
				name, len(name), MaxChildAgentDeployNameLen)
		}
		if p := a.Scale.TargetSaturationPercentage; p != nil && *p > 100 {
			return fmt.Errorf(
				"agent %q scale.targetSaturationPercentage is %d; it must be in [1,100] "+
					"(it is the fraction of a replica's capacity to run at)",
				a.Name, *p)
		}
	}

	for _, e := range as.Spec.ExternalAgents {
		if e.Name == "" {
			return errors.New("external agent name must be non-empty")
		}
		if errs := validation.IsDNS1035Label(e.Name); len(errs) > 0 {
			return fmt.Errorf("invalid external agent name %q (must satisfy DNS-1035 for Service-name compatibility): %s",
				e.Name, strings.Join(errs, "; "))
		}
		if collidesWith, ok := reservedAgentNames[e.Name]; ok {
			return fmt.Errorf("external agent name %q is reserved; it collides with the %s", e.Name, collidesWith)
		}
		if _, dup := seen[e.Name]; dup {
			return fmt.Errorf("duplicate agent name %q (external agents share a name namespace with spec.agents)", e.Name)
		}
		seen[e.Name] = struct{}{}
		if e.URL == "" {
			return fmt.Errorf("external agent %q: url must be non-empty", e.Name)
		}
		parsed, err := url.Parse(e.URL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("external agent %q: url %q must be an absolute URL (scheme + host)", e.Name, e.URL)
		}
	}

	entryIsExternal := false
	for _, e := range as.Spec.ExternalAgents {
		if e.Name == as.Spec.Entry {
			entryIsExternal = true
			break
		}
	}
	if entryIsExternal {
		return fmt.Errorf("entry %q must be a managed agent; external agents can only receive calls, never be Entry", as.Spec.Entry)
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
		if len(as.Spec.ExternalAgents) > 1 {
			return fmt.Errorf("pattern %q supports at most 1 external agent (as the final hop); got %d",
				as.Spec.Pattern, len(as.Spec.ExternalAgents))
		}
	default:
		return fmt.Errorf("unsupported pattern %q (allowed: Supervisor, Handoff, Sequential)", as.Spec.Pattern)
	}
	return nil
}
