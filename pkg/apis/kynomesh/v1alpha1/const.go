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

const (
	Project = "kynomesh"

	// label/annotation keys.
	KeyHash             = "kynomesh.kyno.sh/hash" // hash of the object
	KeyComponent        = "app.kubernetes.io/component"
	KeyPartOf           = "app.kubernetes.io/part-of"
	KeyManagedBy        = "app.kubernetes.io/managed-by"
	KeyAppName          = "app.kubernetes.io/name"
	KeyAgentSetName     = "kynomesh.kyno.sh/agentset-name"
	KeyAgentDeployName  = "kynomesh.kyno.sh/agentdeploy-name"
	KeyReplica          = "kynomesh.kyno.sh/replica"
	KeyDefaultContainer = "kubectl.kubernetes.io/default-container"

	// ENV vars
	EnvNamespace                   = "NAMESPACE"
	EnvPodName                     = "POD_NAME"
	EnvAgentDeployObject           = "KYNOMESH_AGENT_DEPLOY_OBJECT"
	EnvAgentEndpoint               = "KYNOMESH_AGENT_ENDPOINT"
	EnvLeaderElectionDisabled      = "KYNOMESH_LEADER_ELECTION_DISABLED"
	EnvLeaderElectionLeaseDuration = "KYNOMESH_LEADER_ELECTION_LEASE_DURATION"
	EnvLeaderElectionRenewDeadline = "KYNOMESH_LEADER_ELECTION_RENEW_DEADLINE"
	EnvLeaderElectionRetryPeriod   = "KYNOMESH_LEADER_ELECTION_RETRY_PERIOD"

	// Container names
	ContainerNameAgent       = "agent"
	ContainerNameAgentBroker = "broker"
	ContainerNameController  = "controller-manager"
	ContainerNameInitSocket  = "init-socket"

	// Component label values (for KeyComponent)
	ComponentAgent             = "agent"
	ComponentControllerManager = "controller-manager"

	// Controller names
	ControllerAgentDeploy = "agentdeploy-controller"
	ControllerAgentSet    = "agentset-controller"

	// AgentBrokerPort is the port the broker listens on inside an AgentDeploy
	// pod, shared by all three A2A transports (JSON-RPC, REST, gRPC).
	// Used by both the controller (to set the broker container's
	// containerPort) and the broker CLI (as its default --port).
	AgentBrokerPort = 9100

	VolumeNameKynomeshRun = "kynomesh-run"                   // Volume name of /var/run/kynowmesh
	PathKynomeshRun       = "/var/run/kynomesh"              // Volume mount path
	BrokerSocketPath      = PathKynomeshRun + "/broker.sock" // Socket path
)
