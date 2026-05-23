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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:validation:Enum="";Running;Failed;Deleting
type AgentSetPhase string

const (
	AgentSetPhaseUnknown  AgentSetPhase = ""
	AgentSetPhaseRunning  AgentSetPhase = "Running"
	AgentSetPhaseFailed   AgentSetPhase = "Failed"
	AgentSetPhaseDeleting AgentSetPhase = "Deleting"

	// AgentSetConditionConfigured has the status True when the AgentSet
	// has valid configuration.
	AgentSetConditionConfigured ConditionType = "Configured"
	// AgentSetConditionDeployed has the status True when the AgentSet
	// has its AgentDeployments created.
	AgentSetConditionDeployed ConditionType = "Deployed"

	// AgentSetConditionAgentsHealthy has the status True when all the agents are healthy.
	AgentSetConditionAgentsHealthy ConditionType = "AgentsHealthy"
)

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=as
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Agents",type=integer,JSONPath=`.status.AgentCount`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:printcolumn:name="Message",type=string,JSONPath=`.status.message`
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true
type AgentSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Spec AgentSetSpec `json:"spec" protobuf:"bytes,2,opt,name=spec"`
	// +optional
	Status AgentSetStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

type AgentSetSpec struct {
	// +patchStrategy=merge
	// +patchMergeKey=name
	Agents []AbstractAgentDeploy `json:"agents,omitempty" patchStrategy:"merge" patchMergeKey:"name" protobuf:"bytes,1,rep,name=agents"`
	// Templates are used to customize additional kubernetes resources required for the Pipeline
	// +optional
	Templates *Templates `json:"templates,omitempty" protobuf:"bytes,2,opt,name=templates"`
}

type Templates struct {
	AgentDeployTemplate *AgentDeployTemplate `json:"agent,omitempty" protobuf:"bytes,1,opt,name=agent"`
}

type AgentSetStatus struct {
	Status `json:",inline" protobuf:"bytes,1,opt,name=status"`
	// +optional
	Phase AgentSetPhase `json:"phase,omitempty" protobuf:"bytes,2,opt,name=phase,casttype=AgentSetPhase"`
	// +optional
	Message string `json:"message,omitempty" protobuf:"bytes,3,opt,name=message"`
	// +optional
	LastUpdated metav1.Time `json:"lastUpdated,omitempty" protobuf:"bytes,4,opt,name=lastUpdated"`
	// +optional
	AgentCount *uint32 `json:"agentCount,omitempty" protobuf:"varint,5,opt,name=agentCount"`
	// The generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty" protobuf:"varint,6,opt,name=observedGeneration"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type AgentSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Items           []AgentSet `json:"items" protobuf:"bytes,2,rep,name=items"`
}
