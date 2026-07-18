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
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	flowpkg "github.com/kynoproj/kynomesh/pkg/client/clientset/versioned/typed/kynomesh/v1alpha1"
)

const (
	pollInterval = 2 * time.Second
)

// HttpPostRequest represents an HTTP POST payload used by HTTP-based agent
// fixtures.
type HttpPostRequest struct {
	URL  string
	Body []byte
	// Optional headers to add to the request.
	Header http.Header
}

// SendMessageTo POSTs req.Body to the given host. The path is the URL on the
// remote host. It is used by When fixtures to exercise an agent's HTTP
// endpoint via a port-forward.
func SendMessageTo(host, path string, req HttpPostRequest) {
	target := req.URL
	if target == "" {
		target = fmt.Sprintf("http://%s/%s", host, strings.TrimPrefix(path, "/"))
	}
	r, err := http.NewRequest(http.MethodPost, target, strings.NewReader(string(req.Body)))
	if err != nil {
		return
	}
	for k, vs := range req.Header {
		for _, v := range vs {
			r.Header.Add(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}

// Exec runs a CLI command and returns its combined output. Useful for shelling
// out to kubectl during e2e flows.
func Exec(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// EntryPodName returns the name of a running pod backing the AgentSet's entry
// Service. Kubernetes port-forwarding is pod-level, so forwarding "to the entry
// Service" means resolving one of its backing pods first, exactly as
// `kubectl port-forward svc/<name>-ingress` does under the hood.
func EntryPodName(ctx context.Context, kube kubernetes.Interface, namespace, agentSetName string) (string, error) {
	selector := fmt.Sprintf("%s=%s,%s=true,%s=true",
		kmv1.KeyAgentSetName, agentSetName, kmv1.KeyEntry, kmv1.KeyServing)
	podList, err := kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		return "", fmt.Errorf("failed to list entry pods for AgentSet %q: %w", agentSetName, err)
	}
	if len(podList.Items) == 0 {
		return "", fmt.Errorf("no running entry pods found for AgentSet %q", agentSetName)
	}
	return podList.Items[0].Name, nil
}

// WaitForAgentSetRunning blocks until the AgentSet reaches the Running phase
// or timeout elapses.
func WaitForAgentSetRunning(ctx context.Context, c flowpkg.AgentSetInterface, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		as, err := c.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierr.IsNotFound(err) {
				return false, nil
			}
			return false, fmt.Errorf("failed to get AgentSet %q: %w", name, err)
		}
		return as.Status.Phase == kmv1.AgentSetPhaseRunning, nil
	})
}

// WaitForAgentSetDeleted blocks until the AgentSet no longer exists.
func WaitForAgentSetDeleted(ctx context.Context, c flowpkg.AgentSetInterface, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		_, err := c.Get(ctx, name, metav1.GetOptions{})
		if apierr.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, fmt.Errorf("failed to get AgentSet %q: %w", name, err)
		}
		return false, nil
	})
}

// WaitForAgentSetPodsRunning waits until at least minReady pods owned by the
// AgentSet are in the Running phase.
func WaitForAgentSetPodsRunning(ctx context.Context, kube kubernetes.Interface, namespace, agentSetName string, minReady int, timeout time.Duration) error {
	selector := fmt.Sprintf("%s=%s,%s=%s", kmv1.KeyAgentSetName, agentSetName, kmv1.KeyComponent, kmv1.ComponentAgent)
	return wait.PollUntilContextTimeout(ctx, pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		podList, err := kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return false, fmt.Errorf("failed to list AgentSet pods: %w", err)
		}
		running := 0
		for _, p := range podList.Items {
			if p.Status.Phase == corev1.PodRunning {
				running++
			}
		}
		return running >= minReady, nil
	})
}

// WaitForAgentSetPodsTerminated blocks until no pods matching the AgentSet
// remain in the namespace.
func WaitForAgentSetPodsTerminated(ctx context.Context, kube kubernetes.Interface, namespace, agentSetName string, timeout time.Duration) error {
	selector := fmt.Sprintf("%s=%s,%s=%s", kmv1.KeyAgentSetName, agentSetName, kmv1.KeyComponent, kmv1.ComponentAgent)
	return wait.PollUntilContextTimeout(ctx, pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		podList, err := kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return false, fmt.Errorf("failed to list AgentSet pods: %w", err)
		}
		return len(podList.Items) == 0, nil
	})
}

// ServiceHasReadyEndpoint reports whether some EndpointSlice for the named
// Service has at least one endpoint with Ready==true.
func ServiceHasReadyEndpoint(ctx context.Context, kube kubernetes.Interface, namespace, serviceName string) (bool, error) {
	selector := fmt.Sprintf("%s=%s", discoveryv1.LabelServiceName, serviceName)
	slices, err := kube.DiscoveryV1().EndpointSlices(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return false, fmt.Errorf("failed to list EndpointSlices for Service %q: %w", serviceName, err)
	}
	for _, sl := range slices.Items {
		for _, ep := range sl.Endpoints {
			if ep.Conditions.Ready != nil && *ep.Conditions.Ready {
				return true, nil
			}
		}
	}
	return false, nil
}

// WaitForServicesReady blocks until every named Service has a ready endpoint or
// timeout elapses.
func WaitForServicesReady(ctx context.Context, kube kubernetes.Interface, namespace string, serviceNames []string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		for _, name := range serviceNames {
			ready, err := ServiceHasReadyEndpoint(ctx, kube, namespace, name)
			if err != nil {
				return false, err
			}
			if !ready {
				return false, nil
			}
		}
		return true, nil
	})
}

// PodPortForward forwards a local port to a port on the named pod. The caller
// is responsible for closing stopCh to terminate the forward.
func PodPortForward(restConfig *rest.Config, namespace, podName string, localPort, remotePort int, stopCh chan struct{}) error {
	roundTripper, upgrader, err := spdy.RoundTripperFor(restConfig)
	if err != nil {
		return fmt.Errorf("failed to build SPDY round tripper: %w", err)
	}
	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", namespace, podName)
	hostIP := strings.TrimLeft(restConfig.Host, "htps:/")
	serverURL := &url.URL{Scheme: "https", Path: path, Host: hostIP}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: roundTripper}, http.MethodPost, serverURL)
	readyCh := make(chan struct{})
	forwarder, err := portforward.New(dialer, []string{fmt.Sprintf("%d:%d", localPort, remotePort)}, stopCh, readyCh, io.Discard, io.Discard)
	if err != nil {
		return fmt.Errorf("failed to create port-forwarder: %w", err)
	}
	errCh := make(chan error, 1)
	go func() {
		if err := forwarder.ForwardPorts(); err != nil {
			errCh <- err
		}
	}()
	select {
	case <-readyCh:
		return nil
	case err := <-errCh:
		return fmt.Errorf("port-forward failed: %w", err)
	case <-time.After(30 * time.Second):
		return fmt.Errorf("timed out waiting for port-forward to be ready")
	}
}

// streamPodLogs streams logs from a pod container until stopCh is closed.
func streamPodLogs(ctx context.Context, kube kubernetes.Interface, namespace, podName, containerName string, stopCh chan struct{}) {
	go func() {
		req := kube.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
			Container: containerName,
			Follow:    true,
		})
		stream, err := req.Stream(ctx)
		if err != nil {
			return
		}
		defer func() { _ = stream.Close() }()
		scanner := bufio.NewScanner(stream)
		for scanner.Scan() {
			select {
			case <-stopCh:
				return
			default:
				fmt.Printf("[%s/%s] %s\n", podName, containerName, scanner.Text())
			}
		}
	}()
}

// PodLogCheckOption configures how AgentSetPodLogContains evaluates log lines.
type PodLogCheckOption func(*podLogCheckOptions)

type podLogCheckOptions struct {
	timeout       time.Duration
	containerName string
}

// PodLogCheckOptionWithTimeout caps the time spent scanning logs.
func PodLogCheckOptionWithTimeout(d time.Duration) PodLogCheckOption {
	return func(o *podLogCheckOptions) { o.timeout = d }
}

// PodLogCheckOptionWithContainer restricts the check to a specific container.
func PodLogCheckOptionWithContainer(name string) PodLogCheckOption {
	return func(o *podLogCheckOptions) { o.containerName = name }
}

// AgentSetPodLogContains returns true if any pod backing the AgentSet has a
// log line matching regex within the configured timeout.
func AgentSetPodLogContains(ctx context.Context, kube kubernetes.Interface, namespace, agentSetName, regex string, opts ...PodLogCheckOption) (bool, error) {
	cfg := &podLogCheckOptions{timeout: time.Minute}
	for _, o := range opts {
		o(cfg)
	}
	re, err := regexp.Compile(regex)
	if err != nil {
		return false, fmt.Errorf("invalid regex %q: %w", regex, err)
	}
	selector := fmt.Sprintf("%s=%s", kmv1.KeyAgentSetName, agentSetName)
	podList, err := kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return false, fmt.Errorf("failed to list AgentSet pods: %w", err)
	}
	streamCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()
	for _, pod := range podList.Items {
		req := kube.CoreV1().Pods(namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
			Container: cfg.containerName,
			Follow:    true,
		})
		stream, err := req.Stream(streamCtx)
		if err != nil {
			continue
		}
		found := scanLogsFor(stream, re)
		_ = stream.Close()
		if found {
			return true, nil
		}
	}
	return false, nil
}

func scanLogsFor(stream io.Reader, re *regexp.Regexp) bool {
	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		if re.MatchString(scanner.Text()) {
			return true
		}
	}
	return false
}
