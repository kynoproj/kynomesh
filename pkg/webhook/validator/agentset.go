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

package validator

import (
	"context"

	admissionv1 "k8s.io/api/admission/v1"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	"github.com/kynoproj/kynomesh/pkg/reconciler/validator"
)

type agentSetValidator struct {
	oldAgentSet *kmv1.AgentSet
	newAgentSet *kmv1.AgentSet
}

func NewAgentSetValidator(old, new *kmv1.AgentSet) Validator {
	return &agentSetValidator{oldAgentSet: old, newAgentSet: new}
}

func (v *agentSetValidator) ValidateCreate(_ context.Context) *admissionv1.AdmissionResponse {
	if err := validator.ValidateAgentSet(v.newAgentSet); err != nil {
		return DeniedResponse(err.Error())
	}
	return AllowedResponse()
}

func (v *agentSetValidator) ValidateUpdate(_ context.Context) *admissionv1.AdmissionResponse {
	// check the new AgentSet is valid
	if err := validator.ValidateAgentSet(v.newAgentSet); err != nil {
		return DeniedResponse(err.Error())
	}
	// Anything else comes here
	return AllowedResponse()
}
