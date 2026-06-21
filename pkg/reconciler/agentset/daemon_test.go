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

package agentset

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

func TestNewDaemonDeployment_Shape(t *testing.T) {
	as := newAgentSet("hello", "alpha", "beta")
	r, _ := newTestReconciler(t)

	dep, err := r.newDaemonDeployment(as)
	require.NoError(t, err)

	assert.Equal(t, "hello-daemon", dep.Name)
	assert.Equal(t, testNamespace, dep.Namespace)

	// Strict singleton: replicas=1, Recreate strategy, owner ref.
	require.NotNil(t, dep.Spec.Replicas)
	assert.Equal(t, int32(1), *dep.Spec.Replicas)
	assert.Equal(t, appsv1.RecreateDeploymentStrategyType, dep.Spec.Strategy.Type)
	require.Len(t, dep.OwnerReferences, 1)
	assert.Equal(t, "hello", dep.OwnerReferences[0].Name)
	assert.True(t, *dep.OwnerReferences[0].Controller)

	// Labels + selector use the daemon component and AgentSet name.
	assert.Equal(t, kmv1.ComponentDaemon, dep.Labels[kmv1.KeyComponent])
	assert.Equal(t, "hello", dep.Labels[kmv1.KeyAgentSetName])
	assert.Equal(t, dep.Spec.Template.Labels, dep.Spec.Selector.MatchLabels)

	// Container shape: one container running `kynomesh daemon` with
	// the configured image and the two ports the daemon listens on.
	require.Len(t, dep.Spec.Template.Spec.Containers, 1)
	c := dep.Spec.Template.Spec.Containers[0]
	assert.Equal(t, kmv1.ContainerNameDaemon, c.Name)
	assert.Equal(t, "test-image:latest", c.Image)
	assert.Equal(t, []string{"daemon"}, c.Args)
	assert.Equal(t, corev1.PullIfNotPresent, c.ImagePullPolicy)

	portNames := map[string]int32{}
	for _, p := range c.Ports {
		portNames[p.Name] = p.ContainerPort
	}
	assert.Equal(t, int32(kmv1.DaemonAPIPort), portNames["api"])
	assert.Equal(t, int32(kmv1.DaemonMetricsPort), portNames["metrics"])

	// Probes hit /healthz on the metrics port over HTTPS.
	require.NotNil(t, c.ReadinessProbe)
	require.NotNil(t, c.LivenessProbe)
	for _, p := range []*corev1.Probe{c.ReadinessProbe, c.LivenessProbe} {
		require.NotNil(t, p.HTTPGet)
		assert.Equal(t, "/healthz", p.HTTPGet.Path)
		assert.Equal(t, corev1.URISchemeHTTPS, p.HTTPGet.Scheme)
		assert.Equal(t, "metrics", p.HTTPGet.Port.String())
	}
}

func TestNewDaemonDeployment_AgentDeploysEnvVar(t *testing.T) {
	as := newAgentSet("hello", "alpha", "beta")
	r, _ := newTestReconciler(t)

	dep, err := r.newDaemonDeployment(as)
	require.NoError(t, err)
	env := envMap(dep.Spec.Template.Spec.Containers[0].Env)

	// Namespace + pod name come from the downward API.
	assert.Equal(t, "metadata.namespace", env[kmv1.EnvNamespace].fieldRef)
	assert.Equal(t, "metadata.name", env[kmv1.EnvPodName].fieldRef)
	// AgentSet name passed verbatim.
	assert.Equal(t, "hello", env[kmv1.EnvAgentSetName].value)
	// AgentDeploys list is JSON, with names matching what AgentDeploy
	// reconciler will create (childName: "<set>-<agent>").
	var names []string
	require.NoError(t, json.Unmarshal([]byte(env[kmv1.EnvAgentSetAgentDeploys].value), &names))
	assert.Equal(t, []string{"hello-alpha", "hello-beta"}, names)
}

type envEntry struct {
	value    string
	fieldRef string
}

func envMap(env []corev1.EnvVar) map[string]envEntry {
	out := make(map[string]envEntry, len(env))
	for _, e := range env {
		entry := envEntry{value: e.Value}
		if e.ValueFrom != nil && e.ValueFrom.FieldRef != nil {
			entry.fieldRef = e.ValueFrom.FieldRef.FieldPath
		}
		out[e.Name] = entry
	}
	return out
}

func TestNewDaemonService_Shape(t *testing.T) {
	as := newAgentSet("hello", "alpha")
	r, _ := newTestReconciler(t)

	svc, err := r.newDaemonService(as)
	require.NoError(t, err)
	assert.Equal(t, "hello-daemon", svc.Name)
	assert.Equal(t, corev1.ServiceTypeClusterIP, svc.Spec.Type)
	require.Len(t, svc.OwnerReferences, 1)
	assert.Equal(t, "hello", svc.OwnerReferences[0].Name)

	ports := map[string]int32{}
	for _, p := range svc.Spec.Ports {
		ports[p.Name] = p.Port
	}
	assert.Equal(t, int32(kmv1.DaemonAPIPort), ports["api"])
	assert.Equal(t, int32(kmv1.DaemonMetricsPort), ports["metrics"])

	// Service selector must match the Deployment's pod labels.
	dep, err := r.newDaemonDeployment(as)
	require.NoError(t, err)
	assert.Equal(t, dep.Spec.Template.Labels, svc.Spec.Selector)
}

func TestReconcileDaemon_CreatesDeploymentAndService(t *testing.T) {
	as := newAgentSet("hello", "alpha")
	r, c := newTestReconciler(t, as)

	require.NoError(t, r.reconcileDaemon(context.Background(), as))

	var dep appsv1.Deployment
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "hello-daemon"}, &dep))
	assert.NotEmpty(t, dep.Annotations[kmv1.KeyHash], "hash annotation must be set on create")

	var svc corev1.Service
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "hello-daemon"}, &svc))
	assert.NotEmpty(t, svc.Annotations[kmv1.KeyHash])
}

func TestReconcileDaemon_NoopOnUnchanged(t *testing.T) {
	as := newAgentSet("hello", "alpha")
	r, c := newTestReconciler(t, as)

	require.NoError(t, r.reconcileDaemon(context.Background(), as))
	// Capture the live ResourceVersion; a subsequent no-op reconcile
	// must not bump it (the fake client increments on every write).
	var dep appsv1.Deployment
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "hello-daemon"}, &dep))
	rvBefore := dep.ResourceVersion

	require.NoError(t, r.reconcileDaemon(context.Background(), as))
	var depAfter appsv1.Deployment
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "hello-daemon"}, &depAfter))
	assert.Equal(t, rvBefore, depAfter.ResourceVersion, "no-op reconcile must not write to the Deployment")
}

func TestReconcileDaemon_RecreatesOnAgentDeploysChange(t *testing.T) {
	as := newAgentSet("hello", "alpha")
	r, c := newTestReconciler(t, as)

	require.NoError(t, r.reconcileDaemon(context.Background(), as))
	var dep0 appsv1.Deployment
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "hello-daemon"}, &dep0))
	hash0 := dep0.Annotations[kmv1.KeyHash]

	// Add a second agent — the AgentDeploys env var now differs,
	// hash changes, daemon Deployment gets recreated with the new env.
	as.Spec.Agents = append(as.Spec.Agents, kmv1.AbstractAgentDeploy{Name: "beta"})
	require.NoError(t, r.reconcileDaemon(context.Background(), as))
	var dep1 appsv1.Deployment
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "hello-daemon"}, &dep1))
	assert.NotEqual(t, hash0, dep1.Annotations[kmv1.KeyHash], "hash must change when AgentDeploys list changes")

	// The visible effect of recreation: the env var reflects the new
	// AgentDeploy list. (We can't easily assert the recreation event
	// itself against the fake client, but the env change proves the
	// Pod template was rewritten — which is what a real-cluster
	// Recreate strategy would translate to a Pod rollover.)
	env := envMap(dep1.Spec.Template.Spec.Containers[0].Env)
	assert.Contains(t, env[kmv1.EnvAgentSetAgentDeploys].value, "hello-beta",
		"new agent must appear in the AgentDeploys env after spec change")
}

// Sanity-check that the daemon name helper matches the wiring used
// throughout the reconciler.
func TestAgentSet_DaemonName(t *testing.T) {
	as := newAgentSet("my-set", "x")
	assert.Equal(t, "my-set-daemon", as.DaemonName())
}
