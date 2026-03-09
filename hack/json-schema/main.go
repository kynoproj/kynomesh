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

package main

import (
	"encoding/json"
	"fmt"
	"os"
)

const (
	group        = "kynomesh.kyno.sh"
	version      = "v1alpha1"
	agentSetKind = "AgentSet"
)

type obj = map[string]interface{}

func main() {
	swagger := obj{}
	{
		f, err := os.Open("api/openapi-spec/swagger.json")
		if err != nil {
			panic(err)
		}
		err = json.NewDecoder(f).Decode(&swagger)
		if err != nil {
			panic(err)
		}
	}
	{
		crdKinds := []string{
			agentSetKind,
		}
		definitions := swagger["definitions"]
		oneOf := make([]obj, 0, len(crdKinds))
		for _, kind := range crdKinds {
			definitionKey := fmt.Sprintf("sh.kyno.kynomesh.%s.%s", version, kind)
			v := definitions.(obj)[definitionKey].(obj)
			v["x-kubernetes-group-version-kind"] = []obj{
				{
					"group":   group,
					"kind":    kind,
					"version": version,
				},
			}
			props := v["properties"].(obj)
			props["apiVersion"].(obj)["const"] = fmt.Sprintf("%s/%s", group, version)
			props["kind"].(obj)["const"] = kind
			oneOf = append(oneOf, obj{"$ref": "#/definitions/" + definitionKey})
		}

		schema := obj{
			"$id":         "http://sh.kyno.kynomesh/kynomesh.json",
			"$schema":     "http://json-schema.org/schema#",
			"type":        "object",
			"oneOf":       oneOf,
			"definitions": definitions,
		}
		f, err := os.Create("api/json-schema/schema.json")
		if err != nil {
			panic(err)
		}

		e := json.NewEncoder(f)
		e.SetIndent("", "  ")
		err = e.Encode(schema)
		if err != nil {
			panic(err)
		}

		err = f.Close()
		if err != nil {
			panic(err)
		}
	}
}
