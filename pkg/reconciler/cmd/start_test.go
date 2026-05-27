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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	assert.Equal(t, ":9090", metricsAddr)
	assert.Equal(t, ":8081", probeAddr)
	// Defaults match client-go upstream so the controller behaves like
	// every other controller-runtime operator out of the box.
	assert.Equal(t, 15*time.Second, defaultLeaseDuration)
	assert.Equal(t, 10*time.Second, defaultRenewDeadline)
	assert.Equal(t, 2*time.Second, defaultRetryPeriod)
}

// clearLeaderElectionEnv unsets all three lease env vars for the duration
// of a test. Necessary because t.Setenv only handles set values, but the
// helper-under-test reads via os.LookupEnv — a value leaking from another
// test (or the host shell) would corrupt the defaults assertion.
func clearLeaderElectionEnv(t *testing.T) {
	t.Helper()
	t.Setenv(kmv1.EnvLeaderElectionLeaseDuration, "")
	t.Setenv(kmv1.EnvLeaderElectionRenewDeadline, "")
	t.Setenv(kmv1.EnvLeaderElectionRetryPeriod, "")
}

func TestResolveLeaderElectionTimings_Defaults(t *testing.T) {
	clearLeaderElectionEnv(t)

	lease, renew, retry, err := resolveLeaderElectionTimings()
	require.NoError(t, err)
	assert.Equal(t, defaultLeaseDuration, lease)
	assert.Equal(t, defaultRenewDeadline, renew)
	assert.Equal(t, defaultRetryPeriod, retry)
}

func TestResolveLeaderElectionTimings_HonorsEnvOverrides(t *testing.T) {
	t.Setenv(kmv1.EnvLeaderElectionLeaseDuration, "30s")
	t.Setenv(kmv1.EnvLeaderElectionRenewDeadline, "20s")
	t.Setenv(kmv1.EnvLeaderElectionRetryPeriod, "5s")

	lease, renew, retry, err := resolveLeaderElectionTimings()
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, lease)
	assert.Equal(t, 20*time.Second, renew)
	assert.Equal(t, 5*time.Second, retry)
}

func TestResolveLeaderElectionTimings_RejectsUnparseable(t *testing.T) {
	cases := []struct {
		name   string
		envKey string
	}{
		{"lease duration", kmv1.EnvLeaderElectionLeaseDuration},
		{"renew deadline", kmv1.EnvLeaderElectionRenewDeadline},
		{"retry period", kmv1.EnvLeaderElectionRetryPeriod},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearLeaderElectionEnv(t)
			t.Setenv(tc.envKey, "not-a-duration")

			_, _, _, err := resolveLeaderElectionTimings()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.envKey, "error must name the offending env var")
		})
	}
}

func TestResolveLeaderElectionTimings_RejectsInvalidOrdering(t *testing.T) {
	// controller-runtime requires retry < renew < lease. The helper
	// should reject any input that violates the invariant before the
	// manager even sees it.
	cases := []struct {
		name   string
		lease  string
		renew  string
		retry  string
		errSub string
	}{
		{
			name:   "retry equals renew",
			lease:  "15s",
			renew:  "10s",
			retry:  "10s",
			errSub: "retry < renew < lease",
		},
		{
			name:   "renew equals lease",
			lease:  "10s",
			renew:  "10s",
			retry:  "2s",
			errSub: "retry < renew < lease",
		},
		{
			name:   "lease shorter than renew",
			lease:  "5s",
			renew:  "10s",
			retry:  "2s",
			errSub: "retry < renew < lease",
		},
		{
			name:   "retry larger than renew",
			lease:  "15s",
			renew:  "10s",
			retry:  "12s",
			errSub: "retry < renew < lease",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(kmv1.EnvLeaderElectionLeaseDuration, tc.lease)
			t.Setenv(kmv1.EnvLeaderElectionRenewDeadline, tc.renew)
			t.Setenv(kmv1.EnvLeaderElectionRetryPeriod, tc.retry)

			_, _, _, err := resolveLeaderElectionTimings()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errSub)
		})
	}
}

func TestControllerImageFromPod(t *testing.T) {
	cases := []struct {
		name      string
		pod       *corev1.Pod
		want      string
		wantErr   bool
		errSubstr string
	}{
		{
			name: "single container — use it",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "p"},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "anything", Image: "img:v1"}},
				},
			},
			want: "img:v1",
		},
		{
			name: "multi container — match by name",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "p"},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "sidecar", Image: "side:v1"},
						{Name: kmv1.ContainerNameController, Image: "ctrl:v1"},
					},
				},
			},
			want: "ctrl:v1",
		},
		{
			name: "no containers — error",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "p"},
			},
			wantErr:   true,
			errSubstr: "no containers",
		},
		{
			name: "multi container without controller-manager — error",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "p"},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "a", Image: "a:v1"},
						{Name: "b", Image: "b:v1"},
					},
				},
			},
			wantErr:   true,
			errSubstr: "no container named",
		},
		{
			name: "single container with empty image — error",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "p"},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "a"}},
				},
			},
			wantErr:   true,
			errSubstr: "empty image",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := controllerImageFromPod(tc.pod)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errSubstr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
