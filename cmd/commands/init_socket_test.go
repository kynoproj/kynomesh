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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitSocket_CreatesPlaceholder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broker.sock")

	require.NoError(t, initSocket(path))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.True(t, info.Mode().IsRegular(), "init-socket creates a regular file, not a socket — the broker will rebind it later")
	assert.Equal(t, int64(0), info.Size(), "placeholder must be empty")
}

func TestInitSocket_RemovesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broker.sock")
	require.NoError(t, os.WriteFile(path, []byte("stale content"), 0o644))

	require.NoError(t, initSocket(path))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Empty(t, got, "init-socket must overwrite, not preserve, prior contents")
}

func TestInitSocket_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "subdir", "broker.sock")

	require.NoError(t, initSocket(path))

	_, err := os.Stat(path)
	require.NoError(t, err)
}

func TestInitSocket_ErrorsWhenParentIsNotADirectory(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-dir")
	require.NoError(t, os.WriteFile(blocker, []byte("blocker"), 0o644))

	path := filepath.Join(blocker, "broker.sock")
	err := initSocket(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create parent dir")
}
