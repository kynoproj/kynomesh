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

// Scale defines the parameters for autoscaling.
type Scale struct {
	// Whether to disable autoscaling.
	// Set to "true" when using Kubernetes HPA or any other 3rd party autoscaling strategies.
	// +optional
	Disabled bool `json:"disabled,omitempty" protobuf:"bytes,1,opt,name=disabled"`
	// Minimum replicas.
	// +optional
	Min *int32 `json:"min,omitempty" protobuf:"varint,2,opt,name=min"`
	// Maximum replicas.
	// +optional
	Max *int32 `json:"max,omitempty" protobuf:"varint,3,opt,name=max"`
	// Lookback seconds to calculate the average pending messages and processing rate.
	// +optional
	LookbackSeconds *uint32 `json:"lookbackSeconds,omitempty" protobuf:"varint,4,opt,name=lookbackSeconds"`
	// TargetProcessingSeconds is used to tune the aggressiveness of autoscaling for source vertices, it measures how fast
	// you want the vertex to process all the pending messages. Typically increasing the value, which leads to lower processing
	// rate, thus less replicas. It's only effective for source vertices.
	// +optional
	TargetProcessingSeconds *uint32 `json:"targetProcessingSeconds,omitempty" protobuf:"varint,5,opt,name=targetProcessingSeconds"`
	// ScaleUpCooldownSeconds defines the cooldown seconds after a scaling operation, before a follow-up scaling up.
	// It defaults to the CooldownSeconds if not set.
	// +optional
	ScaleUpCooldownSeconds *uint32 `json:"scaleUpCooldownSeconds,omitempty" protobuf:"varint,6,opt,name=scaleUpCooldownSeconds"`
	// ScaleDownCooldownSeconds defines the cooldown seconds after a scaling operation, before a follow-up scaling down.
	// It defaults to the CooldownSeconds if not set.
	// +optional
	ScaleDownCooldownSeconds *uint32 `json:"scaleDownCooldownSeconds,omitempty" protobuf:"varint,7,opt,name=scaleDownCooldownSeconds"`
	// ReplicasPerScaleUp defines the number of maximum replicas that can be changed in a single scaled up operation.
	// The is use to prevent from too aggressive scaling up operations
	// +optional
	ReplicasPerScaleUp *uint32 `json:"replicasPerScaleUp,omitempty" protobuf:"varint,8,opt,name=replicasPerScaleUp"`
	// ReplicasPerScaleDown defines the number of maximum replicas that can be changed in a single scaled down operation.
	// The is use to prevent from too aggressive scaling down operations
	// +optional
	ReplicasPerScaleDown *uint32 `json:"replicasPerScaleDown,omitempty" protobuf:"varint,9,opt,name=replicasPerScaleDown"`
}
