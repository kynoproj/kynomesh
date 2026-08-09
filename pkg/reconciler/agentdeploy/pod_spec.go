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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

// buildPodSpec composes the corev1.PodSpec from the AgentDeploy spec.
func buildPodSpec(ad *kmv1.AgentDeploy, image string, imagePullPolicy corev1.PullPolicy) corev1.PodSpec {
	encodedAgentDeploy := kmv1.EncodeAgentDeploy(ad)
	containers := []corev1.Container{newBrokerContainer(image, imagePullPolicy, encodedAgentDeploy, ad.Spec.BrokerTemplate)}
	containers = append(containers, ad.Spec.Sidecars...)

	initContainers := []corev1.Container{
		newInitRuntimeContainer(image, imagePullPolicy, encodedAgentDeploy),
		newAgentContainer(ad),
	}
	initContainers = append(initContainers, ad.Spec.InitContainers...)

	ps := corev1.PodSpec{
		Containers:     containers,
		InitContainers: initContainers,
		Volumes:        append([]corev1.Volume{kynomeshRunVolume()}, ad.Spec.Volumes...),
		Subdomain:      ad.HeadlessServiceName(),
	}
	ad.Spec.ApplyToPodSpec(&ps)

	if ps.TerminationGracePeriodSeconds == nil {
		grace := kmv1.DefaultTerminationGracePeriodSeconds
		ps.TerminationGracePeriodSeconds = &grace
	}

	// Only apply to built-in containers
	const controllerOwnedContainerCount = 1 // broker
	const controllerOwnedInitCount = 2      // init-runtime, agent
	applyRuntimeBuiltins(ad, ps.Containers[:controllerOwnedContainerCount])
	applyRuntimeBuiltins(ad, ps.InitContainers[:controllerOwnedInitCount])
	return ps
}

// applyRuntimeBuiltins injects the kynomesh built-in env and the kynomesh-run
// mount onto every container in cs. Built-in env wins over user-supplied
// entries with the same name (see mergeEnv).
func applyRuntimeBuiltins(ad *kmv1.AgentDeploy, cs []corev1.Container) {
	env := commonEnv(ad)
	mount := kynomeshRunMount()
	for i := range cs {
		cs[i].Env = mergeEnv(cs[i].Env, env)
		cs[i].VolumeMounts = appendMountIfAbsent(cs[i].VolumeMounts, mount)
	}
}

// newAgentContainer builds the user's agent container as a K8s-native
// sidecar.
func newAgentContainer(ad *kmv1.AgentDeploy) corev1.Container {
	src := ad.Spec.Container
	c := corev1.Container{Name: kmv1.ContainerNameAgent}
	var readinessSpec, livenessSpec *kmv1.Probe
	if src != nil {
		c.Image = src.Image
		c.Command = src.Command
		c.Args = src.Args
		c.Env = src.Env
		c.EnvFrom = src.EnvFrom
		c.VolumeMounts = src.VolumeMounts
		c.Resources = src.Resources
		c.SecurityContext = src.SecurityContext
		if src.ImagePullPolicy != nil {
			c.ImagePullPolicy = *src.ImagePullPolicy
		}
		c.Ports = src.Ports
		readinessSpec = src.ReadinessProbe
		livenessSpec = src.LivenessProbe
	}
	c.ReadinessProbe = agentReadinessProbe(readinessSpec)
	c.LivenessProbe = agentLivenessProbe(livenessSpec)
	always := corev1.ContainerRestartPolicyAlways
	c.RestartPolicy = &always
	return c
}

// brokerDrainExec returns the preStop command for the broker container.
func brokerDrainExec() []string {
	return []string{kmv1.KynomeshBinaryPath, "drain"}
}

// agentProbeExec returns the exec command the agent container runs for
// both readiness and liveness probes: the bundled probe binary speaks
// gRPC health over the broker UDS.
func agentProbeExec() []string {
	return []string{
		kmv1.ProbeBinaryPath,
		"--mode=grpc",
		"--socket=" + kmv1.BrokerSocketPath,
	}
}

func agentReadinessProbe(spec *kmv1.Probe) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler:        corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: agentProbeExec()}},
		InitialDelaySeconds: kmv1.GetProbeInitialDelaySecondsOr(spec, kmv1.DefaultAgentReadinessInitialDelaySec),
		PeriodSeconds:       kmv1.GetProbePeriodSecondsOr(spec, kmv1.DefaultAgentReadinessPeriodSec),
		TimeoutSeconds:      kmv1.GetProbeTimeoutSecondsOr(spec, kmv1.DefaultAgentReadinessTimeoutSec),
		FailureThreshold:    kmv1.GetProbeFailureThresholdOr(spec, kmv1.DefaultAgentReadinessFailureThreshold),
		SuccessThreshold:    kmv1.GetProbeSuccessThresholdOr(spec, kmv1.DefaultAgentReadinessSuccessThreshold),
	}
}

func agentLivenessProbe(spec *kmv1.Probe) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler:        corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: agentProbeExec()}},
		InitialDelaySeconds: kmv1.GetProbeInitialDelaySecondsOr(spec, kmv1.DefaultAgentLivenessInitialDelaySec),
		PeriodSeconds:       kmv1.GetProbePeriodSecondsOr(spec, kmv1.DefaultAgentLivenessPeriodSec),
		TimeoutSeconds:      kmv1.GetProbeTimeoutSecondsOr(spec, kmv1.DefaultAgentLivenessTimeoutSec),
		FailureThreshold:    kmv1.GetProbeFailureThresholdOr(spec, kmv1.DefaultAgentLivenessFailureThreshold),
		SuccessThreshold:    kmv1.GetProbeSuccessThresholdOr(spec, kmv1.DefaultAgentLivenessSuccessThreshold),
	}
}

// kynomeshRunVolume returns the tmpfs-backed Volume used by every
// AgentDeploy pod.
func kynomeshRunVolume() corev1.Volume {
	return corev1.Volume{
		Name: kmv1.VolumeNameKynomeshRun,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				Medium: corev1.StorageMediumMemory,
			},
		},
	}
}

// kynomeshRunMount returns the per-container mount for the shared
// kynomesh-run volume.
func kynomeshRunMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      kmv1.VolumeNameKynomeshRun,
		MountPath: kmv1.KynomeshRunPath,
	}
}

// appendMountIfAbsent adds m to mounts unless an entry with the same
// Name is already present.
func appendMountIfAbsent(mounts []corev1.VolumeMount, m corev1.VolumeMount) []corev1.VolumeMount {
	for _, existing := range mounts {
		if existing.Name == m.Name {
			return mounts
		}
	}
	return append(mounts, m)
}

// commonEnv returns the env vars every AgentDeploy container receives:
// NAMESPACE and POD_NAME via the downward API, plus AGENTSET_NAME and
// AGENTDEPLOY_NAME sourced directly from the AgentDeploy object.
func commonEnv(ad *kmv1.AgentDeploy) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name: kmv1.EnvNamespace,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
			},
		},
		{
			Name: kmv1.EnvPodName,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
			},
		},
		{Name: kmv1.EnvAgentSetName, Value: ad.Spec.AgentSetName},
		{Name: kmv1.EnvAgentDeployName, Value: ad.Spec.Name},
	}
}

// mergeEnv returns existing with overrides applied: any entry in overrides
// replaces an entry in existing with the same Name, and anything not already
// present is appended. overrides wins — used here to guarantee the controller's
// downward-API env can't be shadowed by user-supplied values.
func mergeEnv(existing, overrides []corev1.EnvVar) []corev1.EnvVar {
	overrideByName := make(map[string]corev1.EnvVar, len(overrides))
	for _, o := range overrides {
		overrideByName[o.Name] = o
	}
	out := make([]corev1.EnvVar, 0, len(existing)+len(overrides))
	seen := make(map[string]struct{}, len(existing))
	for _, e := range existing {
		if o, ok := overrideByName[e.Name]; ok {
			out = append(out, o)
		} else {
			out = append(out, e)
		}
		seen[e.Name] = struct{}{}
	}
	for _, o := range overrides {
		if _, ok := seen[o.Name]; ok {
			continue
		}
		out = append(out, o)
	}
	return out
}

// newBrokerContainer builds the broker sidecar.
func newBrokerContainer(image string, pullPolicy corev1.PullPolicy, encodedAgentDeploy string, tmpl *kmv1.ContainerTemplate) corev1.Container {
	c := corev1.Container{
		Name:            kmv1.ContainerNameAgentBroker,
		Image:           image,
		ImagePullPolicy: pullPolicy,
		Args:            []string{"broker"},
		Env: []corev1.EnvVar{
			{Name: kmv1.EnvAgentDeployObject, Value: encodedAgentDeploy},
		},
		Ports: []corev1.ContainerPort{
			{
				Name:          "broker",
				ContainerPort: kmv1.AgentBrokerPort,
				Protocol:      corev1.ProtocolTCP,
			},
			{
				Name:          "introspect",
				ContainerPort: kmv1.AgentBrokerIntrospectionPort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
	}
	c.ReadinessProbe = brokerReadinessProbe()
	c.LivenessProbe = brokerLivenessProbe()
	// preStop drains in-flight requests before the broker is terminated.
	c.Lifecycle = &corev1.Lifecycle{
		PreStop: &corev1.LifecycleHandler{
			Exec: &corev1.ExecAction{Command: brokerDrainExec()},
		},
	}
	if tmpl != nil {
		tmpl.ApplyToContainer(&c)
	}
	return c
}

// brokerReadinessProbe gates readiness on the broker's port.
func brokerReadinessProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler:        corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromString("broker")}},
		InitialDelaySeconds: kmv1.DefaultBrokerReadinessInitialDelaySec,
		PeriodSeconds:       kmv1.DefaultBrokerReadinessPeriodSec,
		TimeoutSeconds:      kmv1.DefaultBrokerReadinessTimeoutSec,
		FailureThreshold:    kmv1.DefaultBrokerReadinessFailureThreshold,
		SuccessThreshold:    kmv1.DefaultBrokerReadinessSuccessThreshold,
	}
}

// brokerLivenessProbe restarts the broker if its port stops accepting.
func brokerLivenessProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler:        corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromString("broker")}},
		InitialDelaySeconds: kmv1.DefaultBrokerLivenessInitialDelaySec,
		PeriodSeconds:       kmv1.DefaultBrokerLivenessPeriodSec,
		TimeoutSeconds:      kmv1.DefaultBrokerLivenessTimeoutSec,
		FailureThreshold:    kmv1.DefaultBrokerLivenessFailureThreshold,
		SuccessThreshold:    kmv1.DefaultBrokerLivenessSuccessThreshold,
	}
}

// newInitRuntimeContainer builds the init container that prepares /var/run/kynomesh.
func newInitRuntimeContainer(image string, pullPolicy corev1.PullPolicy, encodedAgentDeploy string) corev1.Container {
	return corev1.Container{
		Name:            kmv1.ContainerNameInitRuntime,
		Image:           image,
		ImagePullPolicy: pullPolicy,
		Args:            []string{"init-runtime"},
		Env: []corev1.EnvVar{
			{Name: kmv1.EnvAgentDeployObject, Value: encodedAgentDeploy},
		},
	}
}
