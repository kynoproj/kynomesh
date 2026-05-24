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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	"github.com/kynoproj/kynomesh/pkg/shared/logging"
)

func TestBuildScheme_RegistersCoreTypes(t *testing.T) {
	s := buildScheme(logging.NewLogger())
	require.NotNil(t, s)

	coreGVKs := []schema.GroupVersionKind{
		corev1.SchemeGroupVersion.WithKind("Pod"),
		corev1.SchemeGroupVersion.WithKind("Service"),
		corev1.SchemeGroupVersion.WithKind("ConfigMap"),
		corev1.SchemeGroupVersion.WithKind("Secret"),
	}
	for _, gvk := range coreGVKs {
		t.Run(gvk.Kind, func(t *testing.T) {
			assert.True(t, s.Recognizes(gvk), "expected scheme to recognize %s", gvk)
		})
	}
}

func TestBuildScheme_RegistersKynomeshTypes(t *testing.T) {
	s := buildScheme(logging.NewLogger())
	require.NotNil(t, s)

	kmGVKs := []schema.GroupVersionKind{
		kmv1.SchemeGroupVersion.WithKind("AgentSet"),
		kmv1.SchemeGroupVersion.WithKind("AgentSetList"),
		kmv1.SchemeGroupVersion.WithKind("AgentDeploy"),
		kmv1.SchemeGroupVersion.WithKind("AgentDeployList"),
	}
	for _, gvk := range kmGVKs {
		t.Run(gvk.Kind, func(t *testing.T) {
			assert.True(t, s.Recognizes(gvk), "expected scheme to recognize %s", gvk)
		})
	}
}

func TestBuildScheme_NewInstancePerCall(t *testing.T) {
	a := buildScheme(logging.NewLogger())
	b := buildScheme(logging.NewLogger())
	assert.NotSame(t, a, b, "each call should return a fresh *runtime.Scheme")
}

func TestBuildScheme_ObjectKindsResolvesKynomeshTypes(t *testing.T) {
	s := buildScheme(logging.NewLogger())

	tests := []struct {
		name string
		obj  runtime.Object
		gvk  schema.GroupVersionKind
	}{
		{
			name: "AgentSet",
			obj:  &kmv1.AgentSet{},
			gvk:  kmv1.SchemeGroupVersion.WithKind("AgentSet"),
		},
		{
			name: "AgentDeploy",
			obj:  &kmv1.AgentDeploy{},
			gvk:  kmv1.SchemeGroupVersion.WithKind("AgentDeploy"),
		},
		{
			name: "Pod",
			obj:  &corev1.Pod{},
			gvk:  corev1.SchemeGroupVersion.WithKind("Pod"),
		},
		{
			name: "Service",
			obj:  &corev1.Service{},
			gvk:  corev1.SchemeGroupVersion.WithKind("Service"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gvks, _, err := s.ObjectKinds(tc.obj)
			require.NoError(t, err)
			require.NotEmpty(t, gvks)
			assert.Contains(t, gvks, tc.gvk)
		})
	}
}

func TestPackageConstants(t *testing.T) {
	assert.Equal(t, "kynomesh-controller-lock", leaderElectionID)
	assert.Equal(t, ":9090", defaultMetricsAddr)
	assert.Equal(t, ":8081", defaultProbeAddr)
	assert.Equal(t, "KYNOMESH_LEADER_ELECTION_DISABLED", envLeaderElectionDisabled)
	assert.Equal(t, "KYNOMESH_METRICS_BIND_ADDRESS", envMetricsAddr)
	assert.Equal(t, "KYNOMESH_HEALTH_PROBE_BIND_ADDRESS", envProbeAddr)
}
