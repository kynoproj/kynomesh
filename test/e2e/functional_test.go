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
	"fmt"
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
	const entryPort = 8490
	const introspectPort = 8491
	const daemonPort = 9432

	w := s.Given().AgentSet("@testdata/research-assistant.yaml").
		When().
		CreateAgentSetAndWait().
		Expect().
		AgentSetRunning().
		AgentPodsRunning(2).
		DaemonPodsRunning().
		AgentServicesReady().
		When()

	defer func() {
		w.DeleteAgentSetAndWait().
			Expect().
			AgentSetDeleted(2 * time.Minute)
	}()

	defer w.AgentSetEntryPortForwardWithIntrospection(entryPort, introspectPort).
		DaemonPortForward(PortPair{Local: daemonPort, Remote: daemonPort}).
		// The daemon metrics check below needs at least one completed scrape
		// samples for "searcher", or it 503s with "no samples yet".
		Wait(7 * time.Second).TerminateAllPodPortForwards()

	w.SendA2AMessage(entryPort, "Hello, what can you do?").
		Expect().
		AgentResponseContains(`via "searcher"`)

	HTTPExpect(s.T(), fmt.Sprintf("https://localhost:%d", introspectPort)).GET("/introspect").
		Expect().
		Status(200).Body().
		Contains("host").
		Contains("peerHashes").
		Contains("searcher")

	HTTPExpect(s.T(), fmt.Sprintf("https://localhost:%d", daemonPort)).
		GET("/api/v1/agentdeploys/searcher/metrics").
		WithQuery("lookbackSeconds", 30).
		Expect().
		Status(200).Body().Contains(`"metrics"`).
		Contains(`"agentSet":"research-assistant"`).
		Contains("streamMessageRates").
		Contains(`"customWindowEffectiveSeconds":"30"`)
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
		DaemonPodsRunning().
		AgentServicesReady().
		When().
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
