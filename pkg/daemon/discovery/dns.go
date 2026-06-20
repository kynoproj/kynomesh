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

// Package discovery resolves the live pods of an AgentDeploy through
// the headless Service that the AgentDeploy reconciler maintains. The
// daemon never talks to the Kubernetes API; pod-set membership is
// derived from DNS A-records (ready endpoints only).
package discovery

import (
	"context"
	"errors"
	"fmt"
	"net"
)

// HeadlessSuffix matches v1alpha1.AgentDeploy.HeadlessServiceName().
// Duplicated here because the daemon package must not import the
// reconciler-side AgentDeploy methods (those pull in K8s client
// dependencies the daemon doesn't need).
const HeadlessSuffix = "-headless"

// Resolver is a small interface that net.DefaultResolver satisfies;
// tests inject a stub.
type Resolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
}

// HeadlessHost returns the DNS name of the AgentDeploy's headless
// Service. Resolving this name yields one A-record per ready pod.
func HeadlessHost(agentDeploy, namespace string) string {
	return fmt.Sprintf("%s%s.%s.svc.cluster.local", agentDeploy, HeadlessSuffix, namespace)
}

// PodHost returns the DNS name of the i-th replica's pod. The
// AgentDeploy reconciler sets Pod.Spec.Hostname = "<deploy>-<i>" and
// Pod.Spec.Subdomain = "<deploy>-headless", which K8s combines into
// this per-pod DNS name.
func PodHost(agentDeploy, namespace string, replica int) string {
	return fmt.Sprintf("%s-%d.%s%s.%s.svc.cluster.local", agentDeploy, replica, agentDeploy, HeadlessSuffix, namespace)
}

// Discover returns the list of pod DNS names to scrape for the given
// AgentDeploy. It first queries the headless Service to count ready
// pods (N), then constructs the per-pod hostnames "<deploy>-0..N-1".
//
// AgentDeploy guarantees contiguous replica indices [0, desired) at
// steady state. During rolling restarts, the gap (e.g. replica 2
// terminating) may briefly drop N below the desired count; the daemon
// scrapes whatever DNS reports and tolerates per-pod scrape failures.
//
// Returns an empty slice (not an error) when the headless Service has
// no ready endpoints — common during AgentDeploy initial bring-up.
func Discover(ctx context.Context, r Resolver, agentDeploy, namespace string) ([]string, error) {
	host := HeadlessHost(agentDeploy, namespace)
	ips, err := r.LookupHost(ctx, host)
	if err != nil {
		// NXDOMAIN means the headless Service exists but no pods are
		// ready yet. Treat as "zero replicas," same as the scaled-to-
		// zero case — controllers downstream see "rate unavailable"
		// rather than a hard scrape error.
		if isNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}
	n := len(ips)
	out := make([]string, n)
	for i := range n {
		out[i] = PodHost(agentDeploy, namespace, i)
	}
	return out, nil
}

func isNotFound(err error) bool {
	if dnsErr, ok := errors.AsType[*net.DNSError](err); ok {
		return dnsErr.IsNotFound
	}
	return false
}
