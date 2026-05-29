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
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

const (
	testNamespace   = "test-ns"
	testBrokerImage = "quay.io/kynoproj/kynomesh:test"
)

func mustScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, kmv1.AddToScheme(scheme))
	return scheme
}

func newAgentDeploy(name string, replicas int32) *kmv1.AgentDeploy {
	return &kmv1.AgentDeploy{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  testNamespace,
			UID:        types.UID("uid-" + name),
			Generation: 1,
		},
		Spec: kmv1.AgentDeploySpec{
			AbstractAgentDeploy: kmv1.AbstractAgentDeploy{Name: name},
			Replicas:            &replicas,
			AgentSetName:        name + "-set",
		},
	}
}

func newTestReconciler(t *testing.T, objs ...client.Object) (*Reconciler, client.Client) {
	t.Helper()
	scheme := mustScheme(t)
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&kmv1.AgentDeploy{}).
		Build()
	r := NewReconciler(c, scheme, nil, &events.FakeRecorder{}, testBrokerImage)
	return r, c
}

func reconcileRequest(name string) ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: name}}
}

func listPods(t *testing.T, c client.Client) []corev1.Pod {
	t.Helper()
	var list corev1.PodList
	require.NoError(t, c.List(context.Background(), &list, client.InNamespace(testNamespace)))
	return list.Items
}

func TestNewPod_NamingAndDNSWiring(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	hash := "abc123"
	pod := newPod(ad, 2, corev1.PodSpec{Containers: []corev1.Container{{Name: kmv1.ContainerNameAgent}}}, hash)

	// Pod name: <deploy>-<replica>-<rand5>
	assert.Regexp(t, `^greeter-2-[a-z0-9]{5}$`, pod.Name)
	// Stable DNS hostname: <deploy>-<replica>
	assert.Equal(t, "greeter-2", pod.Spec.Hostname)
	assert.Equal(t, "greeter-headless", pod.Spec.Subdomain)
	// Replica index carried in both label and annotation.
	assert.Equal(t, "2", pod.Labels[kmv1.KeyReplica])
	assert.Equal(t, "2", pod.Annotations[kmv1.KeyReplica])
	assert.Equal(t, hash, pod.Annotations[kmv1.KeyHash])
	assert.Equal(t, "greeter", pod.Labels[kmv1.KeyAppName])
	// Controller-namespaced identity labels: this is what listOwnedPods,
	// the headless Service selector, and Status.Selector all key on.
	assert.Equal(t, "greeter", pod.Labels[kmv1.KeyAgentDeployName])
	assert.Equal(t, ad.Spec.AgentSetName, pod.Labels[kmv1.KeyAgentSetName])
	assert.Equal(t, kmv1.ControllerAgentDeploy, pod.Labels[kmv1.KeyManagedBy])
	require.Len(t, pod.OwnerReferences, 1)
	assert.True(t, *pod.OwnerReferences[0].Controller)
}

func TestNewPod_LabelsUseSpecNameNotMetadataName(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	ad.Name = "greeter-set-greeter" // compound metadata name (what AgentSet controller produces)
	ad.Spec.Name = "greeter"        // bare agent name

	pod := newPod(ad, 0, corev1.PodSpec{}, "h")
	assert.Equal(t, "greeter", pod.Labels[kmv1.KeyAgentDeployName],
		"KeyAgentDeployName must be the bare ad.Spec.Name, not the compound ad.Name")
	// KeyAppName intentionally keeps the compound metadata name — it's
	// the kubectl/dashboards-facing convention.
	assert.Equal(t, "greeter-set-greeter", pod.Labels[kmv1.KeyAppName])
	// Pod name still derives from the compound metadata name so it's
	// globally unique within the namespace.
	assert.Contains(t, pod.Name, "greeter-set-greeter-")
}

func TestNewHeadlessService_SelectorUsesSpecName(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	ad.Name = "greeter-set-greeter"
	ad.Spec.Name = "greeter"

	svc := newHeadlessService(ad)
	assert.Equal(t, "greeter", svc.Spec.Selector[kmv1.KeyAgentDeployName])
	assert.Equal(t, "greeter", svc.Labels[kmv1.KeyAgentDeployName])
	// Service name still derives from the compound metadata name.
	assert.Equal(t, "greeter-set-greeter-headless", svc.Name)
}

func TestNewPod_ProjectsAgentSetNameFromSpec(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	ad.Spec.AgentSetName = "greeter-set"
	pod := newPod(ad, 0, corev1.PodSpec{}, "h")
	assert.Equal(t, "greeter-set", pod.Labels[kmv1.KeyAgentSetName])
}

func TestBuildPodSpec_BrokerIsFirstMainContainer(t *testing.T) {
	// Main containers are [broker, ...user sidecars]. The agent lives
	// in init containers as a K8s-native sidecar (restartPolicy=Always).
	ad := newAgentDeploy("greeter", 1)
	ps := buildPodSpec(ad, testBrokerImage)

	require.GreaterOrEqual(t, len(ps.Containers), 1)
	broker := ps.Containers[0]
	assert.Equal(t, kmv1.ContainerNameAgentBroker, broker.Name)
	assert.Equal(t, testBrokerImage, broker.Image)
	assert.Empty(t, broker.Command, "Command must be unset so the Dockerfile ENTRYPOINT is used")
	assert.Equal(t, []string{"broker"}, broker.Args)
	require.Len(t, broker.Ports, 1)
	assert.Equal(t, int32(kmv1.AgentBrokerPort), broker.Ports[0].ContainerPort)
	assert.Equal(t, corev1.ProtocolTCP, broker.Ports[0].Protocol)

	// The agent must NOT appear in main containers.
	for _, c := range ps.Containers {
		assert.NotEqual(t, kmv1.ContainerNameAgent, c.Name,
			"agent must live in init containers as a sidecar, not in main containers")
	}
}

func TestBuildPodSpec_BrokerImageInHash(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	a := buildPodSpec(ad, "image-a")
	b := buildPodSpec(ad, "image-b")
	// Different broker images must produce different pod specs so the
	// hash-based drift detector rolls pods when the controller is upgraded.
	assert.NotEqual(t, a, b)
}

func TestBuildPodSpec_PreservesUserSidecarsAfterBroker(t *testing.T) {
	// Main containers are [broker, ...user sidecars]. The agent lives in
	// init containers as a sidecar (covered by other tests).
	ad := newAgentDeploy("greeter", 1)
	ad.Spec.Sidecars = []corev1.Container{{Name: "user-sidecar", Image: "busybox"}}
	ps := buildPodSpec(ad, testBrokerImage)

	require.Len(t, ps.Containers, 2)
	assert.Equal(t, kmv1.ContainerNameAgentBroker, ps.Containers[0].Name)
	assert.Equal(t, "user-sidecar", ps.Containers[1].Name)
}

func TestBuildPodSpec_AgentRunsAsSidecarInitContainer(t *testing.T) {
	// The user's agent container runs as a K8s-native sidecar — i.e.,
	// an init container with RestartPolicy=Always. This guarantees the
	// agent is up before the broker's startup probe runs.
	ad := newAgentDeploy("greeter", 1)
	ps := buildPodSpec(ad, testBrokerImage)

	// Init containers: [init-socket, agent (sidecar)]
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
	ps := buildPodSpec(ad, testBrokerImage)

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

func TestBuildPodSpec_MountsKynomeshRunOnRuntimeContainersOnly(t *testing.T) {
	// The shared mount goes on every container that needs to interact
	// with the UDS socket:
	//   - main containers: broker, user sidecars
	//   - init containers: init-socket (writes the placeholder) and the
	//     agent sidecar (binds the socket)
	// User-supplied init containers do NOT receive the mount.
	ad := newAgentDeploy("greeter", 1)
	ad.Spec.Sidecars = []corev1.Container{{Name: "user-sidecar", Image: "busybox"}}
	ad.Spec.InitContainers = []corev1.Container{{Name: "init-1", Image: "busybox"}}

	ps := buildPodSpec(ad, testBrokerImage)

	checkMount := func(t *testing.T, mounts []corev1.VolumeMount, owner string) {
		t.Helper()
		matches := 0
		for _, m := range mounts {
			if m.Name == kmv1.VolumeNameKynomeshRun {
				matches++
				assert.Equal(t, kmv1.PathKynomeshRun, m.MountPath,
					"%s must mount kynomesh-run at the canonical path", owner)
			}
		}
		assert.Equal(t, 1, matches, "%s must have exactly one kynomesh-run mount", owner)
	}

	// Main containers: broker + user-sidecar.
	require.Len(t, ps.Containers, 2)
	checkMount(t, ps.Containers[0].VolumeMounts, kmv1.ContainerNameAgentBroker)
	checkMount(t, ps.Containers[1].VolumeMounts, "user-sidecar")

	// Init containers: [init-socket, agent (sidecar), init-1].
	require.Len(t, ps.InitContainers, 3)
	assert.Equal(t, kmv1.ContainerNameInitSocket, ps.InitContainers[0].Name)
	checkMount(t, ps.InitContainers[0].VolumeMounts, kmv1.ContainerNameInitSocket)

	assert.Equal(t, kmv1.ContainerNameAgent, ps.InitContainers[1].Name)
	checkMount(t, ps.InitContainers[1].VolumeMounts, kmv1.ContainerNameAgent)

	assert.Equal(t, "init-1", ps.InitContainers[2].Name)
	for _, m := range ps.InitContainers[2].VolumeMounts {
		assert.NotEqual(t, kmv1.VolumeNameKynomeshRun, m.Name,
			"user-supplied init containers must not carry the kynomesh-run mount")
	}
}

func TestBuildPodSpec_InitContainerOrder(t *testing.T) {
	// Init containers must come out in the order:
	//   [init-socket, agent (sidecar), ...user init containers]
	// init-socket runs to completion first (creates the placeholder
	// socket file). The agent sidecar then starts and binds the UDS;
	// because it's a sidecar (RestartPolicy=Always), the kubelet
	// considers the init phase complete and proceeds to the broker.
	// User init containers come last so users can rely on the socket
	// being ready if they want to inspect it.
	ad := newAgentDeploy("greeter", 1)
	ad.Spec.InitContainers = []corev1.Container{{Name: "user-init", Image: "busybox"}}

	ps := buildPodSpec(ad, testBrokerImage)
	require.Len(t, ps.InitContainers, 3)

	initSocket := ps.InitContainers[0]
	assert.Equal(t, kmv1.ContainerNameInitSocket, initSocket.Name)
	assert.Equal(t, testBrokerImage, initSocket.Image, "must reuse the broker image — they share the kynomesh binary")
	assert.Equal(t, []string{"init-socket"}, initSocket.Args)
	require.Len(t, initSocket.VolumeMounts, 1)
	assert.Equal(t, kmv1.VolumeNameKynomeshRun, initSocket.VolumeMounts[0].Name)
	assert.Equal(t, kmv1.PathKynomeshRun, initSocket.VolumeMounts[0].MountPath)

	agent := ps.InitContainers[1]
	assert.Equal(t, kmv1.ContainerNameAgent, agent.Name)
	require.NotNil(t, agent.RestartPolicy)
	assert.Equal(t, corev1.ContainerRestartPolicyAlways, *agent.RestartPolicy)

	assert.Equal(t, "user-init", ps.InitContainers[2].Name)
}

func TestBuildPodSpec_PreservesUserVolumesAlongsideKynomeshRun(t *testing.T) {
	// User-supplied volumes must still flow through unchanged — the
	// controller-injected kynomesh-run is additive.
	userVol := corev1.Volume{
		Name:         "user-config",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}
	ad := newAgentDeploy("greeter", 1)
	ad.Spec.Volumes = []corev1.Volume{userVol}

	ps := buildPodSpec(ad, testBrokerImage)

	names := make(map[string]int, len(ps.Volumes))
	for _, v := range ps.Volumes {
		names[v.Name]++
	}
	assert.Equal(t, 1, names[kmv1.VolumeNameKynomeshRun])
	assert.Equal(t, 1, names["user-config"])
}

func TestAppendMountIfAbsent_IdempotentOnNameMatch(t *testing.T) {
	// If a user's container already declares a mount with the kynomesh-run
	// volume name (e.g., they shadowed it via their own template), the
	// controller's append step must not produce a duplicate — K8s rejects
	// pods with duplicate mount names.
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
	ps := buildPodSpec(ad, testBrokerImage)

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

func TestBuildPodSpec_AgentDeployEnvOnlyOnBroker(t *testing.T) {
	// The encoded AgentDeploy is broker-only. The agent sidecar (init)
	// and user sidecars (main) must not receive it — they have no need
	// for it, and leaking it expands the trust boundary unnecessarily.
	ad := newAgentDeploy("greeter", 1)
	ad.Spec.Sidecars = []corev1.Container{{Name: "user-sidecar", Image: "busybox"}}
	ps := buildPodSpec(ad, testBrokerImage)

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
		assert.Nil(t, got, "init container %d (%s) must NOT have %s", i, c.Name, kmv1.EnvAgentDeployObject)
	}
}

func TestBuildPodSpec_AgentNameChangeFlowsIntoBrokerEnv(t *testing.T) {
	// A change to the agent's identity (Spec.Name) must change the broker's
	// env-var payload — that's how the hash detects the drift and rolls
	// the pod.
	a := newAgentDeploy("greeter", 1)
	b := newAgentDeploy("greeter", 1)
	b.Spec.Name = "renamed"

	envA := findEnv(brokerFromSpec(t, buildPodSpec(a, testBrokerImage)).Env, kmv1.EnvAgentDeployObject)
	envB := findEnv(brokerFromSpec(t, buildPodSpec(b, testBrokerImage)).Env, kmv1.EnvAgentDeployObject)
	require.NotNil(t, envA)
	require.NotNil(t, envB)
	assert.NotEqual(t, envA.Value, envB.Value)
}

func TestBuildPodSpec_ReplicasChangeDoesNotAffectBrokerEnv(t *testing.T) {
	// Replicas is dropped by SimpleCopy — scaling up/down must not change
	// the broker env-var payload, otherwise every scale event would roll
	// every pod.
	a := newAgentDeploy("greeter", 1)
	b := newAgentDeploy("greeter", 1)
	b.Spec.Replicas = ptr.To[int32](5)

	envA := findEnv(brokerFromSpec(t, buildPodSpec(a, testBrokerImage)).Env, kmv1.EnvAgentDeployObject)
	envB := findEnv(brokerFromSpec(t, buildPodSpec(b, testBrokerImage)).Env, kmv1.EnvAgentDeployObject)
	require.NotNil(t, envA)
	require.NotNil(t, envB)
	assert.Equal(t, envA.Value, envB.Value)
}

func TestBuildPodSpec_InjectsDownwardAPIEnv(t *testing.T) {
	// Every container that runs at request-handling time gets the
	// downward-API env: agent sidecar (in init), broker, user sidecars.
	// The init-socket utility container does not need it.
	ad := newAgentDeploy("greeter", 1)
	ad.Spec.Sidecars = []corev1.Container{{Name: "user-sidecar", Image: "busybox"}}
	ps := buildPodSpec(ad, testBrokerImage)

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

	// Main containers.
	for _, c := range ps.Containers {
		t.Run(c.Name, func(t *testing.T) { check(t, c) })
	}
	// Agent sidecar in init containers.
	for _, c := range ps.InitContainers {
		if c.Name == kmv1.ContainerNameAgent {
			t.Run("init/"+c.Name, func(t *testing.T) { check(t, c) })
		}
	}
}

func TestBuildPodSpec_BuiltinEnvWinsOnConflict(t *testing.T) {
	// Built-in identity env (NAMESPACE / POD_NAME) must always come from
	// the downward API, even if the user tries to set them — workloads
	// must not be able to misrepresent their own pod identity. The agent
	// is the most likely target for this since it's the container whose
	// spec users supply directly.
	ad := newAgentDeploy("greeter", 1)
	ad.Spec.Container = &kmv1.Container{
		Env: []corev1.EnvVar{{Name: kmv1.EnvNamespace, Value: "user-override"}},
	}
	ps := buildPodSpec(ad, testBrokerImage)

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
	// The agent container is materialised from ad.Spec.Container — a
	// full container spec the user supplies. Image, command, args,
	// resources, probes, ports, etc. flow through; the controller only
	// owns Name, RestartPolicy (sidecar=Always), and the augmentations
	// (downward-API env, kynomesh-run mount).
	pullAlways := corev1.PullAlways
	ad := newAgentDeploy("greeter", 1)
	ad.Spec.Container = &kmv1.Container{
		Image:           "user/agent:v1",
		Command:         []string{"/bin/agent"},
		Args:            []string{"--foo", "bar"},
		Env:             []corev1.EnvVar{{Name: "AGENT_FLAG", Value: "on"}},
		EnvFrom:         []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "agent-cfg"}}}},
		ImagePullPolicy: &pullAlways,
		ReadinessProbe:  &corev1.Probe{InitialDelaySeconds: 5},
		LivenessProbe:   &corev1.Probe{InitialDelaySeconds: 10},
		Ports:           []corev1.ContainerPort{{Name: "user", ContainerPort: 7000}},
	}

	ps := buildPodSpec(ad, testBrokerImage)

	var agent corev1.Container
	for _, c := range ps.InitContainers {
		if c.Name == kmv1.ContainerNameAgent {
			agent = c
			break
		}
	}
	require.Equal(t, kmv1.ContainerNameAgent, agent.Name)
	assert.Equal(t, "user/agent:v1", agent.Image)
	assert.Equal(t, []string{"/bin/agent"}, agent.Command)
	assert.Equal(t, []string{"--foo", "bar"}, agent.Args)
	assert.NotNil(t, findEnv(agent.Env, "AGENT_FLAG"), "user-supplied env must flow through")
	require.Len(t, agent.EnvFrom, 1)
	assert.Equal(t, "agent-cfg", agent.EnvFrom[0].ConfigMapRef.Name)
	assert.Equal(t, corev1.PullAlways, agent.ImagePullPolicy)
	require.NotNil(t, agent.ReadinessProbe)
	assert.Equal(t, int32(5), agent.ReadinessProbe.InitialDelaySeconds)
	require.NotNil(t, agent.LivenessProbe)
	assert.Equal(t, int32(10), agent.LivenessProbe.InitialDelaySeconds)
	require.Len(t, agent.Ports, 1)
	assert.Equal(t, "user", agent.Ports[0].Name)
	assert.Equal(t, int32(7000), agent.Ports[0].ContainerPort)
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

	ps := buildPodSpec(ad, testBrokerImage)
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

func TestNewHeadlessService(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	svc := newHeadlessService(ad)
	assert.Equal(t, "greeter-headless", svc.Name)
	assert.Equal(t, corev1.ClusterIPNone, svc.Spec.ClusterIP)
	assert.True(t, svc.Spec.PublishNotReadyAddresses)
	// Selector keys on the controller-namespaced AgentDeploy + AgentSet
	// labels so it doesn't conflate with the generic app.kubernetes.io/name.
	assert.Equal(t, "greeter", svc.Spec.Selector[kmv1.KeyAgentDeployName])
	assert.Equal(t, ad.Spec.AgentSetName, svc.Spec.Selector[kmv1.KeyAgentSetName])
	assert.Equal(t, kmv1.ControllerAgentDeploy, svc.Spec.Selector[kmv1.KeyManagedBy])
	// Metadata labels: KeyAppName for kubectl/dashboards; KeyAgentDeployName
	// and KeyAgentSetName for the controller's own selectors.
	assert.Equal(t, "greeter", svc.Labels[kmv1.KeyAppName])
	assert.Equal(t, "greeter", svc.Labels[kmv1.KeyAgentDeployName])
	assert.Equal(t, ad.Spec.AgentSetName, svc.Labels[kmv1.KeyAgentSetName])
	require.Len(t, svc.OwnerReferences, 1)
}

func TestDesiredReplicas(t *testing.T) {
	zero, neg := int32(0), int32(-3)
	cases := []struct {
		name string
		in   *int32
		want int
	}{
		{"nil defaults to 1", nil, 1},
		{"zero", &zero, 0},
		{"negative clamps to 0", &neg, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ad := &kmv1.AgentDeploy{Spec: kmv1.AgentDeploySpec{Replicas: tc.in}}
			assert.Equal(t, tc.want, desiredReplicas(ad))
		})
	}
}

func TestGroupPodsByReplica(t *testing.T) {
	pods := []*corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{kmv1.KeyReplica: "0"}}},
		{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{kmv1.KeyReplica: "1"}}},
		{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{kmv1.KeyReplica: "1"}}}, // duplicate
		{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{kmv1.KeyReplica: "notanint"}}},
	}
	grouped := groupPodsByReplica(pods)
	assert.Len(t, grouped[0], 1)
	assert.Len(t, grouped[1], 2)
	assert.Len(t, grouped[-1], 1, "invalid annotation bucketed under -1")
}

func TestAddRemoveFinalizer(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	addFinalizer(ad)
	addFinalizer(ad) // idempotent
	assert.Equal(t, []string{FinalizerName}, ad.Finalizers)
	removeFinalizer(ad)
	assert.Empty(t, ad.Finalizers)
}

func TestReconcile_CreatesPodsAndService(t *testing.T) {
	ad := newAgentDeploy("greeter", 3)
	r, c := newTestReconciler(t, ad)

	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)

	pods := listPods(t, c)
	assert.Len(t, pods, 3, "one pod per replica")

	indices := map[string]bool{}
	for _, p := range pods {
		indices[p.Annotations[kmv1.KeyReplica]] = true
		require.Len(t, p.OwnerReferences, 1)
		assert.True(t, *p.OwnerReferences[0].Controller)
	}
	assert.True(t, indices["0"] && indices["1"] && indices["2"])

	var svc corev1.Service
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "greeter-headless"}, &svc))
	assert.Equal(t, corev1.ClusterIPNone, svc.Spec.ClusterIP)
}

func TestReconcile_ScaleUp(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	r, c := newTestReconciler(t, ad)

	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)
	assert.Len(t, listPods(t, c), 1)

	// Bump replicas to 3 and reconcile again.
	var live kmv1.AgentDeploy
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "greeter"}, &live))
	three := int32(3)
	live.Spec.Replicas = &three
	require.NoError(t, c.Update(context.Background(), &live))

	_, err = r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)
	assert.Len(t, listPods(t, c), 3)
}

func TestReconcile_ScaleDown(t *testing.T) {
	ad := newAgentDeploy("greeter", 3)
	r, c := newTestReconciler(t, ad)

	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)
	require.Len(t, listPods(t, c), 3)

	var live kmv1.AgentDeploy
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "greeter"}, &live))
	one := int32(1)
	live.Spec.Replicas = &one
	require.NoError(t, c.Update(context.Background(), &live))

	_, err = r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)
	pods := listPods(t, c)
	assert.Len(t, pods, 1)
	assert.Equal(t, "0", pods[0].Annotations[kmv1.KeyReplica], "replica 0 should survive scale-down")
}

func TestReconcile_HashDriftRecreatesPod(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	r, c := newTestReconciler(t, ad)

	// First reconcile creates the pod.
	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)
	pods := listPods(t, c)
	require.Len(t, pods, 1)
	originalName := pods[0].Name

	// Force a hash mismatch by overwriting the pod's stamped hash.
	pods[0].Annotations[kmv1.KeyHash] = "stale"
	require.NoError(t, c.Update(context.Background(), &pods[0]))

	_, err = r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)

	pods = listPods(t, c)
	require.Len(t, pods, 1, "stale pod replaced by current-hash one")
	assert.NotEqual(t, originalName, pods[0].Name, "delete-and-recreate produces a new name")
	assert.NotEqual(t, "stale", pods[0].Annotations[kmv1.KeyHash])
}

func TestReconcile_DeletesOrphanedReplica(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	// Pre-seed an orphan pod at replica index 5 (well outside desired window).
	orphan := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      "greeter-5-zzzzz",
			Labels: map[string]string{
				kmv1.KeyAppName:         "greeter",
				kmv1.KeyAgentDeployName: "greeter",
				kmv1.KeyAgentSetName:    "greeter-set",
				kmv1.KeyManagedBy:       kmv1.ControllerAgentDeploy,
				kmv1.KeyReplica:         "5",
			},
			Annotations: map[string]string{kmv1.KeyReplica: "5"},
		},
	}
	r, c := newTestReconciler(t, ad, orphan)

	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)

	pods := listPods(t, c)
	for _, p := range pods {
		idx, _ := strconv.Atoi(p.Annotations[kmv1.KeyReplica])
		assert.Less(t, idx, 1, "orphan replica index should be deleted")
	}
}

func TestReconcile_ServiceDriftRecreates(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	r, c := newTestReconciler(t, ad)

	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)

	var svc corev1.Service
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "greeter-headless"}, &svc))
	svc.Annotations[kmv1.KeyHash] = "stale"
	require.NoError(t, c.Update(context.Background(), &svc))

	_, err = r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)

	var got corev1.Service
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "greeter-headless"}, &got))
	assert.NotEqual(t, "stale", got.Annotations[kmv1.KeyHash], "stale service should be replaced")
}

func TestReconcile_DeletionCleansEverything(t *testing.T) {
	now := metav1.NewTime(time.Now())
	ad := newAgentDeploy("greeter", 2)
	ad.DeletionTimestamp = &now
	ad.Finalizers = []string{FinalizerName}

	// Seed existing children.
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      "greeter-0-aaaaa",
			Labels: map[string]string{
				kmv1.KeyAppName:         "greeter",
				kmv1.KeyAgentDeployName: "greeter",
				kmv1.KeyAgentSetName:    "greeter-set",
				kmv1.KeyManagedBy:       kmv1.ControllerAgentDeploy,
				kmv1.KeyReplica:         "0",
			},
			Annotations: map[string]string{kmv1.KeyReplica: "0"},
		},
	}
	svc := newHeadlessService(ad)
	svc.Annotations[kmv1.KeyHash] = "x"

	r, c := newTestReconciler(t, ad, pod, svc)

	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)

	assert.Empty(t, listPods(t, c), "pods removed on deletion")
	var lookup corev1.Service
	err = c.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "greeter-headless"}, &lookup)
	assert.Error(t, err, "headless service removed on deletion")
}

func TestReconcile_StatusReadyCount(t *testing.T) {
	ad := newAgentDeploy("greeter", 2)
	r, c := newTestReconciler(t, ad)

	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)

	// Mark one pod ready, then re-reconcile.
	pods := listPods(t, c)
	require.Len(t, pods, 2)
	pods[0].Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	require.NoError(t, c.Status().Update(context.Background(), &pods[0]))

	_, err = r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)

	var got kmv1.AgentDeploy
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "greeter"}, &got))
	assert.Equal(t, uint32(2), got.Status.DesiredReplicas)
	assert.Equal(t, uint32(2), got.Status.Replicas)
	assert.Equal(t, uint32(1), got.Status.ReadyReplicas)
	wantSelector := fmt.Sprintf("%s=%s,%s=greeter,%s=%s",
		kmv1.KeyAgentSetName, got.Spec.AgentSetName,
		kmv1.KeyAgentDeployName,
		kmv1.KeyManagedBy, kmv1.ControllerAgentDeploy)
	assert.Equal(t, wantSelector, got.Status.Selector)
}

func TestReconcile_NotFoundIsNoop(t *testing.T) {
	r, _ := newTestReconciler(t)
	res, err := r.Reconcile(context.Background(), reconcileRequest("missing"))
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, res)
}

// markAllPodsReady flips every pod's PodReady condition to True via the
// status subresource. Used to advance the rolling-update wait gate
// between batches.
func markAllPodsReady(t *testing.T, c client.Client) {
	t.Helper()
	var list corev1.PodList
	require.NoError(t, c.List(context.Background(), &list, client.InNamespace(testNamespace)))
	for i := range list.Items {
		p := &list.Items[i]
		p.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
		require.NoError(t, c.Status().Update(context.Background(), p))
	}
}

func TestReconcile_RollingUpdate_RespectsMaxUnavailable(t *testing.T) {
	// 4 replicas with MaxUnavailable=1 → exactly one pod replaced per pass.
	// All four pods drift from the desired hash. Verify the controller
	// touches only one slot per reconcile, and that it waits for the
	// in-flight replacement to go Ready before starting the next batch.
	ad := newAgentDeploy("greeter", 4)
	ad.Spec.UpdateStrategy = kmv1.UpdateStrategy{
		Type: kmv1.RollingUpdateStrategyType,
		RollingUpdate: &kmv1.RollingUpdateStrategy{
			MaxUnavailable: ptr.To(intstr.FromInt(1)),
		},
	}
	r, c := newTestReconciler(t, ad)

	// Initial bring-up creates all 4 in one pass (creates aren't gated).
	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)
	require.Len(t, listPods(t, c), 4)
	markAllPodsReady(t, c)

	// Force drift on all four pods by stamping a stale hash.
	pods := listPods(t, c)
	for i := range pods {
		pods[i].Annotations[kmv1.KeyHash] = "stale"
		require.NoError(t, c.Update(context.Background(), &pods[i]))
	}

	// Each pass should replace exactly one slot. Between passes we must
	// mark the new pod Ready or the wait gate will block the next batch.
	stalePerPass := []int{3, 2, 1, 0}
	for pass, wantStale := range stalePerPass {
		_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
		require.NoError(t, err, "pass %d", pass)

		gotStale := 0
		for _, p := range listPods(t, c) {
			if p.Annotations[kmv1.KeyHash] == "stale" {
				gotStale++
			}
		}
		assert.Equal(t, wantStale, gotStale, "pass %d: only one slot replaced per pass", pass)
		markAllPodsReady(t, c)
	}

	// Final state: no stale pods, all 4 slots filled.
	finalPods := listPods(t, c)
	assert.Len(t, finalPods, 4)
	for _, p := range finalPods {
		assert.NotEqual(t, "stale", p.Annotations[kmv1.KeyHash])
	}
}

func TestReconcile_RollingUpdate_WaitGateBlocksUntilReady(t *testing.T) {
	// Without readiness, the next batch must NOT start. Set MaxUnavailable=1
	// over 3 replicas, drift all three, and run two reconciles back-to-back
	// without marking the new pod Ready. The second reconcile must be a noop.
	ad := newAgentDeploy("greeter", 3)
	ad.Spec.UpdateStrategy = kmv1.UpdateStrategy{
		Type: kmv1.RollingUpdateStrategyType,
		RollingUpdate: &kmv1.RollingUpdateStrategy{
			MaxUnavailable: ptr.To(intstr.FromInt(1)),
		},
	}
	r, c := newTestReconciler(t, ad)
	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)
	markAllPodsReady(t, c)

	for _, p := range listPods(t, c) {
		p.Annotations[kmv1.KeyHash] = "stale"
		require.NoError(t, c.Update(context.Background(), &p))
	}

	// First pass replaces one — leaves it not-Ready.
	_, err = r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)
	staleAfter1 := countStale(listPods(t, c))
	assert.Equal(t, 2, staleAfter1)

	// Second pass: new pod still not Ready → gate must block.
	_, err = r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)
	assert.Equal(t, 2, countStale(listPods(t, c)), "wait gate must block until the new pod is Ready")
}

func countStale(pods []corev1.Pod) int {
	n := 0
	for _, p := range pods {
		if p.Annotations[kmv1.KeyHash] == "stale" {
			n++
		}
	}
	return n
}

func TestReconcile_RollingUpdate_InitialCreateNotGated(t *testing.T) {
	// MaxUnavailable=1 must not slow down initial bring-up — empty slots
	// are creates, not replacements, and aren't subject to the rolling-update
	// budget. A fresh deploy with replicas=4 reaches steady state in one pass.
	ad := newAgentDeploy("greeter", 4)
	ad.Spec.UpdateStrategy = kmv1.UpdateStrategy{
		Type: kmv1.RollingUpdateStrategyType,
		RollingUpdate: &kmv1.RollingUpdateStrategy{
			MaxUnavailable: ptr.To(intstr.FromInt(1)),
		},
	}
	r, c := newTestReconciler(t, ad)

	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)
	assert.Len(t, listPods(t, c), 4, "initial bring-up creates all slots in one pass")
}

func TestReconcile_RollingUpdate_NewSpecResetsUpdateCursor(t *testing.T) {
	// A spec change mid-rollout must reset Status.UpdateHash so the rollout
	// restarts from scratch against the new hash.
	ad := newAgentDeploy("greeter", 2)
	r, c := newTestReconciler(t, ad)

	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)

	var afterInitial kmv1.AgentDeploy
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "greeter"}, &afterInitial))
	firstHash := afterInitial.Status.UpdateHash
	require.NotEmpty(t, firstHash)

	// Mutate the spec — the agent's identity is hashed into the broker env,
	// so renaming flips the desired hash.
	afterInitial.Spec.Name = "renamed"
	require.NoError(t, c.Update(context.Background(), &afterInitial))

	_, err = r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)

	var afterChange kmv1.AgentDeploy
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "greeter"}, &afterChange))
	assert.NotEqual(t, firstHash, afterChange.Status.UpdateHash, "UpdateHash must track the new spec")
}
