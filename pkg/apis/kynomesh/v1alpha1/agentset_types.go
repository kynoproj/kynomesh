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
// +kubebuilder:printcolumn:name="Managed Agents",type=integer,JSONPath=`.status.agentCount`
// +kubebuilder:printcolumn:name="External Agents",type=integer,JSONPath=`.status.externalAgentCount`
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
	// Pattern describes how the agents in this AgentSet are wired together.
	//
	//   - Supervisor: Entry sees every other agent; non-entry agents see
	//     no peers. Aliases in the wider ecosystem include "manager",
	//     "orchestrator-worker", and "subagents".
	//   - Handoff: every agent sees every other agent. Aliases include
	//     "swarm" and "network".
	//   - Sequential: each agent sees only the next one in declaration
	//     order; Entry must be agents[0].
	// +kubebuilder:validation:Enum=Supervisor;Handoff;Sequential
	// +kubebuilder:validation:Required
	Pattern AgentPattern `json:"pattern" protobuf:"bytes,1,opt,name=pattern,casttype=AgentPattern"`
	// Entry is the name of the agent external callers reach first. Must
	// match one of agents[].name. For Sequential it must be agents[0].
	// +kubebuilder:validation:Required
	Entry string `json:"entry" protobuf:"bytes,2,opt,name=entry"`
	// +patchStrategy=merge
	// +patchMergeKey=name
	// +kubebuilder:validation:MinItems=1
	Agents []AbstractAgentDeploy `json:"agents" patchStrategy:"merge" patchMergeKey:"name" protobuf:"bytes,3,rep,name=agents"`
	// Templates are used to customize additional kubernetes resources required for the AgentSet
	// +optional
	Templates *Templates `json:"templates,omitempty" protobuf:"bytes,4,opt,name=templates"`
	// ExternalAgents references agents this AgentSet does not deploy, scale, or
	// roll. They may never be Entry.
	// +patchStrategy=merge
	// +patchMergeKey=name
	// +optional
	ExternalAgents []ExternalAgentRef `json:"externalAgents,omitempty" patchStrategy:"merge" patchMergeKey:"name" protobuf:"bytes,5,rep,name=externalAgents"`
}

// ExternalAgentRef is a reference to an existing agent this AgentSet does not
// own — another AgentSet's agent, or any A2A endpoint reachable at a URL.
type ExternalAgentRef struct {
	// Name is the short agent name used for peer lookup and topology, matching
	// how AbstractAgentDeploy.Name works for managed agents.
	// +kubebuilder:validation:Required
	Name string `json:"name" protobuf:"bytes,1,opt,name=name"`
	// URL is the full URL of the external agent's broker/endpoint.
	// +kubebuilder:validation:Required
	URL string `json:"url" protobuf:"bytes,2,opt,name=url"`
}

// AgentPattern is the message-routing shape of an AgentSet.
// +kubebuilder:validation:Enum=Supervisor;Handoff;Sequential
type AgentPattern string

const (
	// AgentPatternSupervisor: Entry sees all workers; workers see no peers.
	AgentPatternSupervisor AgentPattern = "Supervisor"
	// AgentPatternHandoff: every agent sees every other agent.
	AgentPatternHandoff AgentPattern = "Handoff"
	// AgentPatternSequential: each agent sees only the next agent in
	// declaration order; Entry must be agents[0].
	AgentPatternSequential AgentPattern = "Sequential"
)

type Templates struct {
	AgentDeployTemplate *AgentDeployTemplate `json:"agent,omitempty" protobuf:"bytes,1,opt,name=agent"`
	DaemonTemplate      *DaemonTemplate      `json:"daemon,omitempty" protobuf:"bytes,2,opt,name=daemon"`
}

// DaemonTemplate customizes the per-AgentSet daemon Deployment's pod.
type DaemonTemplate struct {
	AbstractPodTemplate `json:",inline" protobuf:"bytes,1,opt,name=abstractPodTemplate"`
	// Container for the daemon container (resources, env, ...).
	Container *ContainerTemplate `json:"container,omitempty" protobuf:"bytes,2,opt,name=container"`
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
	// +optional
	ExternalAgentCount *uint32 `json:"externalAgentCount,omitempty" protobuf:"varint,6,opt,name=externalAgentCount"`
	// The generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty" protobuf:"varint,7,opt,name=observedGeneration"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type AgentSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Items           []AgentSet `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// EntryServiceSuffix is appended to the AgentSet name to form the entry
// service's name.
const EntryServiceSuffix = "ingress"

// EntryServiceName returns the name of the ClusterIP Service that routes
// to pods of the entry AgentDeploy of this AgentSet.
func (as *AgentSet) EntryServiceName() string {
	return as.Name + "-" + EntryServiceSuffix
}

// DaemonSuffix is appended to the AgentSet name to form the per-
// AgentSet daemon Deployment and Service names.
const DaemonSuffix = "daemon"

// DaemonName returns the name shared by the per-AgentSet daemon's
// Deployment and Service.
func (as *AgentSet) DaemonName() string {
	return as.Name + "-" + DaemonSuffix
}

// ChildAgentDeployName returns the metadata name the AgentSet
// reconciler assigns to the AgentDeploy for the given agent.
func (as *AgentSet) ChildAgentDeployName(agentName string) string {
	return as.Name + "-" + agentName
}
