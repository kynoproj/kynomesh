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
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

func TestNewBrokerCommand_Metadata(t *testing.T) {
	c := NewBrokerCommand()
	require.NotNil(t, c)
	assert.Equal(t, "broker", c.Use)
	assert.NotEmpty(t, c.Short)
	assert.NotEmpty(t, c.Long)
	assert.NotNil(t, c.Run, "broker command should have a Run handler")
}

func TestNewBrokerCommand_Flags(t *testing.T) {
	c := NewBrokerCommand()

	pf := c.Flags().Lookup("port")
	require.NotNil(t, pf, "expected --port flag")
	assert.Equal(t, "int", pf.Value.Type())

	hf := c.Flags().Lookup("advertise-host")
	require.NotNil(t, hf, "expected --advertise-host flag")
	assert.Equal(t, "string", hf.Value.Type())
}

func TestNewBrokerCommand_FlagDefaults_NoEnv(t *testing.T) {
	t.Setenv("KYNOMESH_BROKER_PORT", "")
	t.Setenv("KYNOMESH_BROKER_ADVERTISE_HOST", "")

	c := NewBrokerCommand()

	assert.Equal(t, strconv.Itoa(kmv1.AgentBrokerPort), c.Flags().Lookup("port").DefValue)
	assert.Equal(t, "127.0.0.1", c.Flags().Lookup("advertise-host").DefValue)
}

func TestNewBrokerCommand_FlagDefaults_FromEnv(t *testing.T) {
	t.Setenv("KYNOMESH_BROKER_PORT", "8080")
	t.Setenv("KYNOMESH_BROKER_ADVERTISE_HOST", "broker.cluster.local")

	c := NewBrokerCommand()

	assert.Equal(t, "8080", c.Flags().Lookup("port").DefValue)
	assert.Equal(t, "broker.cluster.local", c.Flags().Lookup("advertise-host").DefValue)
}

func TestNewBrokerCommand_ReturnsFreshInstance(t *testing.T) {
	a := NewBrokerCommand()
	b := NewBrokerCommand()
	require.NotNil(t, a)
	require.NotNil(t, b)
	assert.NotSame(t, a, b, "each call should return a fresh *cobra.Command")
}

func TestNewBrokerCommand_FlagOverridesEnvDefault(t *testing.T) {
	t.Setenv("KYNOMESH_BROKER_PORT", "9999")

	c := NewBrokerCommand()
	require.NoError(t, c.Flags().Set("port", "1234"))

	assert.Equal(t, "1234", c.Flags().Lookup("port").Value.String())
	assert.Equal(t, "9999", c.Flags().Lookup("port").DefValue)
}
