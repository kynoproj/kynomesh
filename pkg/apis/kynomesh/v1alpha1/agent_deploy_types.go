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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:validation:Enum="";Running;Paused;Failed
type AgentDeployPhase string

const (
	AgentDeployPhaseUnknown AgentDeployPhase = ""
	AgentDeployPhaseRunning AgentDeployPhase = "Running"
	AgentDeployPhaseFailed  AgentDeployPhase = "Failed"

	// AgentDeployConditionDeployed has the status True when the AgentDeploy related sub resources are deployed.
	AgentDeployConditionDeployed ConditionType = "Deployed"
	// AgentDeployConditionPodsHealthy has the status True when all the AgentDeploy pods are healthy.
	AgentDeployConditionPodsHealthy ConditionType = "PodsHealthy"
)

type AgentDeploy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Spec AgentDeploySpec `json:"spec" protobuf:"bytes,2,opt,name=spec"`
	// +optional
	Status AgentDeployStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

type AgentDeploySpec struct {
	AbstractAgentDeploy `json:",inline" protobuf:"bytes,1,opt,name=abstractAgentDeploy"`
	// +kubebuilder:default=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty" protobuf:"varint,2,opt,name=replicas"`
}

type AbstractAgentDeploy struct {
	Name string `json:"name" protobuf:"bytes,1,opt,name=name"`
	// +optional
	AbstractPodTemplate `json:",inline" protobuf:"bytes,2,opt,name=abstractPodTemplate"`
	// Container template for the main container.
	// +optional
	ContainerTemplate *ContainerTemplate `json:"containerTemplate,omitempty" protobuf:"bytes,3,opt,name=containerTemplate"`
	// +optional
	// +patchStrategy=merge
	// +patchMergeKey=name
	Volumes []corev1.Volume `json:"volumes,omitempty" patchStrategy:"merge" patchMergeKey:"name" protobuf:"bytes,4,rep,name=volumes"`
	// Settings for autoscaling
	// +optional
	Scale Scale `json:"scale,omitempty" protobuf:"bytes,5,opt,name=scale"`
	// List of customized init containers belonging to the pod.
	// More info: https://kubernetes.io/docs/concepts/workloads/pods/init-containers/
	// +optional
	InitContainers []corev1.Container `json:"initContainers,omitempty" protobuf:"bytes,6,rep,name=initContainers"`
	// List of customized sidecar containers belonging to the pod.
	// +optional
	Sidecars []corev1.Container `json:"sidecars,omitempty" protobuf:"bytes,7,rep,name=sidecars"`
	// The strategy to use to replace existing pods with new ones.
	// +kubebuilder:default={"type": "RollingUpdate", "rollingUpdate": {"maxUnavailable": "25%"}}
	// +optional
	UpdateStrategy UpdateStrategy `json:"updateStrategy,omitempty" protobuf:"bytes,8,opt,name=updateStrategy"`
}

type AgentDeployStatus struct {
	Status `json:",inline" protobuf:"bytes,1,opt,name=status"`
	// +optional
	Phase AgentDeployPhase `json:"phase" protobuf:"bytes,2,opt,name=phase,casttype=AgentDeployPhase"`
	// Total number of non-terminated pods targeted by this AgentDeploy (their labels match the selector).
	// +optional
	Replicas uint32 `json:"replicas" protobuf:"varint,3,opt,name=replicas"`
	// The number of desired replicas.
	// +optional
	DesiredReplicas uint32 `json:"desiredReplicas" protobuf:"varint,4,opt,name=desiredReplicas"`
	// +optional
	Selector string `json:"selector,omitempty" protobuf:"bytes,5,opt,name=selector"`
	// +optional
	Reason string `json:"reason,omitempty" protobuf:"bytes,6,opt,name=reason"`
	// +optional
	Message string `json:"message,omitempty" protobuf:"bytes,7,opt,name=message"`
	// Time of last scaling operation.
	// +optional
	LastScaledAt metav1.Time `json:"lastScaledAt,omitempty" protobuf:"bytes,8,opt,name=lastScaledAt"`
	// The generation observed by the AgentDeploy controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty" protobuf:"varint,9,opt,name=observedGeneration"`
	// The number of pods targeted by this AgentDeploy with a Ready Condition.
	// +optional
	ReadyReplicas uint32 `json:"readyReplicas,omitempty" protobuf:"varint,10,opt,name=readyReplicas"`
	// The number of Pods created by the controller from the AgentDeploy version indicated by updateHash.
	UpdatedReplicas uint32 `json:"updatedReplicas,omitempty" protobuf:"varint,11,opt,name=updatedReplicas"`
	// The number of ready Pods created by the controller from the AgentDeploy version indicated by updateHash.
	UpdatedReadyReplicas uint32 `json:"updatedReadyReplicas,omitempty" protobuf:"varint,12,opt,name=updatedReadyReplicas"`
	// If not empty, indicates the current version of the AgentDeploy used to generate Pods.
	CurrentHash string `json:"currentHash,omitempty" protobuf:"bytes,13,opt,name=currentHash"`
	// If not empty, indicates the updated version of the AgentDeploy used to generate Pods.
	UpdateHash string `json:"updateHash,omitempty" protobuf:"bytes,14,opt,name=updateHash"`
}

type AgentDeployTemplate struct {
	// +optional
	AbstractPodTemplate `json:",inline" protobuf:"bytes,1,opt,name=abstractPodTemplate"`
	// Template for the AgentDeploy kyno container
	// +optional
	ContainerTemplate *ContainerTemplate `json:"containerTemplate,omitempty" protobuf:"bytes,2,opt,name=containerTemplate"`
}
