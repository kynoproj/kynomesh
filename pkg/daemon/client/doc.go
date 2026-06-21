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

// Package client provides clients for the per-AgentSet metrics
// daemon. The controller (or any other in-cluster caller) uses one
// of these to talk to a daemon without caring whether transport is
// gRPC or REST.
//
// Two clients are available:
//
//  1. gRPC — preferred for in-cluster calls.
//     NewGRPCClient(address)
//
//  2. REST — useful for debugging with curl-friendly clients.
//     NewRESTClient(address)
//
// Both satisfy DaemonClient and accept addresses in the form
// "host:port" (the REST client prepends "https://" if missing).
package client
