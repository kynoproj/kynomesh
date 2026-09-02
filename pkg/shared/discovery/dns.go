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
// the headless Service that the AgentDeploy reconciler maintains.
package discovery

import (
	"context"
	"errors"
	"fmt"
	"net"
)

// HeadlessSuffix matches v1alpha1.AgentDeploy.HeadlessServiceName().
const HeadlessSuffix = "-headless"

// Resolver is a small interface that net.DefaultResolver satisfies;
// tests inject a stub.
type Resolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
}

// HeadlessHost returns the DNS name of the AgentDeploy's headless
// Service.
func HeadlessHost(agentSet, agentDeploy, namespace string) string {
	return fmt.Sprintf("%s-%s%s.%s.svc.cluster.local", agentSet, agentDeploy, HeadlessSuffix, namespace)
}

// PodHost returns the DNS name of the i-th replica's pod.
func PodHost(agentSet, agentDeploy, namespace string, replica int) string {
	return fmt.Sprintf("%s-%s-%d.%s-%s%s.%s.svc.cluster.local", agentSet, agentDeploy, replica, agentSet, agentDeploy, HeadlessSuffix, namespace)
}

// Discover returns the list of pod DNS names to scrape for the given
// AgentDeploy, identified by its AgentSet and its short (Spec.Name) agent
// name — the same two values an AgentSet's Spec.Agents entry carries.
func Discover(ctx context.Context, r Resolver, as, ad, namespace string) ([]string, error) {
	host := HeadlessHost(as, ad, namespace)
	ips, err := r.LookupHost(ctx, host)
	if err != nil {
		// NXDOMAIN means the headless Service exists but no pods are
		// ready yet. Treat as "zero replicas," same as the scaled-to-
		// zero case.
		if isNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("resolve %s (agentSet %s): %w", host, as, err)
	}
	n := len(ips)
	out := make([]string, n)
	for i := range n {
		out[i] = PodHost(as, ad, namespace, i)
	}
	return out, nil
}

func isNotFound(err error) bool {
	if dnsErr, ok := errors.AsType[*net.DNSError](err); ok {
		return dnsErr.IsNotFound
	}
	return false
}
