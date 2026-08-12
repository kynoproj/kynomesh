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

package ratelimit

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/kynoproj/kynomesh/pkg/shared/discovery"
	"github.com/kynoproj/kynomesh/pkg/shared/logging"
)

// DefaultDNSPollInterval is how often the DNS-count limiter re-reads the replica
// count. Short enough to rebalance slices within roughly one interval of a scale
// event (plus DNS TTL), cheap enough to poll continuously.
const DefaultDNSPollInterval = 10 * time.Second

// dnsCountLimiter is a fleet-aware Limiter that partitions a global max-in-flight
// cap across the live replicas of an AgentDeploy. Each broker enforces a local
// slice of the global cap; a background loop re-reads the replica count from the
// headless Service DNS and rebalances the slice as the fleet scales.
//
// The bound is soft: DNS records lag scale events by a TTL, and slices are
// rounded up (see sliceFor) so the fleet total can exceed the exact cap under
// full saturation. This is an accepted trade for zero request-path coordination,
// no extra dependency, and a total-in-flight signal that can actually reach the
// cap for autoscaling. See NewLimiter for the strict pod-local variant.
type dnsCountLimiter struct {
	sem *semaphore

	maxInFlight int
	agentDeploy string
	namespace   string
	resolver    discovery.Resolver
}

// NewDNSCountLimiter builds a fleet-aware limiter and returns it alongside a
// start func that runs the rebalancing loop until ctx is cancelled. The caller
// runs start in its own goroutine (`go start(ctx)`); the limiter is usable
// immediately, enforcing the whole cap locally until the first successful DNS
// read narrows it to this replica's slice.
func NewDNSCountLimiter(maxInFlight int, agentDeploy, namespace string, resolver discovery.Resolver) (Limiter, func(ctx context.Context)) {
	l := &dnsCountLimiter{
		// Start at the full cap: before the first DNS read we don't yet know the
		// replica count, so admit up to the global cap rather than reject.
		sem:         newSemaphore(maxInFlight),
		maxInFlight: maxInFlight,
		agentDeploy: agentDeploy,
		namespace:   namespace,
		resolver:    resolver,
	}
	return l, l.run
}

func (l *dnsCountLimiter) Acquire() (func(), bool) { return l.sem.Acquire() }

// run rebalances the local slice on every tick until ctx is cancelled. It does
// one immediate recount so the slice narrows without waiting a full interval.
func (l *dnsCountLimiter) run(ctx context.Context) {
	ticker := time.NewTicker(DefaultDNSPollInterval)
	defer ticker.Stop()
	l.recount(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.recount(ctx)
		}
	}
}

// recount reads the live replica count and resizes the local slice. On a DNS
// error it keeps the current slice.
func (l *dnsCountLimiter) recount(ctx context.Context) {
	logger := logging.FromContext(ctx)
	hosts, err := discovery.Discover(ctx, l.resolver, l.agentDeploy, l.namespace)
	if err != nil {
		logger.Warnw("Rate-limit replica recount failed; keeping current slice",
			zap.String("agentDeploy", l.agentDeploy),
			zap.Int("slice", l.sem.limitValue()),
			zap.Error(err))
		return
	}
	slice := sliceFor(l.maxInFlight, len(hosts))
	l.sem.SetLimit(slice)
}

// sliceFor partitions the global cap across replicas, rounding UP. Ceiling is
// deliberate: the cap exists to protect an external dependency (e.g. an LLM
// quota), and the autoscaler suppresses scale-up once total in-flight reaches
// maxInFlight.
func sliceFor(maxInFlight, replicas int) int {
	if replicas <= 1 {
		return maxInFlight
	}
	return (maxInFlight + replicas - 1) / replicas
}
