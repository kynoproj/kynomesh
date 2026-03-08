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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

var (
	testSecretKeySelector = &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{
			Name: "test-secret",
		},
		Key: "test-key",
	}

	testConfigMapSelector = &corev1.ConfigMapKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{
			Name: "test-cm",
		},
		Key: "test-key",
	}
)

func Test_generateSecretVolumeSpecs(t *testing.T) {
	v, vm := generateSecretVolumeSpecs(testSecretKeySelector)
	assert.NotNil(t, v.Secret)
	assert.Equal(t, "test-secret", v.Secret.SecretName)
	assert.Equal(t, v.Name, vm.Name)
}

func Test_generateConfigMapVolumeSpecs(t *testing.T) {
	v, vm := generateConfigMapVolumeSpecs(testConfigMapSelector)
	assert.NotNil(t, v.ConfigMap)
	assert.Equal(t, "test-cm", v.ConfigMap.Name)
	assert.Equal(t, v.Name, vm.Name)
}

func Test_uniqueVolumes_VolumeMounts(t *testing.T) {
	v1, vm1 := generateSecretVolumeSpecs(testSecretKeySelector)
	v2, vm2 := generateConfigMapVolumeSpecs(testConfigMapSelector)
	assert.Equal(t, 2, len(uniqueVolumes([]corev1.Volume{v1, v2})))
	assert.Equal(t, 2, len(uniqueVolumeMounts([]corev1.VolumeMount{vm1, vm2})))
	assert.Equal(t, 2, len(uniqueVolumes([]corev1.Volume{v1, v2, v1})))
	assert.Equal(t, 2, len(uniqueVolumeMounts([]corev1.VolumeMount{vm1, vm2, vm1})))
}

func Test_volumesFromSecretsOrConfigMaps(t *testing.T) {
	t.Run("test secret", func(t *testing.T) {
		m := map[string]*corev1.SecretKeySelector{
			"a": testSecretKeySelector,
			"b": testSecretKeySelector,
			"c": {
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "test-secret-1",
				},
				Key: "test-key",
			},
		}
		vs, vms := volumesFromSecretsOrConfigMaps(m, secretKeySelectorType)
		assert.Equal(t, 2, len(vs))
		assert.Equal(t, 2, len(vms))
	})

	t.Run("test config map", func(t *testing.T) {
		m := map[string]*corev1.ConfigMapKeySelector{
			"a": testConfigMapSelector,
			"b": testConfigMapSelector,
			"c": {
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "test-cm-1",
				},
				Key: "test-key",
			},
		}
		vs, vms := volumesFromSecretsOrConfigMaps(m, configMapKeySelectorType)
		assert.Equal(t, 2, len(vs))
		assert.Equal(t, 2, len(vms))
	})

	t.Run("test both", func(t *testing.T) {
		m := map[string]interface{}{
			"a1": testSecretKeySelector,
			"b1": testSecretKeySelector,
			"c1": &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "test-secret-1",
				},
				Key: "test-key",
			},
			"a2": testConfigMapSelector,
			"b2": testConfigMapSelector,
			"c2": &corev1.ConfigMapKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "test-cm-1",
				},
				Key: "test-key",
			},
		}
		vs, vms := VolumesFromSecretsAndConfigMaps(m)
		assert.Equal(t, 4, len(vs))
		assert.Equal(t, 4, len(vms))
	})
}

func Test_GetConfigMapVolumePath(t *testing.T) {
	p, e := GetConfigMapVolumePath(testConfigMapSelector)
	assert.Nil(t, e)
	assert.Equal(t, "/var/kyno/config/test-cm/test-key", p)

	_, e = GetConfigMapVolumePath(nil)
	assert.Error(t, e)
}

func Test_GetSecretVolumePath(t *testing.T) {
	p, e := GetSecretVolumePath(testSecretKeySelector)
	assert.Nil(t, e)
	assert.Equal(t, "/var/kyno/secrets/test-secret/test-key", p)

	_, e = GetSecretVolumePath(nil)
	assert.Error(t, e)
}

func TestGetSecretFromVolume_Success(t *testing.T) {
	// Skip if we can't write to /var (e.g. non-root)
	if err := os.MkdirAll("/var/kyno/secrets", 0o755); err != nil {
		t.Skip("cannot write to /var/kyno, skipping")
	}
	t.Cleanup(func() { _ = os.RemoveAll("/var/kyno") })

	dir := filepath.Join(t.TempDir(), "secrets", "my-secret")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "my-key"), []byte("secret-value\n"), 0o644))

	mountDir := "/var/kyno/secrets/my-secret"
	require.NoError(t, os.Symlink(dir, mountDir))

	val, err := GetSecretFromVolume(&corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "my-secret"},
		Key:                  "my-key",
	})
	assert.NoError(t, err)
	assert.Equal(t, "secret-value", val)
}

func TestGetConfigMapFromVolume_Success(t *testing.T) {
	// Skip if we can't write to /var (e.g. non-root)
	if err := os.MkdirAll("/var/kyno/config", 0o755); err != nil {
		t.Skip("cannot write to /var/kyno, skipping")
	}
	t.Cleanup(func() { _ = os.RemoveAll("/var/kyno") })

	dir := filepath.Join(t.TempDir(), "config", "my-cm")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "my-key"), []byte("cm-value\n"), 0o644))

	mountDir := "/var/kyno/config/my-cm"
	require.NoError(t, os.Symlink(dir, mountDir))

	val, err := GetConfigMapFromVolume(&corev1.ConfigMapKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "my-cm"},
		Key:                  "my-key",
	})
	assert.NoError(t, err)
	assert.Equal(t, "cm-value", val)
}

func TestGetConfigMapFromVolume_FileNotFound(t *testing.T) {
	// file not found error
	selector := &corev1.ConfigMapKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "test-configmap"},
		Key:                  "test-key",
	}

	_, err := GetConfigMapFromVolume(selector)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get configMap value")
}

func TestGetSecretFromVolume_FileNotFound(t *testing.T) {

	selector := &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
		Key:                  "test-key",
	}

	_, err := GetSecretFromVolume(selector)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get secret value")
}
