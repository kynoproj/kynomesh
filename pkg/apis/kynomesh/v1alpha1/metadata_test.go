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

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetadata_ZeroValue(t *testing.T) {
	m := Metadata{}

	assert.Nil(t, m.Annotations)
	assert.Nil(t, m.Labels)
	assert.Empty(t, m.Annotations)
	assert.Empty(t, m.Labels)
}

func TestMetadata_FieldAssignment(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		labels      map[string]string
	}{
		{
			name:        "both fields set",
			annotations: map[string]string{"a1": "v1", "a2": "v2"},
			labels:      map[string]string{"l1": "v1", "l2": "v2"},
		},
		{
			name:        "only annotations set",
			annotations: map[string]string{"a1": "v1"},
			labels:      nil,
		},
		{
			name:        "only labels set",
			annotations: nil,
			labels:      map[string]string{"l1": "v1"},
		},
		{
			name:        "empty maps",
			annotations: map[string]string{},
			labels:      map[string]string{},
		},
		{
			name:        "both nil",
			annotations: nil,
			labels:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Metadata{
				Annotations: tt.annotations,
				Labels:      tt.labels,
			}

			assert.Equal(t, tt.annotations, m.Annotations)
			assert.Equal(t, tt.labels, m.Labels)
		})
	}
}

func TestMetadata_JSONMarshal(t *testing.T) {
	tests := []struct {
		name     string
		metadata Metadata
		expected string
	}{
		{
			name:     "empty metadata omits all fields",
			metadata: Metadata{},
			expected: `{}`,
		},
		{
			name: "annotations only",
			metadata: Metadata{
				Annotations: map[string]string{"key": "value"},
			},
			expected: `{"annotations":{"key":"value"}}`,
		},
		{
			name: "labels only",
			metadata: Metadata{
				Labels: map[string]string{"app": "kynomesh"},
			},
			expected: `{"labels":{"app":"kynomesh"}}`,
		},
		{
			name: "both annotations and labels",
			metadata: Metadata{
				Annotations: map[string]string{"a": "1"},
				Labels:      map[string]string{"l": "2"},
			},
			expected: `{"annotations":{"a":"1"},"labels":{"l":"2"}}`,
		},
		{
			name: "empty maps are omitted",
			metadata: Metadata{
				Annotations: map[string]string{},
				Labels:      map[string]string{},
			},
			expected: `{}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.metadata)
			require.NoError(t, err)
			assert.JSONEq(t, tt.expected, string(data))
		})
	}
}

func TestMetadata_JSONUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected Metadata
	}{
		{
			name:     "empty object",
			input:    `{}`,
			expected: Metadata{},
		},
		{
			name:  "annotations only",
			input: `{"annotations":{"k":"v"}}`,
			expected: Metadata{
				Annotations: map[string]string{"k": "v"},
			},
		},
		{
			name:  "labels only",
			input: `{"labels":{"app":"kynomesh"}}`,
			expected: Metadata{
				Labels: map[string]string{"app": "kynomesh"},
			},
		},
		{
			name:  "both fields populated",
			input: `{"annotations":{"a":"1"},"labels":{"l":"2"}}`,
			expected: Metadata{
				Annotations: map[string]string{"a": "1"},
				Labels:      map[string]string{"l": "2"},
			},
		},
		{
			name:  "unknown fields ignored",
			input: `{"annotations":{"a":"1"},"unknown":"ignored"}`,
			expected: Metadata{
				Annotations: map[string]string{"a": "1"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Metadata
			err := json.Unmarshal([]byte(tt.input), &got)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestMetadata_JSONRoundTrip(t *testing.T) {
	original := Metadata{
		Annotations: map[string]string{
			"prometheus.io/scrape": "true",
			"prometheus.io/port":   "9090",
		},
		Labels: map[string]string{
			"app.kubernetes.io/name":      "kynomesh",
			"app.kubernetes.io/component": "agent",
		},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded Metadata
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, original, decoded)
}

func TestMetadata_JSONUnmarshalInvalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "malformed JSON",
			input: `{"annotations":`,
		},
		{
			name:  "wrong type for annotations",
			input: `{"annotations":"not-a-map"}`,
		},
		{
			name:  "wrong type for labels",
			input: `{"labels":[1,2,3]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m Metadata
			err := json.Unmarshal([]byte(tt.input), &m)
			assert.Error(t, err)
		})
	}
}
