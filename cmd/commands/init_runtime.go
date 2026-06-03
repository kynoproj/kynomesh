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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

// NewInitRuntimeCommand returns the "init-runtime" subcommand.
func NewInitRuntimeCommand() *cobra.Command {
	var topologyPath, probeSrc, probeDst string
	command := &cobra.Command{
		Use:   "init-runtime",
		Short: "Prepare the per-agent runtime directory (topology, probe binary)",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := writeTopology(topologyPath, os.Getenv(kmv1.EnvAgentDeployObject)); err != nil {
				return err
			}
			return installProbeBinary(probeSrc, probeDst)
		},
	}
	command.Flags().StringVar(&topologyPath, "topology-path", kmv1.TopologyFilePath,
		"Path at which to write the per-agent topology JSON.")
	command.Flags().StringVar(&probeSrc, "probe-src", kmv1.ProbeBinaryImagePath,
		"Path to the probe binary inside the init-runtime image.")
	command.Flags().StringVar(&probeDst, "probe-dst", kmv1.ProbeBinaryPath,
		"Path on the shared kynomesh-run volume where the probe binary is installed.")
	return command
}

// writeTopology decodes the AgentDeploy payload and atomically writes its Topology as JSON to path.
func writeTopology(path, encodedAgentDeploy string) error {
	ad, err := kmv1.DecodeAgentDeploy(encodedAgentDeploy)
	if err != nil {
		return fmt.Errorf("decode AgentDeploy for topology: %w", err)
	}
	topology := resolvePeerURLs(ad)
	payload, err := json.Marshal(topology)
	if err != nil {
		return fmt.Errorf("marshal topology: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent dir %q: %w", filepath.Dir(path), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp topology file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write topology %q: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close topology %q: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename topology to %q: %w", path, err)
	}
	return nil
}

// resolvePeerURLs returns a Topology copy with every Managed peer's URL
// stamped from cluster DNS. External peers are left unchanged.
func resolvePeerURLs(ad *kmv1.AgentDeploy) kmv1.Topology {
	out := *ad.Spec.Topology.DeepCopy()
	for i := range out.Peers {
		p := &out.Peers[i]
		if p.Kind == kmv1.PeerKindExternal {
			continue
		}
		p.URL = managedPeerURL(ad.Spec.AgentSetName, p.Name, ad.Namespace)
	}
	return out
}

func managedPeerURL(setName, peerName, namespace string) string {
	host := fmt.Sprintf("%s-%s-headless.%s.svc.cluster.local", setName, peerName, namespace)
	return fmt.Sprintf("https://%s:%d", host, kmv1.AgentBrokerPort)
}

// installProbeBinary atomically copies src into dst with mode 0755 so the
// agent container can exec it as a readiness/liveness probe. The destination
// lives on the shared kynomesh-run tmpfs.
func installProbeBinary(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open probe binary %q: %w", src, err)
	}
	defer func() { _ = in.Close() }()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create parent dir %q: %w", filepath.Dir(dst), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp probe binary: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("copy probe binary to %q: %w", tmpName, err)
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("chmod probe binary %q: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close probe binary %q: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename probe binary to %q: %w", dst, err)
	}
	return nil
}
