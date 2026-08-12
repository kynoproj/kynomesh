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

package v1alpha1

// RateLimit configures broker-side admission control for an agent.
type RateLimit struct {
	// MaxInFlight caps the number of concurrent in-flight A2A requests the
	// broker admits. Requests beyond the cap are rejected immediately (HTTP 429
	// with Retry-After, gRPC RESOURCE_EXHAUSTED) instead of being queued.
	// Only A2A traffic (JSON-RPC, REST, gRPC) counts against the cap;
	// passthrough (non-A2A) traffic is not gated.
	// 0 or unset means unlimited.
	// +optional
	MaxInFlight *int32 `json:"maxInFlight,omitempty" protobuf:"varint,1,opt,name=maxInFlight"`
}
