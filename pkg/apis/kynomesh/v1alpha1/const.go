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
	KeyServing          = "kynomesh.kyno.sh/serving"
	KeyEntry            = "kynomesh.kyno.sh/entry"
	KeyDefaultContainer = "kubectl.kubernetes.io/default-container"

	// ENV vars
	EnvNamespace                   = "NAMESPACE"
	EnvPodName                     = "POD_NAME"
	EnvAgentSetName                = "KYNOMESH_AGENTSET_NAME"
	EnvAgentDeployName             = "KYNOMESH_AGENTDEPLOY_NAME"
	EnvAgentDeployObject           = "KYNOMESH_AGENTDEPLOY_OBJECT"
	EnvLeaderElectionDisabled      = "KYNOMESH_LEADER_ELECTION_DISABLED"
	EnvLeaderElectionLeaseDuration = "KYNOMESH_LEADER_ELECTION_LEASE_DURATION"
	EnvLeaderElectionRenewDeadline = "KYNOMESH_LEADER_ELECTION_RENEW_DEADLINE"
	EnvLeaderElectionRetryPeriod   = "KYNOMESH_LEADER_ELECTION_RETRY_PERIOD"
	EnvImagePullPolicy             = "KYNOMESH_IMAGE_PULL_POLICY"
	EnvPPROFEnabled                = "KYNOMESH_PPROF_ENABLED"
	EnvAgentSetAgentDeploys        = "KYNOMESH_AGENTSET_AGENTDEPLOYS"

	// Container names
	ContainerNameAgent       = "agent"
	ContainerNameAgentBroker = "broker"
	ContainerNameController  = "controller-manager"
	ContainerNameDaemon      = "daemon"
	ContainerNameInitRuntime = "init-runtime"

	// Component label values (for KeyComponent)
	ComponentAgent             = "agent"
	ComponentControllerManager = "controller-manager"
	ComponentDaemon            = "daemon"

	// Controller names
	ControllerAgentDeploy = "agentdeploy-controller"
	ControllerAgentSet    = "agentset-controller"

	// AgentBrokerPort is the port the broker listens on inside an AgentDeploy
	// pod, shared by all three A2A transports (JSON-RPC, REST, gRPC).
	AgentBrokerPort = 8490
	// AgentBrokerIntrospectionPort is a separate port the broker listens on
	// for observability and probes (/metrics, /healthz, /readyz).
	AgentBrokerIntrospectionPort = 8491
	// DaemonAPIPort is the port the per-AgentSet daemon listens on for
	// gRPC + REST API traffic, multiplexed on a single TLS listener.
	DaemonAPIPort = 9432
	// DaemonMetricsPort is the port the daemon listens on for its own
	// /metrics endpoint.
	DaemonMetricsPort = 9433

	VolumeNameKynomeshRun = "kynomesh-run"                     // Volume name of /var/run/kynomesh
	KynomeshRunPath       = "/var/run/kynomesh"                // Volume mount path
	BrokerSocketPath      = KynomeshRunPath + "/broker.sock"   // UDS socket the agent listens on and the broker connects to
	TopologyFilePath      = KynomeshRunPath + "/topology.json" // Topology file path
	ServerInfoFilePath    = KynomeshRunPath + "/server-info"   // Agent server-info file (written by the agent at startup)
	ProbeBinaryPath       = KynomeshRunPath + "/bin/kynoprobe" // Static probe binary copied by init-runtime; used by agent container probes
	ProbeBinaryImagePath  = "/bin/kynoprobe"                   //probe binary lives inside the kynomesh image
)

// Agent container probe timing defaults.
const (
	DefaultAgentReadinessInitialDelaySec  int32 = 1
	DefaultAgentReadinessPeriodSec        int32 = 10
	DefaultAgentReadinessTimeoutSec       int32 = 2
	DefaultAgentReadinessFailureThreshold int32 = 3
	DefaultAgentReadinessSuccessThreshold int32 = 1

	DefaultAgentLivenessInitialDelaySec  int32 = 30
	DefaultAgentLivenessPeriodSec        int32 = 15
	DefaultAgentLivenessTimeoutSec       int32 = 3
	DefaultAgentLivenessFailureThreshold int32 = 6
	DefaultAgentLivenessSuccessThreshold int32 = 1
)
