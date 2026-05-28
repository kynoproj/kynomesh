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

package broker

import (
	"net/http"
	"sync/atomic"
)

// Counters tracks in-flight requests per transport. Each transport gets
// its own counter so autoscaling and observability can attribute load to
// the protocol it arrived on (an HTTP/2-streaming gRPC call holds a slot
// for its whole stream lifetime, not just a single request).
type Counters struct {
	jsonRPC atomic.Int64
	rest    atomic.Int64
	grpc    atomic.Int64
}

// JSONRPC returns the current in-flight JSON-RPC request count.
func (c *Counters) JSONRPC() int64 { return c.jsonRPC.Load() }

// REST returns the current in-flight REST request count.
func (c *Counters) REST() int64 { return c.rest.Load() }

// GRPC returns the current in-flight gRPC stream count.
func (c *Counters) GRPC() int64 { return c.grpc.Load() }

// wrapHTTP returns an http.Handler that brackets every request with an
// increment/decrement on the supplied counter. The defer guarantees the
// counter balances even when the handler panics.
func wrapHTTP(counter *atomic.Int64, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter.Add(1)
		defer counter.Add(-1)
		h.ServeHTTP(w, r)
	})
}
