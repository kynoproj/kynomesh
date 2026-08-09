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
	"time"

	"github.com/spf13/cobra"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	brokercmd "github.com/kynoproj/kynomesh/pkg/broker/cmd"
	sharedutil "github.com/kynoproj/kynomesh/pkg/shared/util"
)

// NewDrainCommand returns the "drain" subcommand, run as the broker pod's
// preStop hook. It waits for in-flight requests to finish before the container
// is terminated, so scale-down and rolling updates don't cut long-running
// agentic requests.
func NewDrainCommand() *cobra.Command {
	var (
		introspectionPort int
		propagationSecs   int
		budgetSecs        int
		pollSecs          int
	)

	command := &cobra.Command{
		Use:   "drain",
		Short: "Wait for in-flight broker requests to drain (broker pod preStop hook)",
		Run: func(cmd *cobra.Command, args []string) {
			brokercmd.RunDrain(brokercmd.DrainConfig{
				IntrospectionPort: introspectionPort,
				PropagationDelay:  time.Duration(propagationSecs) * time.Second,
				Budget:            time.Duration(budgetSecs) * time.Second,
				PollInterval:      time.Duration(pollSecs) * time.Second,
			})
		},
	}
	command.Flags().IntVar(&introspectionPort, "introspection-port",
		sharedutil.LookupEnvIntOr("KYNOMESH_BROKER_INTROSPECTION_PORT", kmv1.AgentBrokerIntrospectionPort),
		"Broker introspection port to scrape /metrics for in-flight requests.")
	command.Flags().IntVar(&propagationSecs, "propagation-seconds",
		sharedutil.LookupEnvIntOr("KYNOMESH_DRAIN_PROPAGATION_SECONDS", 5),
		"Fixed initial wait for endpoint removal to propagate before polling in-flight.")
	command.Flags().IntVar(&budgetSecs, "budget-seconds",
		sharedutil.LookupEnvIntOr("KYNOMESH_DRAIN_BUDGET_SECONDS", 110),
		"Overall drain budget; must fit within the pod's terminationGracePeriodSeconds.")
	command.Flags().IntVar(&pollSecs, "poll-seconds",
		sharedutil.LookupEnvIntOr("KYNOMESH_DRAIN_POLL_SECONDS", 2),
		"How often to re-check in-flight requests while draining.")
	return command
}
