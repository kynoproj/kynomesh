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

package serverinfo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server-info")
	payload := `{"protocol":"uds","language":"python","version":"1.2.3","metadata":{"sdk":"a2a-py"}}`
	require.NoError(t, os.WriteFile(path, []byte(payload), 0o644))

	info, err := Load(path)
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, UDS, info.Protocol)
	assert.Equal(t, Python, info.Language)
	assert.Equal(t, "1.2.3", info.Version)
	assert.Equal(t, "a2a-py", info.Metadata["sdk"])
}

func TestLoad_MissingFileReturnsNil(t *testing.T) {
	info, err := Load(filepath.Join(t.TempDir(), "absent"))
	require.NoError(t, err)
	assert.Nil(t, info)
}

func TestLoad_InvalidJSONErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server-info")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0o644))

	info, err := Load(path)
	require.Error(t, err)
	assert.Nil(t, info)
	assert.Contains(t, err.Error(), "decode")
}

func TestRegisterMetric_SetsLabelsAndValue(t *testing.T) {
	registry := prometheus.NewRegistry()
	RegisterMetric(registry, ServerInfo{Protocol: UDS, Language: Go, Version: "0.4.2"})

	expected := `
# HELP broker_agent_server_info Static info about the agent server colocated with this broker. Always 1; labels carry the values.
# TYPE broker_agent_server_info gauge
broker_agent_server_info{language="go",protocol="uds",version="0.4.2"} 1
`
	require.NoError(t, testutil.GatherAndCompare(registry, strings.NewReader(expected), "broker_agent_server_info"))
}
