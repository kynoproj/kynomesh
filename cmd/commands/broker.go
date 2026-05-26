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
	brokercmd "github.com/kynoproj/kynomesh/pkg/broker/cmd"
	sharedutil "github.com/kynoproj/kynomesh/pkg/shared/util"
)

func NewBrokerCommand() *cobra.Command {
	var (
		port          int
		advertiseHost string
	)

	command := &cobra.Command{
		Use:   "broker",
		Short: "Start the kynomesh A2A broker (JSON-RPC, REST, gRPC on one port)",
		Long: "Run the kynomesh A2A broker. The broker hosts an A2A server " +
			"reachable over every transport the a2a-go library supports — " +
			"JSON-RPC, REST, and gRPC — backed by a single shared request " +
			"handler and demultiplexed onto a single TCP port.",
		Run: func(cmd *cobra.Command, args []string) {
			brokercmd.Start(port, advertiseHost)
		},
	}
	command.Flags().IntVar(&port, "port",
		sharedutil.LookupEnvIntOr("KYNOMESH_BROKER_PORT", kmv1.AgentBrokerPort),
		"Port the broker listens on; all A2A transports share it.")
	command.Flags().StringVar(&advertiseHost, "advertise-host",
		sharedutil.LookupEnvStringOr("KYNOMESH_BROKER_ADVERTISE_HOST", brokercmd.AdvertiseHostDefault),
		"Hostname or IP advertised on the AgentCard for clients to dial.")
	return command
}
