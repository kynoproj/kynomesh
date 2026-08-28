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
	"maps"

	corev1 "k8s.io/api/core/v1"
)

// ContainerTemplate defines customized spec for a container
type ContainerTemplate struct {
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty" protobuf:"bytes,1,opt,name=resources"`
	// +optional
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty" protobuf:"bytes,2,opt,name=imagePullPolicy,casttype=PullPolicy"`
	// +optional
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty" protobuf:"bytes,3,opt,name=securityContext"`
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty" protobuf:"bytes,4,rep,name=env"`
	// +optional
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty" protobuf:"bytes,5,rep,name=envFrom"`
}

// ApplyToContainer updates the Container with the values from the
// ContainerTemplate.
func (ct *ContainerTemplate) ApplyToContainer(c *corev1.Container) {
	c.Resources.Requests = mergeResourceList(c.Resources.Requests, ct.Resources.Requests)
	c.Resources.Limits = mergeResourceList(c.Resources.Limits, ct.Resources.Limits)
	c.Resources.Claims = mergeResourceClaims(c.Resources.Claims, ct.Resources.Claims)
	c.SecurityContext = ct.SecurityContext
	// ImagePullPolicy is intentionally container-wins (unlike the fields above,
	// where the template overrides). The container's policy is stamped from
	// KYNOMESH_IMAGE_PULL_POLICY, an environment-level guardrail (e.g.
	// IfNotPresent for local/test clusters). The template only fills it in when the
	// environment left it unset, so a per-AgentSet template can't override that
	// guardrail.
	if c.ImagePullPolicy == "" {
		c.ImagePullPolicy = ct.ImagePullPolicy
	}
	if len(ct.Env) > 0 {
		c.Env = append(c.Env, ct.Env...)
	}
	if len(ct.EnvFrom) > 0 {
		c.EnvFrom = append(c.EnvFrom, ct.EnvFrom...)
	}
}

// ApplyDefaultResourcesIfMissing sets c.Resources to defaults, but only if the
// container has no resources of its own.
func ApplyDefaultResourcesIfMissing(c *corev1.Container, defaults corev1.ResourceRequirements) {
	if len(c.Resources.Requests) > 0 || len(c.Resources.Limits) > 0 {
		return
	}
	c.Resources = defaults
}

// ApplyDefaultsFrom fills the receiver's unset fields from other, so other acts
// as defaults that the receiver's own values override (fill-if-unset), field by
// field.
func (ct *ContainerTemplate) ApplyDefaultsFrom(other *ContainerTemplate) {
	if other == nil {
		return
	}
	ct.Resources.Requests = mergeResourceDefaults(ct.Resources.Requests, other.Resources.Requests)
	ct.Resources.Limits = mergeResourceDefaults(ct.Resources.Limits, other.Resources.Limits)
	ct.Resources.Claims = mergeResourceClaims(ct.Resources.Claims, other.Resources.Claims)
	if ct.ImagePullPolicy == "" {
		ct.ImagePullPolicy = other.ImagePullPolicy
	}
	if ct.SecurityContext == nil {
		ct.SecurityContext = other.SecurityContext
	}
	ct.Env = mergeEnvDefaults(ct.Env, other.Env)
	if len(ct.EnvFrom) == 0 {
		ct.EnvFrom = other.EnvFrom
	}
}

// mergeResourceDefaults returns own with any key only present in defaults added
// (own wins). Returns own unchanged when defaults is empty.
func mergeResourceDefaults(own, defaults corev1.ResourceList) corev1.ResourceList {
	if len(defaults) == 0 {
		return own
	}
	out := make(corev1.ResourceList, len(own)+len(defaults))
	maps.Copy(out, defaults)
	maps.Copy(out, own) // own wins
	return out
}

// mergeEnvDefaults returns own with any env var whose name is only present in
// defaults appended (own wins on name collisions). Returns own unchanged when
// defaults is empty.
func mergeEnvDefaults(own, defaults []corev1.EnvVar) []corev1.EnvVar {
	if len(defaults) == 0 {
		return own
	}
	seen := make(map[string]struct{}, len(own))
	for _, e := range own {
		seen[e.Name] = struct{}{}
	}
	out := own
	for _, e := range defaults {
		if _, ok := seen[e.Name]; !ok {
			out = append(out, e)
		}
	}
	return out
}

// mergeResourceList returns base with override applied per-key. Returns
// base unchanged when override is empty so a nil-template field doesn't
// allocate an empty map onto the container.
func mergeResourceList(base, override corev1.ResourceList) corev1.ResourceList {
	if len(override) == 0 {
		return base
	}
	out := make(corev1.ResourceList, len(base)+len(override))
	maps.Copy(out, base)
	maps.Copy(out, override)
	return out
}

// mergeResourceClaims returns own with any claim whose name is only present in
// other appended (own wins on name collisions). Returns own unchanged when
// other is empty.
func mergeResourceClaims(own, other []corev1.ResourceClaim) []corev1.ResourceClaim {
	if len(other) == 0 {
		return own
	}
	seen := make(map[string]struct{}, len(own))
	for _, c := range own {
		seen[c.Name] = struct{}{}
	}
	out := own
	for _, c := range other {
		if _, ok := seen[c.Name]; !ok {
			out = append(out, c)
		}
	}
	return out
}
