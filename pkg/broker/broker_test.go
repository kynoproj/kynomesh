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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAddrHelpers(t *testing.T) {
	assert.Equal(t, "https://broker.local:9100/rpc", JSONRPCAddr("broker.local", 9100))
	assert.Equal(t, "https://broker.local:9101/api", RESTAddr("broker.local", 9101))
	assert.Equal(t, "broker.local:9102", GRPCAddr("broker.local", 9102))
}
