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
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootCmd_Metadata(t *testing.T) {
	assert.Equal(t, "kynomesh", rootCmd.Use)
	assert.Equal(t, "Kynomesh CLI", rootCmd.Short)
	assert.NotNil(t, rootCmd.Run, "root command should have a Run that prints help")
}

func TestRootCmd_HasControllerSubcommand(t *testing.T) {
	var found bool
	for _, sub := range rootCmd.Commands() {
		if sub.Use == "controller" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected 'controller' subcommand to be registered on rootCmd")
}

func TestRootCmd_HasBrokerSubcommand(t *testing.T) {
	var found bool
	for _, sub := range rootCmd.Commands() {
		if sub.Use == "broker" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected 'broker' subcommand to be registered on rootCmd")
}

func TestRootCmd_HasDrainSubcommand(t *testing.T) {
	var found bool
	for _, sub := range rootCmd.Commands() {
		if sub.Use == "drain" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected 'drain' subcommand to be registered on rootCmd")
}

func TestRootCmd_HasWebhookSubcommand(t *testing.T) {
	var found bool
	for _, sub := range rootCmd.Commands() {
		if sub.Use == "webhook" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected 'webhook' subcommand to be registered on rootCmd")
}

func TestNewWebhookCommand_Metadata(t *testing.T) {
	c := NewWebhookCommand()
	require.NotNil(t, c)
	assert.Equal(t, "webhook", c.Use)
	assert.Equal(t, "Start the kynomesh validating admission webhook", c.Short)
	assert.NotNil(t, c.Run, "webhook command should have a Run handler")
}

func TestNewWebhookCommand_ReturnsFreshInstance(t *testing.T) {
	a := NewWebhookCommand()
	b := NewWebhookCommand()
	require.NotNil(t, a)
	require.NotNil(t, b)
	assert.NotSame(t, a, b, "each call should return a fresh *cobra.Command")
}

func TestNewDrainCommand_Metadata(t *testing.T) {
	c := NewDrainCommand()
	require.NotNil(t, c)
	assert.Equal(t, "drain", c.Use)
	assert.NotNil(t, c.Run, "drain command should have a Run handler")
	require.NotNil(t, c.Flags().Lookup("introspection-port"), "expected --introspection-port flag")
}

func TestRootCmd_RunPrintsHelp(t *testing.T) {
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})

	rootCmd.Run(rootCmd, []string{})

	got := out.String()
	assert.Contains(t, got, "kynomesh")
	assert.Contains(t, got, "controller", "help output should list the controller subcommand")
}

func TestNewControllerCommand_Metadata(t *testing.T) {
	c := NewControllerCommand()
	require.NotNil(t, c)
	assert.Equal(t, "controller", c.Use)
	assert.Equal(t, "Start a kynomesh controller", c.Short)
	assert.NotNil(t, c.Run, "controller command should have a Run handler")
}

func TestNewControllerCommand_Flags(t *testing.T) {
	c := NewControllerCommand()

	nsFlag := c.Flags().Lookup("namespaced")
	require.NotNil(t, nsFlag, "expected --namespaced flag")
	assert.Equal(t, "bool", nsFlag.Value.Type())

	mnsFlag := c.Flags().Lookup("managed-namespace")
	require.NotNil(t, mnsFlag, "expected --managed-namespace flag")
	assert.Equal(t, "string", mnsFlag.Value.Type())
}

func TestNewControllerCommand_FlagDefaults_NoEnv(t *testing.T) {
	t.Setenv("KYNOMESH_CONTROLLER_NAMESPACED", "")
	t.Setenv("KYNOMESH_CONTROLLER_MANAGED_NAMESPACE", "")
	t.Setenv("NAMESPACE", "")

	c := NewControllerCommand()

	assert.Equal(t, "false", c.Flags().Lookup("namespaced").DefValue)
	assert.Equal(t, "kynomesh-system", c.Flags().Lookup("managed-namespace").DefValue)
}

func TestNewControllerCommand_FlagDefaults_FromEnv(t *testing.T) {
	t.Setenv("KYNOMESH_CONTROLLER_NAMESPACED", "true")
	t.Setenv("KYNOMESH_CONTROLLER_MANAGED_NAMESPACE", "custom-ns")

	c := NewControllerCommand()

	assert.Equal(t, "true", c.Flags().Lookup("namespaced").DefValue)
	assert.Equal(t, "custom-ns", c.Flags().Lookup("managed-namespace").DefValue)
}

func TestNewControllerCommand_FlagDefaults_NamespaceFallback(t *testing.T) {
	// KYNOMESH_CONTROLLER_MANAGED_NAMESPACE unset → fall back to NAMESPACE,
	// which is the conventional downward-API key.
	t.Setenv("KYNOMESH_CONTROLLER_MANAGED_NAMESPACE", "")
	t.Setenv("NAMESPACE", "pod-ns")

	c := NewControllerCommand()

	assert.Equal(t, "pod-ns", c.Flags().Lookup("managed-namespace").DefValue)
}

func TestNewControllerCommand_FlagDefaults_PrecedenceKynomeshOverNAMESPACE(t *testing.T) {
	// When both are set, the kynomesh-specific var wins.
	t.Setenv("KYNOMESH_CONTROLLER_MANAGED_NAMESPACE", "specific-ns")
	t.Setenv("NAMESPACE", "generic-ns")

	c := NewControllerCommand()

	assert.Equal(t, "specific-ns", c.Flags().Lookup("managed-namespace").DefValue)
}

func TestNewControllerCommand_ReturnsFreshInstance(t *testing.T) {
	a := NewControllerCommand()
	b := NewControllerCommand()
	require.NotNil(t, a)
	require.NotNil(t, b)
	assert.NotSame(t, a, b, "each call should return a fresh *cobra.Command so flag state is not shared")
}

func TestNewControllerCommand_FlagOverridesEnvDefault(t *testing.T) {
	t.Setenv("KYNOMESH_CONTROLLER_MANAGED_NAMESPACE", "from-env")

	c := NewControllerCommand()
	require.NoError(t, c.Flags().Set("managed-namespace", "from-flag"))

	assert.Equal(t, "from-flag", c.Flags().Lookup("managed-namespace").Value.String())
	// DefValue still reflects the env-sourced default — only the live Value changed.
	assert.Equal(t, "from-env", c.Flags().Lookup("managed-namespace").DefValue)
}
