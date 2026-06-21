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

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

func TestLoadConfig_HappyPath(t *testing.T) {
	t.Setenv(kmv1.EnvNamespace, "default")
	t.Setenv(kmv1.EnvAgentSetName, "my-set")
	t.Setenv(kmv1.EnvAgentSetAgentDeploys, `["greeter","summarizer"]`)

	cfg, err := loadConfig(9432, 9433)
	require.NoError(t, err)
	assert.Equal(t, "default", cfg.Namespace)
	assert.Equal(t, "my-set", cfg.AgentSet)
	assert.Equal(t, []string{"greeter", "summarizer"}, cfg.AgentDeploys)
	assert.Equal(t, 9432, cfg.APIPort)
	assert.Equal(t, 9433, cfg.MetricsPort)
}

func TestLoadConfig_MissingNamespace(t *testing.T) {
	t.Setenv(kmv1.EnvAgentSetName, "x")
	t.Setenv(kmv1.EnvAgentSetAgentDeploys, `["a"]`)
	_, err := loadConfig(9432, 9433)
	require.Error(t, err)
	assert.Contains(t, err.Error(), kmv1.EnvNamespace)
}

func TestLoadConfig_MissingAgentSet(t *testing.T) {
	t.Setenv(kmv1.EnvNamespace, "default")
	t.Setenv(kmv1.EnvAgentSetAgentDeploys, `["a"]`)
	_, err := loadConfig(9432, 9433)
	require.Error(t, err)
	assert.Contains(t, err.Error(), kmv1.EnvAgentSetName)
}

func TestLoadConfig_MissingAgentDeploys(t *testing.T) {
	t.Setenv(kmv1.EnvNamespace, "default")
	t.Setenv(kmv1.EnvAgentSetName, "x")
	_, err := loadConfig(9432, 9433)
	require.Error(t, err)
	assert.Contains(t, err.Error(), kmv1.EnvAgentSetAgentDeploys)
}

func TestLoadConfig_MalformedAgentDeploysJSON(t *testing.T) {
	t.Setenv(kmv1.EnvNamespace, "default")
	t.Setenv(kmv1.EnvAgentSetName, "x")
	t.Setenv(kmv1.EnvAgentSetAgentDeploys, `not-json`)
	_, err := loadConfig(9432, 9433)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}

func TestLoadConfig_EmptyAgentDeploysList(t *testing.T) {
	t.Setenv(kmv1.EnvNamespace, "default")
	t.Setenv(kmv1.EnvAgentSetName, "x")
	t.Setenv(kmv1.EnvAgentSetAgentDeploys, `[]`)
	_, err := loadConfig(9432, 9433)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one")
}
