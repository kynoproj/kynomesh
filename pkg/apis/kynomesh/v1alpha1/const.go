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
	KeyComponent        = "app.kubernetes.io/component"
	KeyPartOf           = "app.kubernetes.io/part-of"
	KeyManagedBy        = "app.kubernetes.io/managed-by"
	KeyAppName          = "app.kubernetes.io/name"
	KeyHash             = "kynomesh.kyno.sh/hash" // hash of the object
	KeyAgentSetName     = "kynomesh.kyno.sh/agentset-name"
	KeyAgentDeployName  = "kynomesh.kyno.sh/agentdeploy-name"
	KeyReplica          = "kynomesh.kyno.sh/replica"
	KeyServing          = "kynomesh.kyno.sh/serving"
	KeyEntry            = "kynomesh.kyno.sh/entry"
	KeyServiceKind      = "kynomesh.kyno.sh/service-kind"
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
	EnvTerminationGraceSeconds     = "KYNOMESH_TERMINATION_GRACE_SECONDS"

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

	// Service kind label values (for KeyServiceKind)
	ServiceKindHeadless  = "headless"
	ServiceKindClusterIP = "clusterip"

	// Controller names
	ControllerAgentDeploy = "agentdeploy-controller"
	ControllerAgentSet    = "agentset-controller"

	// AgentBrokerPort is the port the broker listens on inside an AgentDeploy
	// pod, shared by all three A2A transports (JSON-RPC, REST, gRPC).
	AgentBrokerPort = 8490
	// AgentBrokerIntrospectionPort is a separate port the broker listens on
	// for observability and probes (/metrics, /healthz, /readyz).
	AgentBrokerIntrospectionPort = 8491
	// DefaultTerminationGracePeriodSeconds is the grace period stamped on
	// AgentDeploy pods when the user sets none. It is sized for agentic
	// workloads: the broker's preStop drain waits for long-running in-flight
	// requests to finish, and this is the ceiling for (drain + post-SIGTERM
	// shutdown).
	DefaultTerminationGracePeriodSeconds int64 = 120
	// DaemonAPIPort is the port the per-AgentSet daemon listens on for
	// gRPC + REST API traffic, multiplexed on a single TLS listener.
	DaemonAPIPort = 9432
	// DaemonMetricsPort is the port the daemon listens on for its own
	// /metrics endpoint.
	DaemonMetricsPort = 9433

	VolumeNameKynomeshRun = "kynomesh-run"                        // Volume name of /var/run/kynomesh
	KynomeshRunPath       = "/var/run/kynomesh"                   // Volume mount path
	BrokerHTTPSocketPath  = KynomeshRunPath + "/broker-http.sock" // UDS socket the agent's HTTP server listens on and the broker connects to
	BrokerGRPCSocketPath  = KynomeshRunPath + "/broker-grpc.sock" // UDS socket the agent's gRPC server listens on and the broker connects to
	TopologyFilePath      = KynomeshRunPath + "/topology.json"    // Topology file path
	ServerInfoFilePath    = KynomeshRunPath + "/server-info"      // Agent server-info file (written by the agent at startup)
	ProbeBinaryPath       = KynomeshRunPath + "/bin/kynoprobe"    // Static probe binary copied by init-runtime; used by agent container probes
	ProbeBinaryImagePath  = "/bin/kynoprobe"                      // The probe binary lives inside the kynomesh image
	KynomeshBinaryPath    = "/bin/kynomesh"                       // The main binary lives inside the kynomesh image
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

// Broker container probe timing defaults.
const (
	DefaultBrokerReadinessInitialDelaySec  int32 = 1
	DefaultBrokerReadinessPeriodSec        int32 = 10
	DefaultBrokerReadinessTimeoutSec       int32 = 2
	DefaultBrokerReadinessFailureThreshold int32 = 3
	DefaultBrokerReadinessSuccessThreshold int32 = 1

	DefaultBrokerLivenessInitialDelaySec  int32 = 30
	DefaultBrokerLivenessPeriodSec        int32 = 15
	DefaultBrokerLivenessTimeoutSec       int32 = 3
	DefaultBrokerLivenessFailureThreshold int32 = 6
	DefaultBrokerLivenessSuccessThreshold int32 = 1
)

// Autoscaling defaults.
const (
	// DefaultMinReplicas is the replica floor when scale.min is unset. An
	// AgentDeploy never runs fewer than this.
	DefaultMinReplicas int32 = 1
	// DefaultMaxReplicas is the replica ceiling when scale.max is unset. Both
	// the reconcile (initial/clamped replica count) and the autoscaler bound to
	// this so they agree on the upper limit.
	DefaultMaxReplicas int32 = 50
	// DefaultScaleUpCooldownSeconds is the wait after a scale before another
	// scale-up, when scale.scaleUpCooldownSeconds is unset.
	DefaultScaleUpCooldownSeconds uint32 = 90
	// DefaultScaleDownCooldownSeconds is the wait after a scale before another
	// scale-down, when scale.scaleDownCooldownSeconds is unset.
	DefaultScaleDownCooldownSeconds uint32 = 90
	// DefaultReplicasPerScaleUp caps the replica change per scale-up step when
	// scale.replicasPerScaleUp is unset.
	DefaultReplicasPerScaleUp uint32 = 2
	// DefaultReplicasPerScaleDown caps the replica change per scale-down step
	// when scale.replicasPerScaleDown is unset.
	DefaultReplicasPerScaleDown uint32 = 2
	// DefaultTargetSaturationPercentage is the steady-state fraction (1-100) of
	// a replica's learned capacity to run at, when
	// scale.targetSaturationPercentage is unset.
	DefaultTargetSaturationPercentage uint32 = 80
)
