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
	"os"
	"path/filepath"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const metricNameAgentServerInfo = "kynomesh_broker_agent_server_info"

func withServerInfoFile(t *testing.T, path string) {
	t.Helper()
	prev := serverInfoFilePath
	serverInfoFilePath = path
	t.Cleanup(func() { serverInfoFilePath = prev })
}

func hasMetric(t *testing.T, registry *prometheus.Registry, name string) bool {
	t.Helper()
	mfs, err := registry.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() == name {
			return true
		}
	}
	return false
}

func TestPublishAgentServerInfo_RegistersGaugeInCluster(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server-info")
	require.NoError(t, os.WriteFile(path, []byte(`{"protocol":"uds","language":"go","version":"1.0.0"}`), 0o644))
	withServerInfoFile(t, path)
	withInClusterMode(t, true)

	registry := prometheus.NewRegistry()
	publishAgentServerInfo(zap.NewNop().Sugar(), registry)

	assert.True(t, hasMetric(t, registry, metricNameAgentServerInfo), "in-cluster + file present: gauge must be registered")
}

func TestPublishAgentServerInfo_SkipsOutOfCluster(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server-info")
	require.NoError(t, os.WriteFile(path, []byte(`{"protocol":"uds","language":"go","version":"1.0.0"}`), 0o644))
	withServerInfoFile(t, path)
	withInClusterMode(t, false)

	registry := prometheus.NewRegistry()
	publishAgentServerInfo(zap.NewNop().Sugar(), registry)

	assert.False(t, hasMetric(t, registry, metricNameAgentServerInfo), "out-of-cluster: must not touch the registry")
}

func TestPublishAgentServerInfo_TolerantOfMissingFile(t *testing.T) {
	withServerInfoFile(t, filepath.Join(t.TempDir(), "absent"))
	withInClusterMode(t, true)

	registry := prometheus.NewRegistry()
	publishAgentServerInfo(zap.NewNop().Sugar(), registry)

	assert.False(t, hasMetric(t, registry, metricNameAgentServerInfo), "missing server-info must not register a gauge")
}
