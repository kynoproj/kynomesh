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
	"github.com/spf13/cobra"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	daemoncmd "github.com/kynoproj/kynomesh/pkg/daemon/cmd"
	sharedutil "github.com/kynoproj/kynomesh/pkg/shared/util"
)

func NewDaemonCommand() *cobra.Command {
	var (
		apiPort     int
		metricsPort int
	)

	command := &cobra.Command{
		Use:   "daemon",
		Short: "Start the per-AgentSet metrics daemon (gRPC + REST API)",
		Run: func(_ *cobra.Command, _ []string) {
			daemoncmd.Start(apiPort, metricsPort)
		},
	}
	command.Flags().IntVar(&apiPort, "api-port",
		sharedutil.LookupEnvIntOr("KYNOMESH_DAEMON_API_PORT", kmv1.DaemonAPIPort),
		"Port the daemon listens on for gRPC + REST API (multiplexed via ALPN).")
	command.Flags().IntVar(&metricsPort, "metrics-port",
		sharedutil.LookupEnvIntOr("KYNOMESH_DAEMON_METRICS_PORT", kmv1.DaemonMetricsPort),
		"Port the daemon listens on for its own /metrics endpoint.")
	return command
}
