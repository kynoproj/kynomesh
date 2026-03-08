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

import "encoding/json"

func MustJSON(in any) string {
	data, err := json.Marshal(in)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// MustUnJSON unmarshalls JSON or panics.
// v - must be []byte or string
// in - must be a pointer.
func MustUnJSON(v any, in any) {
	switch data := v.(type) {
	case []byte:
		if err := json.Unmarshal(data, in); err != nil {
			panic(err)
		}
	case string:
		MustUnJSON([]byte(data), in)
	default:
		panic("unknown type")
	}
}
