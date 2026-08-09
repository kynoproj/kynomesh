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

package agentdeploy

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

func TestBuildPodSpec_BrokerIsFirstMainContainer(t *testing.T) {
	// Main containers are [broker, ...user sidecars]. The agent lives
	// in init containers as a K8s-native sidecar (restartPolicy=Always).
	ad := newAgentDeploy("greeter", 1)
	ps := buildPodSpec(ad, testBrokerImage, "")

	require.GreaterOrEqual(t, len(ps.Containers), 1)
	broker := ps.Containers[0]
	assert.Equal(t, kmv1.ContainerNameAgentBroker, broker.Name)
	assert.Equal(t, testBrokerImage, broker.Image)
	assert.Empty(t, broker.Command, "Command must be unset so the Dockerfile ENTRYPOINT is used")
	assert.Equal(t, []string{"broker"}, broker.Args)
	// Broker exposes two ports: the main A2A port and the introspection
	// port for /metrics, /healthz, /readyz.
	require.Len(t, broker.Ports, 2)
	gotPorts := map[string]int32{}
	for _, p := range broker.Ports {
		gotPorts[p.Name] = p.ContainerPort
	}
	assert.Equal(t, int32(kmv1.AgentBrokerPort), gotPorts["broker"])
	assert.Equal(t, int32(kmv1.AgentBrokerIntrospectionPort), gotPorts["introspect"])

	// The agent must NOT appear in main containers.
	for _, c := range ps.Containers {
		assert.NotEqual(t, kmv1.ContainerNameAgent, c.Name,
			"agent must live in init containers as a sidecar, not in main containers")
	}
}

func TestBuildPodSpec_BrokerImageInHash(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	a := buildPodSpec(ad, "image-a", "")
	b := buildPodSpec(ad, "image-b", "")
	// Different broker images must produce different pod specs so the
	// hash-based drift detector rolls pods when the controller is upgraded.
	assert.NotEqual(t, a, b)
}

func TestBuildPodSpec_PreservesUserSidecarsAfterBroker(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	ad.Spec.Sidecars = []corev1.Container{{Name: "user-sidecar", Image: "busybox"}}
	ps := buildPodSpec(ad, testBrokerImage, "")

	require.Len(t, ps.Containers, 2)
	assert.Equal(t, kmv1.ContainerNameAgentBroker, ps.Containers[0].Name)
	assert.Equal(t, "user-sidecar", ps.Containers[1].Name)
}

func TestBuildPodSpec_AgentRunsAsSidecarInitContainer(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	ps := buildPodSpec(ad, testBrokerImage, "")

	// Init containers: [init-runtime, agent (sidecar)]
	require.GreaterOrEqual(t, len(ps.InitContainers), 2)
	agent := ps.InitContainers[1]
	assert.Equal(t, kmv1.ContainerNameAgent, agent.Name)
	require.NotNil(t, agent.RestartPolicy, "agent must carry an explicit RestartPolicy to be a sidecar")
	assert.Equal(t, corev1.ContainerRestartPolicyAlways, *agent.RestartPolicy,
		"sidecar containers require RestartPolicy=Always")

	// The agent must carry the downward-API env and the kynomesh-run
	// mount — stamped by newAgentContainer.
	assert.NotNil(t, findEnv(agent.Env, kmv1.EnvNamespace), "agent must have NAMESPACE downward-API env")
	assert.NotNil(t, findEnv(agent.Env, kmv1.EnvPodName), "agent must have POD_NAME downward-API env")
	var hasMount bool
	for _, m := range agent.VolumeMounts {
		if m.Name == kmv1.VolumeNameKynomeshRun {
			hasMount = true
		}
	}
	assert.True(t, hasMount, "agent sidecar must mount kynomesh-run so it can serve over the shared UDS")
}

func TestBuildPodSpec_InjectsKynomeshRunVolume(t *testing.T) {
	// The volume is tmpfs-backed so the broker's UDS socket lives in
	// memory, not on disk. Exactly one copy must appear; appearing
	// multiple times would fail pod admission.
	ad := newAgentDeploy("greeter", 1)
	ps := buildPodSpec(ad, testBrokerImage, "")

	var got *corev1.Volume
	matches := 0
	for i := range ps.Volumes {
		if ps.Volumes[i].Name == kmv1.VolumeNameKynomeshRun {
			matches++
			got = &ps.Volumes[i]
		}
	}
	require.Equal(t, 1, matches, "kynomesh-run volume must be present exactly once")
	require.NotNil(t, got.EmptyDir, "expected an EmptyDir source")
	assert.Equal(t, corev1.StorageMediumMemory, got.EmptyDir.Medium,
		"emptyDir must use tmpfs so the UDS socket doesn't touch disk")
}

func TestBuildPodSpec_MountsKynomeshRunOnControllerOwnedContainersOnly(t *testing.T) {
	// kynomesh-run mounts on broker, init-runtime, and the agent sidecar only;
	// user sidecars and user init containers don't get it.
	ad := newAgentDeploy("greeter", 1)
	ad.Spec.Sidecars = []corev1.Container{{Name: "user-sidecar", Image: "busybox"}}
	ad.Spec.InitContainers = []corev1.Container{{Name: "init-1", Image: "busybox"}}

	ps := buildPodSpec(ad, testBrokerImage, "")

	checkMount := func(t *testing.T, mounts []corev1.VolumeMount, owner string) {
		t.Helper()
		matches := 0
		for _, m := range mounts {
			if m.Name == kmv1.VolumeNameKynomeshRun {
				matches++
				assert.Equal(t, kmv1.KynomeshRunPath, m.MountPath,
					"%s must mount kynomesh-run at the canonical path", owner)
			}
		}
		assert.Equal(t, 1, matches, "%s must have exactly one kynomesh-run mount", owner)
	}
	checkNoMount := func(t *testing.T, mounts []corev1.VolumeMount, owner string) {
		t.Helper()
		for _, m := range mounts {
			assert.NotEqual(t, kmv1.VolumeNameKynomeshRun, m.Name,
				"%s must not carry the kynomesh-run mount", owner)
		}
	}

	// Main containers: broker + user-sidecar.
	require.Len(t, ps.Containers, 2)
	checkMount(t, ps.Containers[0].VolumeMounts, kmv1.ContainerNameAgentBroker)
	checkNoMount(t, ps.Containers[1].VolumeMounts, "user-sidecar")

	// Init containers: [init-runtime, agent (sidecar), init-1].
	require.Len(t, ps.InitContainers, 3)
	assert.Equal(t, kmv1.ContainerNameInitRuntime, ps.InitContainers[0].Name)
	checkMount(t, ps.InitContainers[0].VolumeMounts, kmv1.ContainerNameInitRuntime)

	assert.Equal(t, kmv1.ContainerNameAgent, ps.InitContainers[1].Name)
	checkMount(t, ps.InitContainers[1].VolumeMounts, kmv1.ContainerNameAgent)

	assert.Equal(t, "init-1", ps.InitContainers[2].Name)
	checkNoMount(t, ps.InitContainers[2].VolumeMounts, "user-init")
}

func TestBuildPodSpec_InitContainerOrder(t *testing.T) {
	// Init containers come out as [init-runtime, agent (sidecar), ...user init containers].
	ad := newAgentDeploy("greeter", 1)
	ad.Spec.InitContainers = []corev1.Container{{Name: "user-init", Image: "busybox"}}

	ps := buildPodSpec(ad, testBrokerImage, "")
	require.Len(t, ps.InitContainers, 3)

	initRuntime := ps.InitContainers[0]
	assert.Equal(t, kmv1.ContainerNameInitRuntime, initRuntime.Name)
	assert.Equal(t, testBrokerImage, initRuntime.Image, "must reuse the broker image — they share the kynomesh binary")
	assert.Equal(t, []string{"init-runtime"}, initRuntime.Args)
	require.Len(t, initRuntime.VolumeMounts, 1)
	assert.Equal(t, kmv1.VolumeNameKynomeshRun, initRuntime.VolumeMounts[0].Name)
	assert.Equal(t, kmv1.KynomeshRunPath, initRuntime.VolumeMounts[0].MountPath)

	agent := ps.InitContainers[1]
	assert.Equal(t, kmv1.ContainerNameAgent, agent.Name)
	require.NotNil(t, agent.RestartPolicy)
	assert.Equal(t, corev1.ContainerRestartPolicyAlways, *agent.RestartPolicy)

	assert.Equal(t, "user-init", ps.InitContainers[2].Name)
}

func TestBuildPodSpec_PreservesUserVolumesAlongsideKynomeshRun(t *testing.T) {
	userVol := corev1.Volume{
		Name:         "user-config",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}
	ad := newAgentDeploy("greeter", 1)
	ad.Spec.Volumes = []corev1.Volume{userVol}

	ps := buildPodSpec(ad, testBrokerImage, "")

	names := make(map[string]int, len(ps.Volumes))
	for _, v := range ps.Volumes {
		names[v.Name]++
	}
	assert.Equal(t, 1, names[kmv1.VolumeNameKynomeshRun])
	assert.Equal(t, 1, names["user-config"])
}

func TestAppendMountIfAbsent_IdempotentOnNameMatch(t *testing.T) {
	existing := []corev1.VolumeMount{
		{Name: kmv1.VolumeNameKynomeshRun, MountPath: "/custom/path"},
	}
	got := appendMountIfAbsent(existing, kynomeshRunMount())
	require.Len(t, got, 1)
	assert.Equal(t, "/custom/path", got[0].MountPath, "user-supplied mount path must be preserved")
}

// brokerFromSpec finds the broker container in ps.Containers. After
// the agent moved to init containers, the broker is at index 0.
func brokerFromSpec(t *testing.T, ps corev1.PodSpec) corev1.Container {
	t.Helper()
	for _, c := range ps.Containers {
		if c.Name == kmv1.ContainerNameAgentBroker {
			return c
		}
	}
	t.Fatalf("broker container not found in ps.Containers")
	return corev1.Container{}
}

func TestBuildPodSpec_BrokerCarriesEncodedAgentDeployEnv(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	ps := buildPodSpec(ad, testBrokerImage, "")

	broker := brokerFromSpec(t, ps)
	env := findEnv(broker.Env, kmv1.EnvAgentDeployObject)
	require.NotNil(t, env, "broker container must carry %s", kmv1.EnvAgentDeployObject)
	require.NotEmpty(t, env.Value, "env value must be a base64-encoded JSON payload")

	// Decode → JSON unmarshal → must match SimpleCopy().
	raw, err := base64.StdEncoding.DecodeString(env.Value)
	require.NoError(t, err, "value must be valid base64")

	var got kmv1.AgentDeploy
	require.NoError(t, json.Unmarshal(raw, &got), "decoded payload must be valid AgentDeploy JSON")
	assert.Equal(t, ad.SimpleCopy(), got)
}

func TestBuildPodSpec_AgentDeployEnvOnlyOnBrokerAndInit(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	ad.Spec.Sidecars = []corev1.Container{{Name: "user-sidecar", Image: "busybox"}}
	ad.Spec.InitContainers = []corev1.Container{{Name: "user-init", Image: "busybox"}}
	ps := buildPodSpec(ad, testBrokerImage, "")

	for i, c := range ps.Containers {
		got := findEnv(c.Env, kmv1.EnvAgentDeployObject)
		if c.Name == kmv1.ContainerNameAgentBroker {
			assert.NotNil(t, got, "main container %d (%s) must have %s", i, c.Name, kmv1.EnvAgentDeployObject)
		} else {
			assert.Nil(t, got, "main container %d (%s) must NOT have %s", i, c.Name, kmv1.EnvAgentDeployObject)
		}
	}
	for i, c := range ps.InitContainers {
		got := findEnv(c.Env, kmv1.EnvAgentDeployObject)
		if c.Name == kmv1.ContainerNameInitRuntime {
			assert.NotNil(t, got, "init container %d (%s) must have %s", i, c.Name, kmv1.EnvAgentDeployObject)
		} else {
			assert.Nil(t, got, "init container %d (%s) must NOT have %s", i, c.Name, kmv1.EnvAgentDeployObject)
		}
	}
}

func TestBuildPodSpec_AgentNameChangeFlowsIntoBrokerEnv(t *testing.T) {
	a := newAgentDeploy("greeter", 1)
	b := newAgentDeploy("greeter", 1)
	b.Spec.Name = "renamed"

	envA := findEnv(brokerFromSpec(t, buildPodSpec(a, testBrokerImage, "")).Env, kmv1.EnvAgentDeployObject)
	envB := findEnv(brokerFromSpec(t, buildPodSpec(b, testBrokerImage, "")).Env, kmv1.EnvAgentDeployObject)
	require.NotNil(t, envA)
	require.NotNil(t, envB)
	assert.NotEqual(t, envA.Value, envB.Value)
}

func TestBuildPodSpec_ReplicasChangeDoesNotAffectBrokerEnv(t *testing.T) {
	a := newAgentDeploy("greeter", 1)
	b := newAgentDeploy("greeter", 1)
	b.Spec.Replicas = ptr.To[int32](5)

	envA := findEnv(brokerFromSpec(t, buildPodSpec(a, testBrokerImage, "")).Env, kmv1.EnvAgentDeployObject)
	envB := findEnv(brokerFromSpec(t, buildPodSpec(b, testBrokerImage, "")).Env, kmv1.EnvAgentDeployObject)
	require.NotNil(t, envA)
	require.NotNil(t, envB)
	assert.Equal(t, envA.Value, envB.Value)
}

func TestBuildPodSpec_InjectsDownwardAPIEnv(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	ad.Spec.Sidecars = []corev1.Container{{Name: "user-sidecar", Image: "busybox"}}
	ps := buildPodSpec(ad, testBrokerImage, "")

	check := func(t *testing.T, c corev1.Container) {
		t.Helper()
		ns := findEnv(c.Env, kmv1.EnvNamespace)
		require.NotNil(t, ns, "expected NAMESPACE env on %s", c.Name)
		require.NotNil(t, ns.ValueFrom)
		require.NotNil(t, ns.ValueFrom.FieldRef)
		assert.Equal(t, "metadata.namespace", ns.ValueFrom.FieldRef.FieldPath)

		pn := findEnv(c.Env, kmv1.EnvPodName)
		require.NotNil(t, pn, "expected POD_NAME env on %s", c.Name)
		require.NotNil(t, pn.ValueFrom)
		require.NotNil(t, pn.ValueFrom.FieldRef)
		assert.Equal(t, "metadata.name", pn.ValueFrom.FieldRef.FieldPath)
	}

	// Controller-owned containers only: broker, init-runtime, agent sidecar.
	// User-supplied sidecars and init containers must not receive the built-in env.
	for _, c := range ps.Containers {
		if c.Name == kmv1.ContainerNameAgentBroker {
			t.Run(c.Name, func(t *testing.T) { check(t, c) })
		} else {
			t.Run(c.Name+"/no-builtin", func(t *testing.T) {
				assert.Nil(t, findEnv(c.Env, kmv1.EnvNamespace),
					"user sidecar %s must not receive built-in NAMESPACE env", c.Name)
				assert.Nil(t, findEnv(c.Env, kmv1.EnvPodName),
					"user sidecar %s must not receive built-in POD_NAME env", c.Name)
			})
		}
	}
	for _, c := range ps.InitContainers {
		if c.Name == kmv1.ContainerNameAgent || c.Name == kmv1.ContainerNameInitRuntime {
			t.Run("init/"+c.Name, func(t *testing.T) { check(t, c) })
		}
	}
}

func TestBuildPodSpec_BuiltinEnvWinsOnConflict(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	ad.Spec.Container = &kmv1.Container{
		Env: []corev1.EnvVar{{Name: kmv1.EnvNamespace, Value: "user-override"}},
	}
	ps := buildPodSpec(ad, testBrokerImage, "")

	// Agent is the sidecar in init containers — find it by name.
	var agent corev1.Container
	for _, c := range ps.InitContainers {
		if c.Name == kmv1.ContainerNameAgent {
			agent = c
			break
		}
	}
	require.Equal(t, kmv1.ContainerNameAgent, agent.Name, "agent must be present in init containers")

	ns := findEnv(agent.Env, kmv1.EnvNamespace)
	require.NotNil(t, ns)
	assert.Empty(t, ns.Value, "user-supplied literal must be replaced by the downward-API ref")
	require.NotNil(t, ns.ValueFrom)
	require.NotNil(t, ns.ValueFrom.FieldRef)
	assert.Equal(t, "metadata.namespace", ns.ValueFrom.FieldRef.FieldPath)

	// Built-in entry replaces the user entry in place — no duplicate key.
	var nsCount int
	for _, e := range agent.Env {
		if e.Name == kmv1.EnvNamespace {
			nsCount++
		}
	}
	assert.Equal(t, 1, nsCount, "merged env must not contain duplicate NAMESPACE entries")

	pn := findEnv(agent.Env, kmv1.EnvPodName)
	require.NotNil(t, pn)
	require.NotNil(t, pn.ValueFrom)
}

func TestBuildPodSpec_AgentProjectsContainerFields(t *testing.T) {
	pullAlways := corev1.PullAlways
	ad := newAgentDeploy("greeter", 1)
	ad.Spec.Container = &kmv1.Container{
		Image:           "user/agent:v1",
		Command:         []string{"/bin/agent"},
		Args:            []string{"--foo", "bar"},
		Env:             []corev1.EnvVar{{Name: "AGENT_FLAG", Value: "on"}},
		EnvFrom:         []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "agent-cfg"}}}},
		ImagePullPolicy: &pullAlways,
		Ports:           []corev1.ContainerPort{{Name: "user", ContainerPort: 7000}},
	}

	ps := buildPodSpec(ad, testBrokerImage, "")

	agent := initContainerByName(ps, kmv1.ContainerNameAgent)
	require.Equal(t, kmv1.ContainerNameAgent, agent.Name)
	assert.Equal(t, "user/agent:v1", agent.Image)
	assert.Equal(t, []string{"/bin/agent"}, agent.Command)
	assert.Equal(t, []string{"--foo", "bar"}, agent.Args)
	assert.NotNil(t, findEnv(agent.Env, "AGENT_FLAG"), "user-supplied env must flow through")
	require.Len(t, agent.EnvFrom, 1)
	assert.Equal(t, "agent-cfg", agent.EnvFrom[0].ConfigMapRef.Name)
	assert.Equal(t, corev1.PullAlways, agent.ImagePullPolicy)
	require.Len(t, agent.Ports, 1)
	assert.Equal(t, "user", agent.Ports[0].Name)
	assert.Equal(t, int32(7000), agent.Ports[0].ContainerPort)
}

func TestBuildPodSpec_AgentProbes(t *testing.T) {
	wantCmd := []string{
		kmv1.ProbeBinaryPath,
		"--mode=grpc",
		"--socket=" + kmv1.BrokerSocketPath,
	}

	t.Run("defaults_when_spec_has_no_probes", func(t *testing.T) {
		ad := newAgentDeploy("greeter", 1)
		ad.Spec.Container = &kmv1.Container{Image: "user/agent:v1"}

		agent := initContainerByName(buildPodSpec(ad, testBrokerImage, ""), kmv1.ContainerNameAgent)

		require.NotNil(t, agent.ReadinessProbe)
		require.NotNil(t, agent.ReadinessProbe.Exec)
		assert.Equal(t, wantCmd, agent.ReadinessProbe.Exec.Command)
		assert.Equal(t, kmv1.DefaultAgentReadinessInitialDelaySec, agent.ReadinessProbe.InitialDelaySeconds)
		assert.Equal(t, kmv1.DefaultAgentReadinessPeriodSec, agent.ReadinessProbe.PeriodSeconds)
		assert.Equal(t, kmv1.DefaultAgentReadinessTimeoutSec, agent.ReadinessProbe.TimeoutSeconds)
		assert.Equal(t, kmv1.DefaultAgentReadinessFailureThreshold, agent.ReadinessProbe.FailureThreshold)
		assert.Equal(t, kmv1.DefaultAgentReadinessSuccessThreshold, agent.ReadinessProbe.SuccessThreshold)

		require.NotNil(t, agent.LivenessProbe)
		require.NotNil(t, agent.LivenessProbe.Exec)
		assert.Equal(t, wantCmd, agent.LivenessProbe.Exec.Command)
		assert.Equal(t, kmv1.DefaultAgentLivenessInitialDelaySec, agent.LivenessProbe.InitialDelaySeconds)
		assert.Equal(t, kmv1.DefaultAgentLivenessPeriodSec, agent.LivenessProbe.PeriodSeconds)
		assert.Equal(t, kmv1.DefaultAgentLivenessTimeoutSec, agent.LivenessProbe.TimeoutSeconds)
		assert.Equal(t, kmv1.DefaultAgentLivenessFailureThreshold, agent.LivenessProbe.FailureThreshold)
		assert.Equal(t, kmv1.DefaultAgentLivenessSuccessThreshold, agent.LivenessProbe.SuccessThreshold)
	})

	t.Run("honors_spec_timing_overrides_per_field", func(t *testing.T) {
		ad := newAgentDeploy("greeter", 1)
		ad.Spec.Container = &kmv1.Container{
			Image: "user/agent:v1",
			ReadinessProbe: &kmv1.Probe{
				PeriodSeconds:    ptr.To(int32(15)),
				FailureThreshold: ptr.To(int32(7)),
			},
			LivenessProbe: &kmv1.Probe{
				InitialDelaySeconds: ptr.To(int32(45)),
				TimeoutSeconds:      ptr.To(int32(8)),
			},
		}

		agent := initContainerByName(buildPodSpec(ad, testBrokerImage, ""), kmv1.ContainerNameAgent)

		require.NotNil(t, agent.ReadinessProbe)
		require.NotNil(t, agent.ReadinessProbe.Exec)
		assert.Equal(t, wantCmd, agent.ReadinessProbe.Exec.Command, "exec handler stays controller-owned")
		assert.Equal(t, int32(15), agent.ReadinessProbe.PeriodSeconds, "user override wins")
		assert.Equal(t, int32(7), agent.ReadinessProbe.FailureThreshold, "user override wins")
		assert.Equal(t, int32(2), agent.ReadinessProbe.TimeoutSeconds, "unset fields fall back to default")
		assert.Equal(t, kmv1.DefaultAgentReadinessInitialDelaySec, agent.ReadinessProbe.InitialDelaySeconds, "unset fields fall back to default")
		assert.Equal(t, int32(1), agent.ReadinessProbe.SuccessThreshold, "unset fields fall back to default")

		require.NotNil(t, agent.LivenessProbe)
		require.NotNil(t, agent.LivenessProbe.Exec)
		assert.Equal(t, wantCmd, agent.LivenessProbe.Exec.Command, "exec handler stays controller-owned")
		assert.Equal(t, int32(45), agent.LivenessProbe.InitialDelaySeconds, "user override wins")
		assert.Equal(t, int32(8), agent.LivenessProbe.TimeoutSeconds, "user override wins")
		assert.Equal(t, kmv1.DefaultAgentLivenessPeriodSec, agent.LivenessProbe.PeriodSeconds, "unset fields fall back to default")
		assert.Equal(t, int32(6), agent.LivenessProbe.FailureThreshold, "unset fields fall back to default")
		assert.Equal(t, int32(1), agent.LivenessProbe.SuccessThreshold, "unset fields fall back to default")
	})
}

func initContainerByName(ps corev1.PodSpec, name string) corev1.Container {
	for _, c := range ps.InitContainers {
		if c.Name == name {
			return c
		}
	}
	return corev1.Container{}
}

func TestBuildPodSpec_BrokerProbes(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	ad.Spec.Container = &kmv1.Container{Image: "user/agent:v1"}

	broker := buildPodSpec(ad, testBrokerImage, "").Containers[0]
	require.Equal(t, kmv1.ContainerNameAgentBroker, broker.Name)

	require.NotNil(t, broker.ReadinessProbe)
	require.NotNil(t, broker.ReadinessProbe.TCPSocket, "readiness gates on the :8490 listener")
	assert.Equal(t, "broker", broker.ReadinessProbe.TCPSocket.Port.String())
	assert.Equal(t, kmv1.DefaultBrokerReadinessInitialDelaySec, broker.ReadinessProbe.InitialDelaySeconds)
	assert.Equal(t, kmv1.DefaultBrokerReadinessPeriodSec, broker.ReadinessProbe.PeriodSeconds)
	assert.Equal(t, kmv1.DefaultBrokerReadinessTimeoutSec, broker.ReadinessProbe.TimeoutSeconds)
	assert.Equal(t, kmv1.DefaultBrokerReadinessFailureThreshold, broker.ReadinessProbe.FailureThreshold)
	assert.Equal(t, kmv1.DefaultBrokerReadinessSuccessThreshold, broker.ReadinessProbe.SuccessThreshold)

	require.NotNil(t, broker.LivenessProbe)
	require.NotNil(t, broker.LivenessProbe.TCPSocket)
	assert.Equal(t, "broker", broker.LivenessProbe.TCPSocket.Port.String())
	assert.Equal(t, kmv1.DefaultBrokerLivenessInitialDelaySec, broker.LivenessProbe.InitialDelaySeconds)
	assert.Equal(t, kmv1.DefaultBrokerLivenessPeriodSec, broker.LivenessProbe.PeriodSeconds)
	assert.Equal(t, kmv1.DefaultBrokerLivenessTimeoutSec, broker.LivenessProbe.TimeoutSeconds)
	assert.Equal(t, kmv1.DefaultBrokerLivenessFailureThreshold, broker.LivenessProbe.FailureThreshold)
	assert.Equal(t, kmv1.DefaultBrokerLivenessSuccessThreshold, broker.LivenessProbe.SuccessThreshold)
}

func TestBuildPodSpec_BrokerTemplatePreservesProbes(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	ad.Spec.Container = &kmv1.Container{Image: "user/agent:v1"}
	ad.Spec.BrokerTemplate = &kmv1.ContainerTemplate{ImagePullPolicy: corev1.PullIfNotPresent}

	broker := buildPodSpec(ad, testBrokerImage, "").Containers[0]

	require.NotNil(t, broker.ReadinessProbe, "BrokerTemplate must not clobber controller-owned probes")
	require.NotNil(t, broker.ReadinessProbe.TCPSocket)
	require.NotNil(t, broker.LivenessProbe)
	require.NotNil(t, broker.LivenessProbe.TCPSocket)
}

func TestBuildPodSpec_BrokerPreStopDrain(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	broker := buildPodSpec(ad, testBrokerImage, "").Containers[0]

	require.NotNil(t, broker.Lifecycle, "broker needs a preStop drain hook")
	require.NotNil(t, broker.Lifecycle.PreStop)
	require.NotNil(t, broker.Lifecycle.PreStop.Exec)
	assert.Equal(t, []string{kmv1.KynomeshBinaryPath, "drain"}, broker.Lifecycle.PreStop.Exec.Command)
}

func TestBuildPodSpec_TerminationGracePeriod(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	ps := buildPodSpec(ad, testBrokerImage, "")
	require.NotNil(t, ps.TerminationGracePeriodSeconds, "grace period must be defaulted for agentic drains")
	assert.Equal(t, kmv1.DefaultTerminationGracePeriodSeconds, *ps.TerminationGracePeriodSeconds)
}

func TestBuildPodSpec_BrokerTemplateAppliedAfterDefaults(t *testing.T) {
	// BrokerTemplate is the user's knob for tuning the controller-owned
	// broker container — resources, env, securityContext, etc. — without
	// being able to override the broker's identity (name, image, args).
	ad := newAgentDeploy("greeter", 1)
	ad.Spec.BrokerTemplate = &kmv1.ContainerTemplate{
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env:             []corev1.EnvVar{{Name: "BROKER_DEBUG", Value: "1"}},
	}

	ps := buildPodSpec(ad, testBrokerImage, "")
	broker := brokerFromSpec(t, ps)

	// User-supplied tuning was applied...
	assert.Equal(t, corev1.PullIfNotPresent, broker.ImagePullPolicy)
	assert.NotNil(t, findEnv(broker.Env, "BROKER_DEBUG"), "BrokerTemplate.Env must be appended to the broker")

	// ...but the broker's infrastructure identity is untouched.
	assert.Equal(t, kmv1.ContainerNameAgentBroker, broker.Name)
	assert.Equal(t, testBrokerImage, broker.Image)
	assert.Equal(t, []string{"broker"}, broker.Args)
	assert.NotNil(t, findEnv(broker.Env, kmv1.EnvAgentDeployObject),
		"controller-stamped env must survive template application")
}

func findEnv(env []corev1.EnvVar, name string) *corev1.EnvVar {
	for i := range env {
		if env[i].Name == name {
			return &env[i]
		}
	}
	return nil
}

func TestBuildPodSpec_StampsBrokerPullPolicyOnControllerOwnedContainers(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	ad.Spec.Sidecars = []corev1.Container{{Name: "user-sidecar", Image: "busybox"}}
	ad.Spec.InitContainers = []corev1.Container{{Name: "user-init", Image: "busybox"}}

	ps := buildPodSpec(ad, testBrokerImage, corev1.PullAlways)

	broker := brokerFromSpec(t, ps)
	assert.Equal(t, corev1.PullAlways, broker.ImagePullPolicy, "broker must carry the configured pull policy")

	initRuntime := initContainerByName(ps, kmv1.ContainerNameInitRuntime)
	assert.Equal(t, corev1.PullAlways, initRuntime.ImagePullPolicy, "init-runtime must carry the configured pull policy")

	for _, c := range ps.Containers {
		if c.Name == kmv1.ContainerNameAgentBroker {
			continue
		}
		assert.Empty(t, c.ImagePullPolicy, "user sidecar %s must not receive the broker pull policy", c.Name)
	}
	for _, c := range ps.InitContainers {
		if c.Name == kmv1.ContainerNameInitRuntime || c.Name == kmv1.ContainerNameAgent {
			continue
		}
		assert.Empty(t, c.ImagePullPolicy, "user init container %s must not receive the broker pull policy", c.Name)
	}
}

func TestBuildPodSpec_EmptyBrokerPullPolicyLeavesFieldUnset(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	ps := buildPodSpec(ad, testBrokerImage, "")

	broker := brokerFromSpec(t, ps)
	assert.Empty(t, broker.ImagePullPolicy, "empty pull policy must leave kubelet default in effect")

	initRuntime := initContainerByName(ps, kmv1.ContainerNameInitRuntime)
	assert.Empty(t, initRuntime.ImagePullPolicy, "empty pull policy must leave kubelet default in effect")
}
