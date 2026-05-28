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
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

// NewInitSocketCommand returns the "init-socket" subcommand.
func NewInitSocketCommand() *cobra.Command {
	var path string
	command := &cobra.Command{
		Use:   "init-socket",
		Short: "Initialize the broker UDS socket path (init container entrypoint)",
		Long: "Remove any stale file at the broker socket path and create a " +
			"fresh empty placeholder. Intended to run as the init container " +
			"in AgentDeploy pods before the broker and agent containers start.",
		RunE: func(_ *cobra.Command, _ []string) error {
			return initSocket(path)
		},
	}
	command.Flags().StringVar(&path, "path", kmv1.BrokerSocketPath,
		"Path at which to (re)create the placeholder file. Defaults to the broker's canonical socket path.")
	return command
}

// initSocket is the pure-Go body of the command; factored out so unit
// tests can exercise it without spinning up cobra.
func initSocket(path string) error {
	// Ensure the dir exists.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent dir %q: %w", filepath.Dir(path), err)
	}

	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove existing %q: %w", path, err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %q: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %q: %w", path, err)
	}
	return nil
}
