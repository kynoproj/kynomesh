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
	"net/http/httputil"
	"net/url"
	"sync/atomic"
)

// NewJSONRPCReverseProxy returns the JSON-RPC pass-through handler.
func NewJSONRPCReverseProxy(backendBase *url.URL, counters *Counters) http.Handler {
	return newReverseProxy(backendBase, &counters.jsonRPC)
}

// NewRESTReverseProxy returns the REST pass-through handler.
func NewRESTReverseProxy(backendBase *url.URL, counters *Counters) http.Handler {
	return newReverseProxy(backendBase, &counters.rest)
}

// newReverseProxy is the shared httputil.NewSingleHostReverseProxy
// builder.
func newReverseProxy(backendBase *url.URL, counter *atomic.Int64) http.Handler {
	rp := httputil.NewSingleHostReverseProxy(backendBase)
	return wrapHTTP(counter, rp)
}
