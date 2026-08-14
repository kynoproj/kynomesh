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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
)

func TestApplyToContainer_EmptyTemplate(t *testing.T) {
	ct := &ContainerTemplate{}
	c := &corev1.Container{
		Name:            "agent",
		ImagePullPolicy: corev1.PullAlways,
	}

	ct.ApplyToContainer(c)

	assert.Equal(t, "agent", c.Name)
	assert.Equal(t, corev1.PullAlways, c.ImagePullPolicy)
	assert.Nil(t, c.SecurityContext)
	assert.Empty(t, c.Env)
	assert.Empty(t, c.EnvFrom)
}

func TestApplyToContainer_ResourcesMergeWithOverride(t *testing.T) {
	tests := []struct {
		name             string
		templateRequests corev1.ResourceList
		templateLimits   corev1.ResourceList
		containerInit    corev1.ResourceRequirements
		expectedRequests corev1.ResourceList
		expectedLimits   corev1.ResourceList
	}{
		{
			name: "template overrides existing values",
			templateRequests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
			templateLimits: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("1"),
			},
			containerInit: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("100m"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("200m"),
				},
			},
			expectedRequests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
			expectedLimits: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("1"),
			},
		},
		{
			name: "template fills empty container resources",
			templateRequests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			},
			templateLimits: nil,
			containerInit:  corev1.ResourceRequirements{},
			expectedRequests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			},
			expectedLimits: nil,
		},
		{
			name:             "empty template leaves container resources unchanged",
			templateRequests: nil,
			templateLimits:   nil,
			containerInit: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("250m"),
				},
			},
			expectedRequests: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("250m"),
			},
			expectedLimits: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ct := &ContainerTemplate{
				Resources: corev1.ResourceRequirements{
					Requests: tt.templateRequests,
					Limits:   tt.templateLimits,
				},
			}
			c := &corev1.Container{Resources: tt.containerInit}

			ct.ApplyToContainer(c)

			assert.Equal(t, tt.expectedRequests, c.Resources.Requests)
			assert.Equal(t, tt.expectedLimits, c.Resources.Limits)
		})
	}
}

func TestApplyToContainer_ClaimsAppend(t *testing.T) {
	tests := []struct {
		name            string
		templateClaims  []corev1.ResourceClaim
		containerClaims []corev1.ResourceClaim
		expected        []corev1.ResourceClaim
	}{
		{
			name:            "template claim appended to container claims",
			templateClaims:  []corev1.ResourceClaim{{Name: "gpu"}},
			containerClaims: []corev1.ResourceClaim{{Name: "nic"}},
			expected:        []corev1.ResourceClaim{{Name: "nic"}, {Name: "gpu"}},
		},
		{
			name:            "empty template leaves container claims unchanged",
			templateClaims:  nil,
			containerClaims: []corev1.ResourceClaim{{Name: "nic"}},
			expected:        []corev1.ResourceClaim{{Name: "nic"}},
		},
		{
			name:            "template claim added to empty container",
			templateClaims:  []corev1.ResourceClaim{{Name: "gpu"}},
			containerClaims: nil,
			expected:        []corev1.ResourceClaim{{Name: "gpu"}},
		},
		{
			name:            "duplicate name from template is dropped",
			templateClaims:  []corev1.ResourceClaim{{Name: "gpu"}},
			containerClaims: []corev1.ResourceClaim{{Name: "gpu"}},
			expected:        []corev1.ResourceClaim{{Name: "gpu"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ct := &ContainerTemplate{Resources: corev1.ResourceRequirements{Claims: tt.templateClaims}}
			c := &corev1.Container{Resources: corev1.ResourceRequirements{Claims: tt.containerClaims}}

			ct.ApplyToContainer(c)

			assert.Equal(t, tt.expected, c.Resources.Claims)
		})
	}
}

func TestApplyToContainer_SecurityContextUnconditionalReplace(t *testing.T) {
	runAsNonRoot := true
	templateCtx := &corev1.SecurityContext{
		RunAsNonRoot: &runAsNonRoot,
	}

	tests := []struct {
		name         string
		templateCtx  *corev1.SecurityContext
		containerCtx *corev1.SecurityContext
		expected     *corev1.SecurityContext
	}{
		{
			name:         "template replaces existing container context",
			templateCtx:  templateCtx,
			containerCtx: &corev1.SecurityContext{RunAsUser: ptr.To[int64](0)},
			expected:     templateCtx,
		},
		{
			name:         "template sets context on empty container",
			templateCtx:  templateCtx,
			containerCtx: nil,
			expected:     templateCtx,
		},
		{
			name:         "nil template clears existing container context",
			templateCtx:  nil,
			containerCtx: &corev1.SecurityContext{RunAsUser: ptr.To[int64](1001)},
			expected:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ct := &ContainerTemplate{SecurityContext: tt.templateCtx}
			c := &corev1.Container{SecurityContext: tt.containerCtx}

			ct.ApplyToContainer(c)

			assert.Equal(t, tt.expected, c.SecurityContext)
		})
	}
}

func TestApplyToContainer_ImagePullPolicyOnlyWhenEmpty(t *testing.T) {
	tests := []struct {
		name           string
		templatePolicy corev1.PullPolicy
		containerInit  corev1.PullPolicy
		expected       corev1.PullPolicy
	}{
		{
			name:           "template fills empty policy",
			templatePolicy: corev1.PullIfNotPresent,
			containerInit:  "",
			expected:       corev1.PullIfNotPresent,
		},
		{
			name:           "container policy is preserved",
			templatePolicy: corev1.PullAlways,
			containerInit:  corev1.PullNever,
			expected:       corev1.PullNever,
		},
		{
			name:           "both empty stays empty",
			templatePolicy: "",
			containerInit:  "",
			expected:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ct := &ContainerTemplate{ImagePullPolicy: tt.templatePolicy}
			c := &corev1.Container{ImagePullPolicy: tt.containerInit}

			ct.ApplyToContainer(c)

			assert.Equal(t, tt.expected, c.ImagePullPolicy)
		})
	}
}

func TestApplyToContainer_EnvAppend(t *testing.T) {
	tests := []struct {
		name         string
		templateEnv  []corev1.EnvVar
		containerEnv []corev1.EnvVar
		expected     []corev1.EnvVar
	}{
		{
			name:         "template env appended to container env",
			templateEnv:  []corev1.EnvVar{{Name: "T1", Value: "tv1"}},
			containerEnv: []corev1.EnvVar{{Name: "C1", Value: "cv1"}},
			expected: []corev1.EnvVar{
				{Name: "C1", Value: "cv1"},
				{Name: "T1", Value: "tv1"},
			},
		},
		{
			name:         "empty template leaves container env unchanged",
			templateEnv:  nil,
			containerEnv: []corev1.EnvVar{{Name: "C1", Value: "cv1"}},
			expected:     []corev1.EnvVar{{Name: "C1", Value: "cv1"}},
		},
		{
			name:         "template env added to empty container",
			templateEnv:  []corev1.EnvVar{{Name: "T1", Value: "tv1"}},
			containerEnv: nil,
			expected:     []corev1.EnvVar{{Name: "T1", Value: "tv1"}},
		},
		{
			name: "duplicate names are appended (no dedup)",
			templateEnv: []corev1.EnvVar{
				{Name: "SHARED", Value: "from-template"},
			},
			containerEnv: []corev1.EnvVar{
				{Name: "SHARED", Value: "from-container"},
			},
			expected: []corev1.EnvVar{
				{Name: "SHARED", Value: "from-container"},
				{Name: "SHARED", Value: "from-template"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ct := &ContainerTemplate{Env: tt.templateEnv}
			c := &corev1.Container{Env: tt.containerEnv}

			ct.ApplyToContainer(c)

			assert.Equal(t, tt.expected, c.Env)
		})
	}
}

func TestApplyToContainer_EnvFromAppend(t *testing.T) {
	tests := []struct {
		name             string
		templateEnvFrom  []corev1.EnvFromSource
		containerEnvFrom []corev1.EnvFromSource
		expected         []corev1.EnvFromSource
	}{
		{
			name: "template envFrom appended to container envFrom",
			templateEnvFrom: []corev1.EnvFromSource{
				{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "template-cm"}}},
			},
			containerEnvFrom: []corev1.EnvFromSource{
				{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "container-secret"}}},
			},
			expected: []corev1.EnvFromSource{
				{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "container-secret"}}},
				{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "template-cm"}}},
			},
		},
		{
			name:            "empty template leaves container envFrom unchanged",
			templateEnvFrom: nil,
			containerEnvFrom: []corev1.EnvFromSource{
				{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "s"}}},
			},
			expected: []corev1.EnvFromSource{
				{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "s"}}},
			},
		},
		{
			name: "template envFrom added to empty container",
			templateEnvFrom: []corev1.EnvFromSource{
				{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "cm"}}},
			},
			containerEnvFrom: nil,
			expected: []corev1.EnvFromSource{
				{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "cm"}}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ct := &ContainerTemplate{EnvFrom: tt.templateEnvFrom}
			c := &corev1.Container{EnvFrom: tt.containerEnvFrom}

			ct.ApplyToContainer(c)

			assert.Equal(t, tt.expected, c.EnvFrom)
		})
	}
}

func TestApplyToContainer_FullTemplate(t *testing.T) {
	runAsUser := int64(1001)
	template := &ContainerTemplate{
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("1"),
			},
		},
		ImagePullPolicy: corev1.PullIfNotPresent,
		SecurityContext: &corev1.SecurityContext{RunAsUser: &runAsUser},
		Env: []corev1.EnvVar{
			{Name: "LOG_LEVEL", Value: "debug"},
		},
		EnvFrom: []corev1.EnvFromSource{
			{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "app-config"}}},
		},
	}

	c := &corev1.Container{
		Name: "agent",
		Env:  []corev1.EnvVar{{Name: "EXISTING", Value: "preserved"}},
	}

	template.ApplyToContainer(c)

	assert.Equal(t, "agent", c.Name)
	assert.Equal(t, template.Resources.Requests, c.Resources.Requests)
	assert.Equal(t, template.Resources.Limits, c.Resources.Limits)
	assert.Equal(t, corev1.PullIfNotPresent, c.ImagePullPolicy)
	assert.Equal(t, template.SecurityContext, c.SecurityContext)
	assert.Equal(t, []corev1.EnvVar{
		{Name: "EXISTING", Value: "preserved"},
		{Name: "LOG_LEVEL", Value: "debug"},
	}, c.Env)
	assert.Equal(t, template.EnvFrom, c.EnvFrom)
}

func TestContainerTemplate_ApplyDefaultsFrom(t *testing.T) {
	t.Run("nil defaults is a no-op", func(t *testing.T) {
		own := &ContainerTemplate{ImagePullPolicy: corev1.PullAlways}
		own.ApplyDefaultsFrom(nil)
		assert.Equal(t, corev1.PullAlways, own.ImagePullPolicy)
	})

	t.Run("fills unset fields from defaults", func(t *testing.T) {
		own := &ContainerTemplate{}
		defaults := &ContainerTemplate{
			ImagePullPolicy: corev1.PullAlways,
			SecurityContext: &corev1.SecurityContext{RunAsUser: ptr.To[int64](1000)},
			Env:             []corev1.EnvVar{{Name: "FOO", Value: "tmpl"}},
			EnvFrom:         []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "cm"}}}},
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("500Mi")},
				Claims: []corev1.ResourceClaim{{Name: "gpu"}},
			},
		}
		own.ApplyDefaultsFrom(defaults)
		assert.Equal(t, corev1.PullAlways, own.ImagePullPolicy)
		assert.NotNil(t, own.SecurityContext)
		assert.Equal(t, "tmpl", own.Env[0].Value)
		assert.Len(t, own.EnvFrom, 1)
		assert.Equal(t, "500Mi", own.Resources.Limits.Memory().String())
		assert.Equal(t, []corev1.ResourceClaim{{Name: "gpu"}}, own.Resources.Claims)
	})

	t.Run("own values win; resources and env merge", func(t *testing.T) {
		own := &ContainerTemplate{
			ImagePullPolicy: corev1.PullNever,
			Env:             []corev1.EnvVar{{Name: "FOO", Value: "own"}},
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
			},
		}
		defaults := &ContainerTemplate{
			ImagePullPolicy: corev1.PullAlways,
			Env:             []corev1.EnvVar{{Name: "FOO", Value: "tmpl"}, {Name: "BAR", Value: "tmpl"}},
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("500Mi"),
				},
			},
		}
		own.ApplyDefaultsFrom(defaults)

		assert.Equal(t, corev1.PullNever, own.ImagePullPolicy, "own pull policy wins")
		// Env: FOO keeps own value, BAR merged in.
		got := map[string]string{}
		for _, e := range own.Env {
			got[e.Name] = e.Value
		}
		assert.Equal(t, "own", got["FOO"], "own env var wins on name collision")
		assert.Equal(t, "tmpl", got["BAR"], "defaults-only env var merged in")
		// Resources: own CPU wins, defaults memory merged in.
		assert.Equal(t, "2", own.Resources.Limits.Cpu().String(), "own resource key wins")
		assert.Equal(t, "500Mi", own.Resources.Limits.Memory().String(), "defaults-only resource merged in")
	})
}
