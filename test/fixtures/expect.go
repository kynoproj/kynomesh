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

package fixtures

import (
	"context"
	"strings"
	"testing"
	"time"

	apierr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	agentpkg "github.com/kynoproj/kynomesh/pkg/client/clientset/versioned/typed/kynomesh/v1alpha1"
)

// Expect carries the assertion phase of the e2e fixture DSL.
type Expect struct {
	t                 *testing.T
	agentSetClient    agentpkg.AgentSetInterface
	agentDeployClient agentpkg.AgentDeployInterface
	agentSet          *kmv1.AgentSet
	agentDeploy       *kmv1.AgentDeploy
	restConfig        *rest.Config
	kubeClient        kubernetes.Interface
	a2aResponse       A2AResponse
}

// AgentSetRunning asserts that the AgentSet has reached the Running phase
// within defaultTimeout.
func (e *Expect) AgentSetRunning() *Expect {
	e.t.Helper()
	ctx := context.Background()
	if err := WaitForAgentSetRunning(ctx, e.agentSetClient, e.agentSet.Name, defaultTimeout); err != nil {
		e.t.Fatalf("Expected AgentSet %q to be Running: %v", e.agentSet.Name, err)
	}
	return e
}

// AgentSetDeleted asserts that the AgentSet has been removed from the API
// server within timeout.
func (e *Expect) AgentSetDeleted(timeout time.Duration) *Expect {
	e.t.Helper()
	ctx := context.Background()
	if err := WaitForAgentSetDeleted(ctx, e.agentSetClient, e.agentSet.Name, timeout); err != nil {
		e.t.Fatalf("Expected AgentSet %q to be deleted: %v", e.agentSet.Name, err)
	}
	return e
}

// AgentSetExists asserts that the API server still has the AgentSet (no
// background deletion has happened).
func (e *Expect) AgentSetExists() *Expect {
	e.t.Helper()
	ctx := context.Background()
	if _, err := e.agentSetClient.Get(ctx, e.agentSet.Name, metav1.GetOptions{}); err != nil {
		if apierr.IsNotFound(err) {
			e.t.Fatalf("Expected AgentSet %q to exist", e.agentSet.Name)
		}
		e.t.Fatalf("Failed to get AgentSet %q: %v", e.agentSet.Name, err)
	}
	return e
}

// AgentPodsRunning asserts that at least minReady pods backing the AgentSet
// are in the Running phase. Pass the expected pod count for the AgentSet
// (typically the sum of agent replicas).
func (e *Expect) AgentPodsRunning(minReady int) *Expect {
	e.t.Helper()
	ctx := context.Background()
	if err := WaitForAgentSetPodsRunning(ctx, e.kubeClient, Namespace, e.agentSet.Name, minReady, defaultTimeout); err != nil {
		e.t.Fatalf("Expected %d pods running for AgentSet %q: %v", minReady, e.agentSet.Name, err)
	}
	return e
}

// AgentSetCondition asserts that the AgentSet exposes the named condition with
// the expected status.
func (e *Expect) AgentSetCondition(condType kmv1.ConditionType, status metav1.ConditionStatus) *Expect {
	e.t.Helper()
	ctx := context.Background()
	as, err := e.agentSetClient.Get(ctx, e.agentSet.Name, metav1.GetOptions{})
	if err != nil {
		e.t.Fatalf("Failed to get AgentSet %q: %v", e.agentSet.Name, err)
	}
	c := as.Status.GetCondition(condType)
	if c == nil {
		e.t.Fatalf("Expected AgentSet %q to have condition %q", e.agentSet.Name, condType)
	}
	if c.Status != status {
		e.t.Fatalf("Expected AgentSet %q condition %q to be %s, got %s", e.agentSet.Name, condType, status, c.Status)
	}
	return e
}

// AgentSetPodLogContains asserts that some pod backing the AgentSet logs a
// line matching regex within the option-configured timeout.
func (e *Expect) AgentSetPodLogContains(regex string, opts ...PodLogCheckOption) *Expect {
	e.t.Helper()
	ctx := context.Background()
	contains, err := AgentSetPodLogContains(ctx, e.kubeClient, Namespace, e.agentSet.Name, regex, opts...)
	if err != nil {
		e.t.Fatalf("Failed to check AgentSet pod logs: %v", err)
	}
	if !contains {
		e.t.Fatalf("Expected AgentSet %q pod log to contain %q but it didn't", e.agentSet.Name, regex)
	}
	e.t.Logf("Confirmed AgentSet %q pod log contains %q", e.agentSet.Name, regex)
	return e
}

// AgentResponseContains asserts that the text of the last a2acli response
// captured by When().SendA2AMessage contains substr.
func (e *Expect) AgentResponseContains(substr string) *Expect {
	e.t.Helper()
	if !strings.Contains(e.a2aResponse.Text(), substr) {
		e.t.Fatalf("Expected agent response text to contain %q, got: %s", substr, e.a2aResponse.Raw)
	}
	e.t.Logf("Confirmed agent response text contains %q", substr)
	return e
}

// When transitions the fixture back into the actions phase, allowing chained
// assertion-then-action flows.
func (e *Expect) When() *When {
	return &When{
		t:                 e.t,
		agentSetClient:    e.agentSetClient,
		agentDeployClient: e.agentDeployClient,
		agentSet:          e.agentSet,
		agentDeploy:       e.agentDeploy,
		restConfig:        e.restConfig,
		kubeClient:        e.kubeClient,
		a2aResponse:       e.a2aResponse,
	}
}
