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

// AgentSetPodPortForward forwards localPort to remotePort on the first running
// pod for the AgentSet.
func (w *When) AgentSetPodPortForward(localPort, remotePort int) *When {
	w.t.Helper()
	if w.agentSet == nil {
		w.t.Fatal("No AgentSet selected for port-forward")
	}
	labelSelector := fmt.Sprintf("%s=%s", kmv1.KeyAgentSetName, w.agentSet.Name)
	ctx := context.Background()
	podList, err := w.kubeClient.CoreV1().Pods(Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		w.t.Fatalf("Error listing AgentSet pods: %v", err)
	}
	if len(podList.Items) == 0 {
		w.t.Fatalf("No running pods found for AgentSet %q", w.agentSet.Name)
	}
	podName := podList.Items[0].GetName()
	w.t.Logf("AgentSet POD name: %s", podName)

	stopCh := make(chan struct{}, 1)
	if err := PodPortForward(w.restConfig, Namespace, podName, localPort, remotePort, stopCh); err != nil {
		w.t.Fatalf("Failed to start AgentSet pod port-forward: %v", err)
	}
	if w.portForwarderStopChannels == nil {
		w.portForwarderStopChannels = make(map[string]chan struct{})
	}
	w.portForwarderStopChannels[podName] = stopCh
	return w
}

// AgentSetEntryPortForward forwards localPort to the broker port (8490) on a
// pod backing the AgentSet's entry Service, so a2acli can reach the entry agent
// over the forward.
func (w *When) AgentSetEntryPortForward(localPort int) *When {
	w.t.Helper()
	if w.agentSet == nil {
		w.t.Fatal("No AgentSet selected for port-forward")
	}
	ctx := context.Background()
	podName, err := EntryPodName(ctx, w.kubeClient, Namespace, w.agentSet.Name)
	if err != nil {
		w.t.Fatalf("Failed to resolve entry pod: %v", err)
	}
	w.t.Logf("AgentSet entry POD name: %s", podName)

	stopCh := make(chan struct{}, 1)
	if err := PodPortForward(w.restConfig, Namespace, podName, localPort, kmv1.AgentBrokerPort, stopCh); err != nil {
		w.t.Fatalf("Failed to start entry port-forward: %v", err)
	}
	if w.portForwarderStopChannels == nil {
		w.portForwarderStopChannels = make(map[string]chan struct{})
	}
	w.portForwarderStopChannels[podName] = stopCh
	return w
}

// WaitForAgentServicesReady blocks until every per-agent ClusterIP Service and
// the entry (ingress) Service has a ready endpoint.
func (w *When) WaitForAgentServicesReady() *When {
	w.t.Helper()
	if w.agentSet == nil {
		w.t.Fatal("No AgentSet selected")
	}
	services := make([]string, 0, len(w.agentSet.Spec.Agents)+1)
	for _, a := range w.agentSet.Spec.Agents {
		services = append(services, w.agentSet.ChildAgentDeployName(a.Name))
	}
	services = append(services, w.agentSet.EntryServiceName())
	ctx := context.Background()
	if err := WaitForServicesReady(ctx, w.kubeClient, Namespace, services, defaultTimeout); err != nil {
		w.t.Fatalf("Timeout waiting for Services %v to have ready endpoints: %v", services, err)
	}
	w.t.Logf("Confirmed ready endpoints for Services %v", services)
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
		t:                 w.t,
		agentSetClient:    w.agentSetClient,
		agentDeployClient: w.agentDeployClient,
		agentSet:          w.agentSet,
		agentDeploy:       w.agentDeploy,
		restConfig:        w.restConfig,
		kubeClient:        w.kubeClient,
		a2aResponse:       w.a2aResponse,
	}
}
