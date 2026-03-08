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

package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMustHash(t *testing.T) {
	assert.Equal(t, "6446d58d6dfafd58586d3ea85a53f4a6b3cc057f933a22bb58e188a74ac8f663", MustHash([]byte("boo")))
	assert.Equal(t, "6446d58d6dfafd58586d3ea85a53f4a6b3cc057f933a22bb58e188a74ac8f663", MustHash("boo"))
	assert.Equal(t, "fc6334b4bddccbb9d64802eb15ccc9b9c123ba8c574b61d0106c246592087a42", MustHash(
		struct {
			A string
			B string
		}{A: "a1001", B: "b1001"}))
}
