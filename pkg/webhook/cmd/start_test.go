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
	"crypto/tls"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveOptions(t *testing.T) {
	t.Run("errors when NAMESPACE is unset", func(t *testing.T) {
		_, err := resolveOptions()
		require.Error(t, err)
		assert.Contains(t, err.Error(), namespaceEnvVar)
	})

	t.Run("applies defaults when only NAMESPACE is set", func(t *testing.T) {
		t.Setenv(namespaceEnvVar, "kynomesh-system")

		opts, err := resolveOptions()
		require.NoError(t, err)
		assert.Equal(t, "kynomesh-system", opts.Namespace)
		assert.Equal(t, "kynomesh-webhook", opts.ServiceName)
		assert.Equal(t, "kynomesh-webhook", opts.DeploymentName)
		assert.Equal(t, "kynomesh-webhook", opts.ClusterRoleName)
		assert.Equal(t, "kynomesh-webhook-certs", opts.SecretName)
		assert.Equal(t, "webhook.kynomesh.kyno.sh", opts.WebhookName)
		assert.Equal(t, 443, opts.Port)
		assert.Equal(t, tls.VerifyClientCertIfGiven, opts.ClientAuth)
	})

	t.Run("honors overrides for service, deployment, cluster role, and port", func(t *testing.T) {
		t.Setenv(namespaceEnvVar, "kynomesh-system")
		t.Setenv(serviceNameEnvVar, "custom-svc")
		t.Setenv(deploymentNameEnvVar, "custom-deploy")
		t.Setenv(clusterRoleNameEnvVar, "custom-role")
		t.Setenv(portEnvVar, "8443")

		opts, err := resolveOptions()
		require.NoError(t, err)
		assert.Equal(t, "custom-svc", opts.ServiceName)
		assert.Equal(t, "custom-deploy", opts.DeploymentName)
		assert.Equal(t, "custom-role", opts.ClusterRoleName)
		assert.Equal(t, 8443, opts.Port)
	})

	t.Run("errors on a non-numeric port", func(t *testing.T) {
		t.Setenv(namespaceEnvVar, "kynomesh-system")
		t.Setenv(portEnvVar, "not-a-number")

		_, err := resolveOptions()
		require.Error(t, err)
		assert.Contains(t, err.Error(), portEnvVar)
	})
}
