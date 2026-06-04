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

package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

func testLogger() *zap.SugaredLogger { return zap.NewNop().Sugar() }

func TestWriteTopology_PopulatesManagedURLs(t *testing.T) {
	ad := &kmv1.AgentDeploy{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-greeter", Namespace: "ns"},
		Spec: kmv1.AgentDeploySpec{
			AbstractAgentDeploy: kmv1.AbstractAgentDeploy{Name: "greeter"},
			AgentSetName:        "demo",
			Topology: kmv1.Topology{
				Pattern: kmv1.AgentPatternSupervisor,
				IsEntry: true,
				Peers: []kmv1.Peer{
					{Name: "worker1", Kind: kmv1.PeerKindManaged},
					{Name: "worker2", Kind: kmv1.PeerKindManaged},
				},
			},
		},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "topology.json")
	require.NoError(t, writeTopology(testLogger(), path, kmv1.EncodeAgentDeploy(ad)))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var got kmv1.Topology
	require.NoError(t, json.Unmarshal(raw, &got))

	require.Len(t, got.Peers, 2)
	assert.Equal(t, "worker1", got.Peers[0].Name)
	assert.Equal(t, "https://demo-worker1-headless.ns.svc.cluster.local:8490", got.Peers[0].URL)
	assert.Equal(t, "worker2", got.Peers[1].Name)
	assert.Equal(t, "https://demo-worker2-headless.ns.svc.cluster.local:8490", got.Peers[1].URL)
}

func TestWriteTopology_PreservesExternalURLs(t *testing.T) {
	ad := &kmv1.AgentDeploy{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-greeter", Namespace: "ns"},
		Spec: kmv1.AgentDeploySpec{
			AbstractAgentDeploy: kmv1.AbstractAgentDeploy{Name: "greeter"},
			AgentSetName:        "demo",
			Topology: kmv1.Topology{
				Pattern: kmv1.AgentPatternHandoff,
				Peers: []kmv1.Peer{
					{Name: "inside", Kind: kmv1.PeerKindManaged},
					{Name: "outside", Kind: kmv1.PeerKindExternal, URL: "https://example.com/agent"},
				},
			},
		},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "topology.json")
	require.NoError(t, writeTopology(testLogger(), path, kmv1.EncodeAgentDeploy(ad)))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var got kmv1.Topology
	require.NoError(t, json.Unmarshal(raw, &got))

	require.Len(t, got.Peers, 2)
	assert.Equal(t, "https://demo-inside-headless.ns.svc.cluster.local:8490", got.Peers[0].URL)
	assert.Equal(t, "https://example.com/agent", got.Peers[1].URL, "external peers must keep their user-supplied URL")
}

func TestWriteTopology_AtomicReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "topology.json")
	require.NoError(t, os.WriteFile(path, []byte("stale"), 0o644))

	ad := &kmv1.AgentDeploy{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-worker", Namespace: "ns"},
		Spec: kmv1.AgentDeploySpec{
			AbstractAgentDeploy: kmv1.AbstractAgentDeploy{Name: "worker"},
			AgentSetName:        "demo",
			Topology:            kmv1.Topology{Pattern: kmv1.AgentPatternHandoff},
		},
	}
	require.NoError(t, writeTopology(testLogger(), path, kmv1.EncodeAgentDeploy(ad)))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var got kmv1.Topology
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, kmv1.AgentPatternHandoff, got.Pattern)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "atomic replace must not leave temp files behind")
}

func TestWriteTopology_ErrorsOnEmptyPayload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "topology.json")

	err := writeTopology(testLogger(), path, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode AgentDeploy")
}

func TestInstallProbeBinary_CopiesAndMarksExecutable(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "probe")
	require.NoError(t, os.WriteFile(src, []byte("PROBE-BIN"), 0o644))

	dst := filepath.Join(dir, "bin", "probe")
	require.NoError(t, installProbeBinary(testLogger(), src, dst))

	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "PROBE-BIN", string(got))

	info, err := os.Stat(dst)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
}

func TestInstallProbeBinary_AtomicReplace(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "probe")
	require.NoError(t, os.WriteFile(src, []byte("NEW"), 0o755))

	dst := filepath.Join(dir, "out", "probe")
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(dst, []byte("OLD"), 0o755))

	require.NoError(t, installProbeBinary(testLogger(), src, dst))

	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "NEW", string(got))

	entries, err := os.ReadDir(filepath.Dir(dst))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "atomic replace must not leave temp files behind")
}

func TestInstallProbeBinary_ErrorsWhenSrcMissing(t *testing.T) {
	dir := t.TempDir()
	err := installProbeBinary(testLogger(), filepath.Join(dir, "missing"), filepath.Join(dir, "out", "probe"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open probe binary")
}
