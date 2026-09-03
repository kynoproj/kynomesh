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
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	agentpkg "github.com/kynoproj/kynomesh/pkg/client/clientset/versioned/typed/kynomesh/v1alpha1"
)

// When carries the actions phase of the e2e fixture DSL.
type When struct {
	t                 *testing.T
	agentSetClient    agentpkg.AgentSetInterface
	agentDeployClient agentpkg.AgentDeployInterface
	agentSet          *kmv1.AgentSet
	agentDeploy       *kmv1.AgentDeploy
	restConfig        *rest.Config
	kubeClient        kubernetes.Interface

	portForwarderStopChannels map[string]chan struct{}
	streamLogsStopChannels    map[string]chan struct{}

	// loadStop signals background load generators to stop; loadDone is closed
	// once they have all returned. Both nil when no load is running.
	loadStop chan struct{}
	loadDone <-chan struct{}

	// a2aResponse holds the parsed result of the last SendA2AMessage call so
	// the following Expect can assert on it.
	a2aResponse A2AResponse
}

// CreateAgentSetAndWait creates the AgentSet on the cluster and blocks until
// the controller reports the Running phase.
func (w *When) CreateAgentSetAndWait() *When {
	w.t.Helper()
	if w.agentSet == nil {
		w.t.Fatal("No AgentSet to create")
	}
	w.t.Log("Creating AgentSet", w.agentSet.Name)
	ctx := context.Background()
	created, err := w.agentSetClient.Create(ctx, w.agentSet, metav1.CreateOptions{})
	if err != nil {
		w.t.Fatal(err)
	}
	w.agentSet = created
	if err := WaitForAgentSetRunning(ctx, w.agentSetClient, w.agentSet.Name, defaultTimeout); err != nil {
		w.t.Fatal(err)
	}
	return w
}

// DeleteAgentSetAndWait deletes the AgentSet and blocks until all its pods are
// gone.
func (w *When) DeleteAgentSetAndWait() *When {
	w.t.Helper()
	if w.agentSet == nil {
		w.t.Fatal("No AgentSet to delete")
	}
	w.t.Log("Deleting AgentSet", w.agentSet.Name)
	ctx := context.Background()
	if err := w.agentSetClient.Delete(ctx, w.agentSet.Name, metav1.DeleteOptions{}); err != nil {
		w.t.Fatal(err)
	}
	if err := WaitForAgentSetPodsTerminated(ctx, w.kubeClient, Namespace, w.agentSet.Name, defaultTimeout); err != nil {
		w.t.Fatalf("Timeout waiting for AgentSet %q pods to terminate: %v", w.agentSet.Name, err)
	}
	return w
}

// UpdateAgentSet applies mutate to the AgentSet under test and pushes the
// result to the API server, refreshing the in-memory copy.
func (w *When) UpdateAgentSet(mutate func(*kmv1.AgentSet)) *When {
	w.t.Helper()
	if w.agentSet == nil {
		w.t.Fatal("No AgentSet to update")
	}
	ctx := context.Background()
	current, err := w.agentSetClient.Get(ctx, w.agentSet.Name, metav1.GetOptions{})
	if err != nil {
		w.t.Fatal(err)
	}
	updated := current.DeepCopy()
	mutate(updated)
	out, err := w.agentSetClient.Update(ctx, updated, metav1.UpdateOptions{})
	if err != nil {
		w.t.Fatal(err)
	}
	w.agentSet = out
	return w
}

// AgentDeployBrokerPortForward forwards localPort to the broker port (8490) on a
// pod of the AgentDeploy named agentName.
func (w *When) AgentDeployBrokerPortForward(agentName string, localPort int) *When {
	return w.AgentDeployPortForward(agentName, PortPair{Local: localPort, Remote: kmv1.AgentBrokerPort})
}

// AgentDeployPortForwardWithIntrospection forwards both the broker port (8490)
// and the introspection port (8491) of the named agent's pod, through one tunnel
// to the SAME pod — so a metrics scrape on 8491 observes the pod receiving 8490
// load.
func (w *When) AgentDeployPortForwardWithIntrospection(agentName string, brokerLocalPort, introspectLocalPort int) *When {
	return w.AgentDeployPortForward(agentName,
		PortPair{Local: brokerLocalPort, Remote: kmv1.AgentBrokerPort},
		PortPair{Local: introspectLocalPort, Remote: kmv1.AgentBrokerIntrospectionPort},
	)
}

// AgentSetEntryPortForward forwards localPort to the broker port (8490) on a pod
// of the AgentSet's entry agent, so a2acli can reach the entry agent over the
// forward.
func (w *When) AgentSetEntryPortForward(localPort int) *When {
	return w.AgentDeployBrokerPortForward(w.entryAgentName(), localPort)
}

// AgentSetEntryPortForwardWithIntrospection forwards both the broker port (8490)
// and the introspection port (8491) of the entry agent's pod, through one tunnel
// to the SAME pod.
func (w *When) AgentSetEntryPortForwardWithIntrospection(entryLocalPort, introspectLocalPort int) *When {
	return w.AgentDeployPortForwardWithIntrospection(w.entryAgentName(), entryLocalPort, introspectLocalPort)
}

// entryAgentName returns the name of the AgentSet's entry agent.
func (w *When) entryAgentName() string {
	w.t.Helper()
	if w.agentSet == nil {
		w.t.Fatal("No AgentSet selected for port-forward")
	}
	return w.agentSet.Spec.Entry
}

// AgentDeployPortForward forwards all given port pairs to a pod of the AgentDeploy
// named agentName through a single tunnel, so every pair lands on the same pod.
func (w *When) AgentDeployPortForward(agentName string, pairs ...PortPair) *When {
	w.t.Helper()
	if w.agentSet == nil {
		w.t.Fatal("No AgentSet selected for port-forward")
	}
	ctx := context.Background()
	podName, err := AgentDeployPodName(ctx, w.kubeClient, Namespace, w.agentSet.Name, agentName)
	if err != nil {
		w.t.Fatalf("Failed to resolve pod for agent %q: %v", agentName, err)
	}
	w.t.Logf("Port-forward %s: %v", podName, pairs)

	stopCh := make(chan struct{}, 1)
	if err := PodPortForward(w.restConfig, Namespace, podName, pairs, stopCh); err != nil {
		w.t.Fatalf("Failed to start port-forward to %s: %v", podName, err)
	}
	if w.portForwarderStopChannels == nil {
		w.portForwarderStopChannels = make(map[string]chan struct{})
	}
	w.portForwarderStopChannels[podName] = stopCh
	return w
}

// DaemonPortForward forwards all given port pairs to a the daemon pod.
func (w *When) DaemonPortForward(pairs ...PortPair) *When {
	w.t.Helper()
	if w.agentSet == nil {
		w.t.Fatal("No AgentSet selected for port-forward")
	}
	ctx := context.Background()
	podName, err := DaemonPodName(ctx, w.kubeClient, Namespace, w.agentSet.Name)
	if err != nil {
		w.t.Fatalf("Failed to resolve daemon pod for AgentSet %q: %v", w.agentSet.Name, err)
	}
	w.t.Logf("Port-forward %s: %v", podName, pairs)

	stopCh := make(chan struct{}, 1)
	if err := PodPortForward(w.restConfig, Namespace, podName, pairs, stopCh); err != nil {
		w.t.Fatalf("Failed to start port-forward to %s: %v", podName, err)
	}
	if w.portForwarderStopChannels == nil {
		w.portForwarderStopChannels = make(map[string]chan struct{})
	}
	w.portForwarderStopChannels[podName] = stopCh
	return w
}

// SendA2AMessage sends message to localPort (a forward to the entry Service)
// via a2acli, stashing the response for the following Expect assertion.
func (w *When) SendA2AMessage(localPort int, message string) *When {
	w.t.Helper()
	resp, err := SendA2AMessage(localPort, message)
	if err != nil {
		w.t.Fatalf("a2acli send failed: %v", err)
	}
	w.t.Logf("a2acli response: %s", resp.Raw)
	w.a2aResponse = resp
	return w
}

// GenerateLoad starts sustained concurrent load in the background against
// localPort (a forward to the entry Service), so the chain can proceed to a
// scale-up assertion while load is applied. It runs until StopLoad is called.
func (w *When) GenerateLoad(localPort, concurrency int) *When {
	w.t.Helper()
	if w.loadStop != nil {
		w.t.Fatal("Load already running; call StopLoad first")
	}
	w.t.Logf("Generating load: %d concurrent senders", concurrency)
	w.loadStop = make(chan struct{})
	w.loadDone = GenerateLoad(localPort, "drive autoscaling load", concurrency, w.loadStop)
	return w
}

// StopLoad signals the background load to stop and blocks until every sender has
// returned, so a following scale-down assertion sees in-flight actually drain.
func (w *When) StopLoad() *When {
	w.t.Helper()
	if w.loadStop == nil {
		return w
	}
	w.t.Log("Stopping load")
	close(w.loadStop)
	<-w.loadDone
	w.loadStop = nil
	w.loadDone = nil
	w.t.Log("Load stopped")
	return w
}

// TerminateAllPodPortForwards closes every active port-forward started via
// this fixture.
func (w *When) TerminateAllPodPortForwards() *When {
	w.t.Helper()
	for k, v := range w.portForwarderStopChannels {
		w.t.Logf("Terminating port-forward for POD %s", k)
		close(v)
	}
	w.portForwarderStopChannels = nil
	return w
}

// StreamAgentSetPodLogs follows logs for every running pod backing the
// AgentSet, printing them to the test output.
func (w *When) StreamAgentSetPodLogs(containerName string) *When {
	w.t.Helper()
	if w.agentSet == nil {
		w.t.Fatal("No AgentSet selected for log streaming")
	}
	ctx := context.Background()
	labelSelector := fmt.Sprintf("%s=%s", kmv1.KeyAgentSetName, w.agentSet.Name)
	podList, err := w.kubeClient.CoreV1().Pods(Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		w.t.Fatalf("Error listing AgentSet pods: %v", err)
	}
	for _, pod := range podList.Items {
		stopCh := make(chan struct{}, 1)
		streamPodLogs(ctx, w.kubeClient, Namespace, pod.Name, containerName, stopCh)
		if w.streamLogsStopChannels == nil {
			w.streamLogsStopChannels = make(map[string]chan struct{})
		}
		w.streamLogsStopChannels[pod.Name+":"+containerName] = stopCh
	}
	return w
}

// StreamControllerLogs follows logs from kynomesh controller-manager pods.
func (w *When) StreamControllerLogs() *When {
	w.t.Helper()
	ctx := context.Background()
	labelSelector := fmt.Sprintf("%s=%s", kmv1.KeyComponent, kmv1.ComponentControllerManager)
	podList, err := w.kubeClient.CoreV1().Pods(Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		w.t.Fatalf("Error listing controller pods: %v", err)
	}
	for _, pod := range podList.Items {
		stopCh := make(chan struct{}, 1)
		streamPodLogs(ctx, w.kubeClient, Namespace, pod.Name, kmv1.ContainerNameController, stopCh)
		if w.streamLogsStopChannels == nil {
			w.streamLogsStopChannels = make(map[string]chan struct{})
		}
		w.streamLogsStopChannels[pod.Name+":controller-manager"] = stopCh
	}
	return w
}

// TerminateAllPodLogs stops all active log streams started via this fixture.
func (w *When) TerminateAllPodLogs() *When {
	w.t.Helper()
	for k, v := range w.streamLogsStopChannels {
		w.t.Logf("Terminating log streaming for POD %s", k)
		close(v)
	}
	w.streamLogsStopChannels = nil
	return w
}

// Wait pauses for the given duration. Useful between actions when a controller
// reconciliation needs settling time.
func (w *When) Wait(timeout time.Duration) *When {
	w.t.Helper()
	w.t.Log("Waiting for", timeout.String())
	time.Sleep(timeout)
	w.t.Log("Done waiting")
	return w
}

// And runs an arbitrary block of code as part of the chain, failing the test
// fast if the block recorded a failure.
func (w *When) And(block func()) *When {
	w.t.Helper()
	block()
	if w.t.Failed() {
		w.t.FailNow()
	}
	return w
}

// Exec runs a CLI command and hands its output to block for assertions.
func (w *When) Exec(name string, args []string, block func(t *testing.T, output string, err error)) *When {
	w.t.Helper()
	output, err := Exec(name, args...)
	block(w.t, output, err)
	if w.t.Failed() {
		w.t.FailNow()
	}
	return w
}

// Given returns a Given handle sharing this When's state, allowing the test to
// load additional resources in the middle of a flow.
func (w *When) Given() *Given {
	return &Given{
		t:                 w.t,
		agentSetClient:    w.agentSetClient,
		agentDeployClient: w.agentDeployClient,
		agentSet:          w.agentSet,
		agentDeploy:       w.agentDeploy,
		restConfig:        w.restConfig,
		kubeClient:        w.kubeClient,
	}
}

// Expect transitions the fixture into the assertion phase.
func (w *When) Expect() *Expect {
	return &Expect{
		t:                         w.t,
		agentSetClient:            w.agentSetClient,
		agentDeployClient:         w.agentDeployClient,
		agentSet:                  w.agentSet,
		agentDeploy:               w.agentDeploy,
		restConfig:                w.restConfig,
		kubeClient:                w.kubeClient,
		a2aResponse:               w.a2aResponse,
		portForwarderStopChannels: w.portForwarderStopChannels,
		loadStop:                  w.loadStop,
		loadDone:                  w.loadDone,
	}
}
