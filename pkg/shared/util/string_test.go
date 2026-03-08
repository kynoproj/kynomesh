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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRandomString(t *testing.T) {
	str := RandomString(20)
	assert.Equal(t, 20, len(str))
}

func TestRandomLowercaseString(t *testing.T) {
	str := RandomLowerCaseString(20)
	assert.Equal(t, 20, len(str))
	assert.Equal(t, str, strings.ToLower(str))
}

func TestDNS1035(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "lowercase conversion",
			input:    "HElLO",
			expected: "hello",
		},
		{
			name:     "replace special characters",
			input:    "hello#world@123",
			expected: "hello-world-123",
		},
		{
			name:     "multiple consecutive special chars",
			input:    "hello!!!world@@@###123",
			expected: "hello-world-123",
		},
		{
			name:     "spaces and underscores",
			input:    "hello_world space test",
			expected: "hello-world-space-test",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only special characters",
			input:    "@#$%^&*",
			expected: "-",
		},
		{
			name:     "mixed case with numbers and hyphens",
			input:    "My-Cool-Service123",
			expected: "my-cool-service123",
		},
		{
			name:     "unicode characters",
			input:    "héllo→wörld",
			expected: "h-llo-w-rld",
		},
		{
			name:     "empty",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DNS1035(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHashcode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name:     "a",
			input:    "1",
			expected: 2212294583,
		},
		{
			name:     "b",
			input:    "hello world",
			expected: 222957957,
		},
		{
			name:     "c",
			input:    "This is a test",
			expected: 3229261618,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Hashcode(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
