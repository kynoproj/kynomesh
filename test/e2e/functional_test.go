//go:build e2e

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

package e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	. "github.com/kynoproj/kynomesh/test/fixtures"
)

type FunctionalSuite struct {
	E2ESuite
}

// TestResearchAssistant brings up the two-agent research-assistant AgentSet,
// sends a message to the entry (coordinator) agent via a2acli over a
// port-forward, and asserts the coordinator delegated to the searcher peer.
func (s *FunctionalSuite) TestResearchAssistant() {
	const localPort = 8490

	s.Given().AgentSet("@testdata/research-assistant.yaml").
		When().
		CreateAgentSetAndWait().
		Expect().
		AgentSetRunning().
		AgentPodsRunning(2).
		When().
		WaitForAgentServicesReady().
		AgentSetEntryPortForward(localPort).
		Wait(2*time.Second).
		SendA2AMessage(localPort, "Hello, what can you do?").
		Expect().
		AgentResponseContains(`via "searcher"`).
		When().
		TerminateAllPodPortForwards().
		DeleteAgentSetAndWait().
		Expect().
		AgentSetDeleted(2 * time.Minute)
}

// TestRateLimitShedsExcessLoad verifies broker-side max-in-flight enforcement
// across a fleet.
func (s *FunctionalSuite) TestRateLimitShedsExcessLoad() {
	const (
		entryPort      = 8490
		introspectPort = 8491
		concurrency    = 15 // well above the maxInFlight of 4
		rejectWait     = 2 * time.Minute
	)

	s.Given().AgentSet("@testdata/slow-agent-ratelimit.yaml").
		When().
		CreateAgentSetAndWait().
		Expect().
		AgentSetRunning().
		AgentPodsRunning(2).
		When().
		WaitForAgentServicesReady().
		AgentSetEntryPortForwardWithIntrospection(entryPort, introspectPort).
		GenerateLoad(entryPort, concurrency).
		Expect().
		// The cap sheds the excess concurrent load: rejections must appear.
		BrokerRejectedRequests(introspectPort, rejectWait).
		When().
		StopLoad().
		TerminateAllPodPortForwards().
		DeleteAgentSetAndWait().
		Expect().
		AgentSetDeleted(2 * time.Minute)
}

func TestFunctionalSuite(t *testing.T) {
	suite.Run(t, new(FunctionalSuite))
}
