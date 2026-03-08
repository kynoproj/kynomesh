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

package util

import (
	"os"
	"path/filepath"
	"testing"

	"k8s.io/client-go/util/homedir"

	"github.com/stretchr/testify/assert"
)

func TestK8sRestConfig(t *testing.T) {
	t.Run("K8sRestConfig returns error when KUBECONFIG is invalid", func(t *testing.T) {
		// Setup the environment to simulate an invalid KUBECONFIG
		kubeconfig := "fake-kubeconfig"
		os.Setenv("KUBECONFIG", kubeconfig)
		defer os.Unsetenv("KUBECONFIG")

		config, err := K8sRestConfig()
		assert.NotNil(t, err)
		assert.Nil(t, config)
	})

}

func TestK8sRestConfig_blank(t *testing.T) {
	os.Unsetenv("KUBECONFIG")

	// Ensure the default kubeconfig does not exist
	homeDir := homedir.HomeDir()
	defaultKubeconfigPath := filepath.Join(homeDir, ".kube", "config")
	os.Remove(defaultKubeconfigPath)

	restConfig, err := K8sRestConfig()
	assert.Error(t, err)
	assert.Nil(t, restConfig)
}
