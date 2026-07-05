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
	// TargetSaturationPercentage tunes how aggressively to scale. It is the fraction
	// (1-100) of a replica's learned capacity (the saturation knee) to run at in steady
	// state. The autoscaler learns each replica's capacity from observed load and keeps
	// the fleet near this fraction of it: desired replicas trend toward
	// ceil(totalInflight / (knee * TargetSaturationPercentage/100)).
	// Lower values scale out earlier (safer latency, higher cost); higher values pack
	// tighter (lower cost, higher latency risk). Values above 100 are rejected.
	// Defaults to 80 when unset.
	// +optional
	TargetSaturationPercentage *uint32 `json:"targetSaturationPercentage,omitempty" protobuf:"varint,5,opt,name=targetSaturationPercentage"`
	// ScaleUpCooldownSeconds defines the cooldown seconds after a scaling operation, before a follow-up scaling up.
	// +optional
	ScaleUpCooldownSeconds *uint32 `json:"scaleUpCooldownSeconds,omitempty" protobuf:"varint,6,opt,name=scaleUpCooldownSeconds"`
	// ScaleDownCooldownSeconds defines the cooldown seconds after a scaling operation, before a follow-up scaling down.
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
