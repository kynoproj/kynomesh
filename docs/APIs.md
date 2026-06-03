<p>

Packages:
</p>

<ul>

<li>

<a href="#kynomesh.kyno.sh%2fv1alpha1">kynomesh.kyno.sh/v1alpha1</a>
</li>

</ul>

<h2 id="kynomesh.kyno.sh/v1alpha1">

kynomesh.kyno.sh/v1alpha1
</h2>

<p>

</p>

Resource Types:
<ul>

</ul>

<h3 id="kynomesh.kyno.sh/v1alpha1.AbstractAgentDeploy">

AbstractAgentDeploy
</h3>

<p>

(<em>Appears on:</em>
<a href="#kynomesh.kyno.sh/v1alpha1.AgentDeploySpec">AgentDeploySpec</a>,
<a href="#kynomesh.kyno.sh/v1alpha1.AgentSetSpec">AgentSetSpec</a>)
</p>

<p>

</p>

<table>

<thead>

<tr>

<th>

Field
</th>

<th>

Description
</th>

</tr>

</thead>

<tbody>

<tr>

<td>

<code>name</code></br> <em> string </em>
</td>

<td>

</td>

</tr>

<tr>

<td>

<code>AbstractPodTemplate</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.AbstractPodTemplate">
AbstractPodTemplate </a> </em>
</td>

<td>

<p>

(Members of <code>AbstractPodTemplate</code> are embedded into this
type.)
</p>

<em>(Optional)</em>
</td>

</tr>

<tr>

<td>

<code>container</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.Container"> Container </a> </em>
</td>

<td>

<p>

Agent container, the user’s agent code runs here.
</p>

</td>

</tr>

<tr>

<td>

<code>brokerTemplate</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.ContainerTemplate">
ContainerTemplate </a> </em>
</td>

<td>

<em>(Optional)</em>
<p>

Container template for the broker container.
</p>

</td>

</tr>

<tr>

<td>

<code>volumes</code></br> <em>
<a href="https://v1-18.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#volume-v1-core">
\[\]Kubernetes core/v1.Volume </a> </em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

<tr>

<td>

<code>scale</code></br> <em> <a href="#kynomesh.kyno.sh/v1alpha1.Scale">
Scale </a> </em>
</td>

<td>

<em>(Optional)</em>
<p>

Settings for autoscaling
</p>

</td>

</tr>

<tr>

<td>

<code>initContainers</code></br> <em>
<a href="https://v1-18.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#container-v1-core">
\[\]Kubernetes core/v1.Container </a> </em>
</td>

<td>

<em>(Optional)</em>
<p>

List of customized init containers belonging to the pod. More info:
<a href="https://kubernetes.io/docs/concepts/workloads/pods/init-containers/">https://kubernetes.io/docs/concepts/workloads/pods/init-containers/</a>
</p>

</td>

</tr>

<tr>

<td>

<code>sidecars</code></br> <em>
<a href="https://v1-18.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#container-v1-core">
\[\]Kubernetes core/v1.Container </a> </em>
</td>

<td>

<em>(Optional)</em>
<p>

List of customized sidecar containers belonging to the pod.
</p>

</td>

</tr>

<tr>

<td>

<code>updateStrategy</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.UpdateStrategy"> UpdateStrategy </a>
</em>
</td>

<td>

<em>(Optional)</em>
<p>

The strategy to use to replace existing pods with new ones.
</p>

</td>

</tr>

</tbody>

</table>

<h3 id="kynomesh.kyno.sh/v1alpha1.AbstractPodTemplate">

AbstractPodTemplate
</h3>

<p>

(<em>Appears on:</em>
<a href="#kynomesh.kyno.sh/v1alpha1.AbstractAgentDeploy">AbstractAgentDeploy</a>,
<a href="#kynomesh.kyno.sh/v1alpha1.AgentDeployTemplate">AgentDeployTemplate</a>)
</p>

<p>

<p>

AbstractPodTemplate provides a template for pod customization in
vertices, daemon deployments and so on.
</p>

</p>

<table>

<thead>

<tr>

<th>

Field
</th>

<th>

Description
</th>

</tr>

</thead>

<tbody>

<tr>

<td>

<code>metadata</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.Metadata"> Metadata </a> </em>
</td>

<td>

<em>(Optional)</em>
<p>

Metadata sets the pods’s metadata, i.e. annotations and labels
</p>

</td>

</tr>

<tr>

<td>

<code>nodeSelector</code></br> <em> map\[string\]string </em>
</td>

<td>

<em>(Optional)</em>
<p>

NodeSelector is a selector which must be true for the pod to fit on a
node. Selector which must match a node’s labels for the pod to be
scheduled on that node. More info:
<a href="https://kubernetes.io/docs/concepts/configuration/assign-pod-node/">https://kubernetes.io/docs/concepts/configuration/assign-pod-node/</a>
</p>

</td>

</tr>

<tr>

<td>

<code>tolerations</code></br> <em>
<a href="https://v1-18.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#toleration-v1-core">
\[\]Kubernetes core/v1.Toleration </a> </em>
</td>

<td>

<em>(Optional)</em>
<p>

If specified, the pod’s tolerations.
</p>

</td>

</tr>

<tr>

<td>

<code>securityContext</code></br> <em>
<a href="https://v1-18.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#podsecuritycontext-v1-core">
Kubernetes core/v1.PodSecurityContext </a> </em>
</td>

<td>

<em>(Optional)</em>
<p>

SecurityContext holds pod-level security attributes and common container
settings. Optional: Defaults to empty. See type description for default
values of each field.
</p>

</td>

</tr>

<tr>

<td>

<code>imagePullSecrets</code></br> <em>
<a href="https://v1-18.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#localobjectreference-v1-core">
\[\]Kubernetes core/v1.LocalObjectReference </a> </em>
</td>

<td>

<em>(Optional)</em>
<p>

ImagePullSecrets is an optional list of references to secrets in the
same namespace to use for pulling any of the images used by this
PodSpec. If specified, these secrets will be passed to individual puller
implementations for them to use. For example, in the case of docker,
only DockerConfig type secrets are honored. More info:
<a href="https://kubernetes.io/docs/concepts/containers/images#specifying-imagepullsecrets-on-a-pod">https://kubernetes.io/docs/concepts/containers/images#specifying-imagepullsecrets-on-a-pod</a>
</p>

</td>

</tr>

<tr>

<td>

<code>priorityClassName</code></br> <em> string </em>
</td>

<td>

<em>(Optional)</em>
<p>

If specified, indicates the pod’s priority. “system-node-critical” and
“system-cluster-critical” are two special keywords which indicate the
highest priorities with the former being the highest priority. Any other
name must be defined by creating a PriorityClass object with that name.
If not specified, the pod priority will be default or zero if there is
no default. More info:
<a href="https://kubernetes.io/docs/concepts/configuration/pod-priority-preemption/">https://kubernetes.io/docs/concepts/configuration/pod-priority-preemption/</a>
</p>

</td>

</tr>

<tr>

<td>

<code>priority</code></br> <em> int32 </em>
</td>

<td>

<em>(Optional)</em>
<p>

The priority value. Various system components use this field to find the
priority of the pod. When Priority Admission Controller is enabled, it
prevents users from setting this field. The admission controller
populates this field from PriorityClassName. The higher the value, the
higher the priority. More info:
<a href="https://kubernetes.io/docs/concepts/configuration/pod-priority-preemption/">https://kubernetes.io/docs/concepts/configuration/pod-priority-preemption/</a>
</p>

</td>

</tr>

<tr>

<td>

<code>affinity</code></br> <em>
<a href="https://v1-18.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#affinity-v1-core">
Kubernetes core/v1.Affinity </a> </em>
</td>

<td>

<em>(Optional)</em>
<p>

The pod’s scheduling constraints More info:
<a href="https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/">https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/</a>
</p>

</td>

</tr>

<tr>

<td>

<code>serviceAccountName</code></br> <em> string </em>
</td>

<td>

<em>(Optional)</em>
<p>

ServiceAccountName applied to the pod
</p>

</td>

</tr>

<tr>

<td>

<code>runtimeClassName</code></br> <em> string </em>
</td>

<td>

<em>(Optional)</em>
<p>

RuntimeClassName refers to a RuntimeClass object in the node.k8s.io
group, which should be used to run this pod. If no RuntimeClass resource
matches the named class, the pod will not be run. If unset or empty, the
“legacy” RuntimeClass will be used, which is an implicit class with an
empty definition that uses the default runtime handler. More info:
<a href="https://git.k8s.io/enhancements/keps/sig-node/585-runtime-class">https://git.k8s.io/enhancements/keps/sig-node/585-runtime-class</a>
</p>

</td>

</tr>

<tr>

<td>

<code>automountServiceAccountToken</code></br> <em> bool </em>
</td>

<td>

<em>(Optional)</em>
<p>

AutomountServiceAccountToken indicates whether a service account token
should be automatically mounted.
</p>

</td>

</tr>

<tr>

<td>

<code>dnsPolicy</code></br> <em>
<a href="https://v1-18.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#dnspolicy-v1-core">
Kubernetes core/v1.DNSPolicy </a> </em>
</td>

<td>

<em>(Optional)</em>
<p>

Set DNS policy for the pod. Defaults to “ClusterFirst”. Valid values are
‘ClusterFirstWithHostNet’, ‘ClusterFirst’, ‘Default’ or ‘None’. DNS
parameters given in DNSConfig will be merged with the policy selected
with DNSPolicy. To have DNS options set along with hostNetwork, you have
to specify DNS policy explicitly to ‘ClusterFirstWithHostNet’.
</p>

</td>

</tr>

<tr>

<td>

<code>dnsConfig</code></br> <em>
<a href="https://v1-18.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#poddnsconfig-v1-core">
Kubernetes core/v1.PodDNSConfig </a> </em>
</td>

<td>

<em>(Optional)</em>
<p>

Specifies the DNS parameters of a pod. Parameters specified here will be
merged to the generated DNS configuration based on DNSPolicy.
</p>

</td>

</tr>

<tr>

<td>

<code>resourceClaims</code></br> <em>
<a href="https://v1-18.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#podresourceclaim-v1-core">
\[\]Kubernetes core/v1.PodResourceClaim </a> </em>
</td>

<td>

<em>(Optional)</em>
<p>

ResourceClaims defines which ResourceClaims must be allocated and
reserved before the Pod is allowed to start. The resources will be made
available to those containers which consume them by name.
</p>

</td>

</tr>

</tbody>

</table>

<h3 id="kynomesh.kyno.sh/v1alpha1.AgentDeploy">

AgentDeploy
</h3>

<p>

</p>

<table>

<thead>

<tr>

<th>

Field
</th>

<th>

Description
</th>

</tr>

</thead>

<tbody>

<tr>

<td>

<code>metadata</code></br> <em>
<a href="https://v1-18.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#objectmeta-v1-meta">
Kubernetes meta/v1.ObjectMeta </a> </em>
</td>

<td>

Refer to the Kubernetes API documentation for the fields of the
<code>metadata</code> field.
</td>

</tr>

<tr>

<td>

<code>spec</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.AgentDeploySpec"> AgentDeploySpec
</a> </em>
</td>

<td>

<br/> <br/>
<table>

<tr>

<td>

<code>AbstractAgentDeploy</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.AbstractAgentDeploy">
AbstractAgentDeploy </a> </em>
</td>

<td>

<p>

(Members of <code>AbstractAgentDeploy</code> are embedded into this
type.)
</p>

</td>

</tr>

<tr>

<td>

<code>agentSetName</code></br> <em> string </em>
</td>

<td>

<p>

AgentSetName is the name of the AgentSet that owns this AgentDeploy.
</p>

</td>

</tr>

<tr>

<td>

<code>replicas</code></br> <em> int32 </em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

<tr>

<td>

<code>topology</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.Topology"> Topology </a> </em>
</td>

<td>

<em>(Optional)</em>
<p>

Topology is stamped by the AgentSet controller and tells this agent
which peers it should discover.
</p>

</td>

</tr>

</table>

</td>

</tr>

<tr>

<td>

<code>status</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.AgentDeployStatus">
AgentDeployStatus </a> </em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

</tbody>

</table>

<h3 id="kynomesh.kyno.sh/v1alpha1.AgentDeployPhase">

AgentDeployPhase (<code>string</code> alias)
</p>

</h3>

<p>

(<em>Appears on:</em>
<a href="#kynomesh.kyno.sh/v1alpha1.AgentDeployStatus">AgentDeployStatus</a>)
</p>

<p>

</p>

<h3 id="kynomesh.kyno.sh/v1alpha1.AgentDeploySpec">

AgentDeploySpec
</h3>

<p>

(<em>Appears on:</em>
<a href="#kynomesh.kyno.sh/v1alpha1.AgentDeploy">AgentDeploy</a>)
</p>

<p>

</p>

<table>

<thead>

<tr>

<th>

Field
</th>

<th>

Description
</th>

</tr>

</thead>

<tbody>

<tr>

<td>

<code>AbstractAgentDeploy</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.AbstractAgentDeploy">
AbstractAgentDeploy </a> </em>
</td>

<td>

<p>

(Members of <code>AbstractAgentDeploy</code> are embedded into this
type.)
</p>

</td>

</tr>

<tr>

<td>

<code>agentSetName</code></br> <em> string </em>
</td>

<td>

<p>

AgentSetName is the name of the AgentSet that owns this AgentDeploy.
</p>

</td>

</tr>

<tr>

<td>

<code>replicas</code></br> <em> int32 </em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

<tr>

<td>

<code>topology</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.Topology"> Topology </a> </em>
</td>

<td>

<em>(Optional)</em>
<p>

Topology is stamped by the AgentSet controller and tells this agent
which peers it should discover.
</p>

</td>

</tr>

</tbody>

</table>

<h3 id="kynomesh.kyno.sh/v1alpha1.AgentDeployStatus">

AgentDeployStatus
</h3>

<p>

(<em>Appears on:</em>
<a href="#kynomesh.kyno.sh/v1alpha1.AgentDeploy">AgentDeploy</a>)
</p>

<p>

</p>

<table>

<thead>

<tr>

<th>

Field
</th>

<th>

Description
</th>

</tr>

</thead>

<tbody>

<tr>

<td>

<code>Status</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.Status"> Status </a> </em>
</td>

<td>

<p>

(Members of <code>Status</code> are embedded into this type.)
</p>

</td>

</tr>

<tr>

<td>

<code>phase</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.AgentDeployPhase"> AgentDeployPhase
</a> </em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

<tr>

<td>

<code>replicas</code></br> <em> uint32 </em>
</td>

<td>

<em>(Optional)</em>
<p>

Total number of non-terminated pods targeted by this AgentDeploy (their
labels match the selector).
</p>

</td>

</tr>

<tr>

<td>

<code>desiredReplicas</code></br> <em> uint32 </em>
</td>

<td>

<em>(Optional)</em>
<p>

The number of desired replicas.
</p>

</td>

</tr>

<tr>

<td>

<code>selector</code></br> <em> string </em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

<tr>

<td>

<code>reason</code></br> <em> string </em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

<tr>

<td>

<code>message</code></br> <em> string </em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

<tr>

<td>

<code>lastScaledAt</code></br> <em>
<a href="https://v1-18.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#time-v1-meta">
Kubernetes meta/v1.Time </a> </em>
</td>

<td>

<em>(Optional)</em>
<p>

Time of last scaling operation.
</p>

</td>

</tr>

<tr>

<td>

<code>observedGeneration</code></br> <em> int64 </em>
</td>

<td>

<em>(Optional)</em>
<p>

The generation observed by the AgentDeploy controller.
</p>

</td>

</tr>

<tr>

<td>

<code>readyReplicas</code></br> <em> uint32 </em>
</td>

<td>

<em>(Optional)</em>
<p>

The number of pods targeted by this AgentDeploy with a Ready Condition.
</p>

</td>

</tr>

<tr>

<td>

<code>updatedReplicas</code></br> <em> uint32 </em>
</td>

<td>

<p>

The number of Pods created by the controller from the AgentDeploy
version indicated by updateHash.
</p>

</td>

</tr>

<tr>

<td>

<code>updatedReadyReplicas</code></br> <em> uint32 </em>
</td>

<td>

<p>

The number of ready Pods created by the controller from the AgentDeploy
version indicated by updateHash.
</p>

</td>

</tr>

<tr>

<td>

<code>currentHash</code></br> <em> string </em>
</td>

<td>

<p>

If not empty, indicates the current version of the AgentDeploy used to
generate Pods.
</p>

</td>

</tr>

<tr>

<td>

<code>updateHash</code></br> <em> string </em>
</td>

<td>

<p>

If not empty, indicates the updated version of the AgentDeploy used to
generate Pods.
</p>

</td>

</tr>

</tbody>

</table>

<h3 id="kynomesh.kyno.sh/v1alpha1.AgentDeployTemplate">

AgentDeployTemplate
</h3>

<p>

(<em>Appears on:</em>
<a href="#kynomesh.kyno.sh/v1alpha1.Templates">Templates</a>)
</p>

<p>

</p>

<table>

<thead>

<tr>

<th>

Field
</th>

<th>

Description
</th>

</tr>

</thead>

<tbody>

<tr>

<td>

<code>AbstractPodTemplate</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.AbstractPodTemplate">
AbstractPodTemplate </a> </em>
</td>

<td>

<p>

(Members of <code>AbstractPodTemplate</code> are embedded into this
type.)
</p>

<em>(Optional)</em>
</td>

</tr>

<tr>

<td>

<code>brokerTemplate</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.ContainerTemplate">
ContainerTemplate </a> </em>
</td>

<td>

<em>(Optional)</em>
<p>

Template for the AgentDeploy broker container
</p>

</td>

</tr>

</tbody>

</table>

<h3 id="kynomesh.kyno.sh/v1alpha1.AgentPattern">

AgentPattern (<code>string</code> alias)
</p>

</h3>

<p>

(<em>Appears on:</em>
<a href="#kynomesh.kyno.sh/v1alpha1.AgentSetSpec">AgentSetSpec</a>,
<a href="#kynomesh.kyno.sh/v1alpha1.Topology">Topology</a>)
</p>

<p>

<p>

AgentPattern is the message-routing shape of an AgentSet.
</p>

</p>

<h3 id="kynomesh.kyno.sh/v1alpha1.AgentSet">

AgentSet
</h3>

<p>

</p>

<table>

<thead>

<tr>

<th>

Field
</th>

<th>

Description
</th>

</tr>

</thead>

<tbody>

<tr>

<td>

<code>metadata</code></br> <em>
<a href="https://v1-18.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#objectmeta-v1-meta">
Kubernetes meta/v1.ObjectMeta </a> </em>
</td>

<td>

Refer to the Kubernetes API documentation for the fields of the
<code>metadata</code> field.
</td>

</tr>

<tr>

<td>

<code>spec</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.AgentSetSpec"> AgentSetSpec </a>
</em>
</td>

<td>

<br/> <br/>
<table>

<tr>

<td>

<code>pattern</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.AgentPattern"> AgentPattern </a>
</em>
</td>

<td>

<p>

Pattern describes how the agents in this AgentSet are wired together.
</p>

<ul>

<li>

Supervisor: Entry sees every other agent; non-entry agents see no peers.
Aliases in the wider ecosystem include “manager”, “orchestrator-worker”,
and “subagents”.
</li>

<li>

Handoff: every agent sees every other agent. Aliases include “swarm” and
“network”.
</li>

<li>

Sequential: each agent sees only the next one in declaration order;
Entry must be agents\[0\].
</li>

</ul>

</td>

</tr>

<tr>

<td>

<code>entry</code></br> <em> string </em>
</td>

<td>

<p>

Entry is the name of the agent external callers reach first. Must match
one of agents\[\].name. For Sequential it must be agents\[0\].
</p>

</td>

</tr>

<tr>

<td>

<code>agents</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.AbstractAgentDeploy">
\[\]AbstractAgentDeploy </a> </em>
</td>

<td>

</td>

</tr>

<tr>

<td>

<code>templates</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.Templates"> Templates </a> </em>
</td>

<td>

<em>(Optional)</em>
<p>

Templates are used to customize additional kubernetes resources required
for the Pipeline
</p>

</td>

</tr>

</table>

</td>

</tr>

<tr>

<td>

<code>status</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.AgentSetStatus"> AgentSetStatus </a>
</em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

</tbody>

</table>

<h3 id="kynomesh.kyno.sh/v1alpha1.AgentSetPhase">

AgentSetPhase (<code>string</code> alias)
</p>

</h3>

<p>

(<em>Appears on:</em>
<a href="#kynomesh.kyno.sh/v1alpha1.AgentSetStatus">AgentSetStatus</a>)
</p>

<p>

</p>

<h3 id="kynomesh.kyno.sh/v1alpha1.AgentSetSpec">

AgentSetSpec
</h3>

<p>

(<em>Appears on:</em>
<a href="#kynomesh.kyno.sh/v1alpha1.AgentSet">AgentSet</a>)
</p>

<p>

</p>

<table>

<thead>

<tr>

<th>

Field
</th>

<th>

Description
</th>

</tr>

</thead>

<tbody>

<tr>

<td>

<code>pattern</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.AgentPattern"> AgentPattern </a>
</em>
</td>

<td>

<p>

Pattern describes how the agents in this AgentSet are wired together.
</p>

<ul>

<li>

Supervisor: Entry sees every other agent; non-entry agents see no peers.
Aliases in the wider ecosystem include “manager”, “orchestrator-worker”,
and “subagents”.
</li>

<li>

Handoff: every agent sees every other agent. Aliases include “swarm” and
“network”.
</li>

<li>

Sequential: each agent sees only the next one in declaration order;
Entry must be agents\[0\].
</li>

</ul>

</td>

</tr>

<tr>

<td>

<code>entry</code></br> <em> string </em>
</td>

<td>

<p>

Entry is the name of the agent external callers reach first. Must match
one of agents\[\].name. For Sequential it must be agents\[0\].
</p>

</td>

</tr>

<tr>

<td>

<code>agents</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.AbstractAgentDeploy">
\[\]AbstractAgentDeploy </a> </em>
</td>

<td>

</td>

</tr>

<tr>

<td>

<code>templates</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.Templates"> Templates </a> </em>
</td>

<td>

<em>(Optional)</em>
<p>

Templates are used to customize additional kubernetes resources required
for the Pipeline
</p>

</td>

</tr>

</tbody>

</table>

<h3 id="kynomesh.kyno.sh/v1alpha1.AgentSetStatus">

AgentSetStatus
</h3>

<p>

(<em>Appears on:</em>
<a href="#kynomesh.kyno.sh/v1alpha1.AgentSet">AgentSet</a>)
</p>

<p>

</p>

<table>

<thead>

<tr>

<th>

Field
</th>

<th>

Description
</th>

</tr>

</thead>

<tbody>

<tr>

<td>

<code>Status</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.Status"> Status </a> </em>
</td>

<td>

<p>

(Members of <code>Status</code> are embedded into this type.)
</p>

</td>

</tr>

<tr>

<td>

<code>phase</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.AgentSetPhase"> AgentSetPhase </a>
</em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

<tr>

<td>

<code>message</code></br> <em> string </em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

<tr>

<td>

<code>lastUpdated</code></br> <em>
<a href="https://v1-18.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#time-v1-meta">
Kubernetes meta/v1.Time </a> </em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

<tr>

<td>

<code>agentCount</code></br> <em> uint32 </em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

<tr>

<td>

<code>observedGeneration</code></br> <em> int64 </em>
</td>

<td>

<em>(Optional)</em>
<p>

The generation observed by the controller.
</p>

</td>

</tr>

</tbody>

</table>

<h3 id="kynomesh.kyno.sh/v1alpha1.ConditionType">

ConditionType (<code>string</code> alias)
</p>

</h3>

<p>

<p>

ConditionType is a valid value of Condition.Type
</p>

</p>

<h3 id="kynomesh.kyno.sh/v1alpha1.Container">

Container
</h3>

<p>

(<em>Appears on:</em>
<a href="#kynomesh.kyno.sh/v1alpha1.AbstractAgentDeploy">AbstractAgentDeploy</a>)
</p>

<p>

<p>

Container is used to define the container properties for user agent.
</p>

</p>

<table>

<thead>

<tr>

<th>

Field
</th>

<th>

Description
</th>

</tr>

</thead>

<tbody>

<tr>

<td>

<code>image</code></br> <em> string </em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

<tr>

<td>

<code>command</code></br> <em> \[\]string </em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

<tr>

<td>

<code>args</code></br> <em> \[\]string </em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

<tr>

<td>

<code>env</code></br> <em>
<a href="https://v1-18.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#envvar-v1-core">
\[\]Kubernetes core/v1.EnvVar </a> </em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

<tr>

<td>

<code>envFrom</code></br> <em>
<a href="https://v1-18.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#envfromsource-v1-core">
\[\]Kubernetes core/v1.EnvFromSource </a> </em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

<tr>

<td>

<code>volumeMounts</code></br> <em>
<a href="https://v1-18.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#volumemount-v1-core">
\[\]Kubernetes core/v1.VolumeMount </a> </em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

<tr>

<td>

<code>resources</code></br> <em>
<a href="https://v1-18.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#resourcerequirements-v1-core">
Kubernetes core/v1.ResourceRequirements </a> </em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

<tr>

<td>

<code>securityContext</code></br> <em>
<a href="https://v1-18.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#securitycontext-v1-core">
Kubernetes core/v1.SecurityContext </a> </em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

<tr>

<td>

<code>imagePullPolicy</code></br> <em>
<a href="https://v1-18.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#pullpolicy-v1-core">
Kubernetes core/v1.PullPolicy </a> </em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

<tr>

<td>

<code>readinessProbe</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.Probe"> Probe </a> </em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

<tr>

<td>

<code>livenessProbe</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.Probe"> Probe </a> </em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

<tr>

<td>

<code>ports</code></br> <em>
<a href="https://v1-18.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#containerport-v1-core">
\[\]Kubernetes core/v1.ContainerPort </a> </em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

</tbody>

</table>

<h3 id="kynomesh.kyno.sh/v1alpha1.ContainerTemplate">

ContainerTemplate
</h3>

<p>

(<em>Appears on:</em>
<a href="#kynomesh.kyno.sh/v1alpha1.AbstractAgentDeploy">AbstractAgentDeploy</a>,
<a href="#kynomesh.kyno.sh/v1alpha1.AgentDeployTemplate">AgentDeployTemplate</a>)
</p>

<p>

<p>

ContainerTemplate defines customized spec for a container
</p>

</p>

<table>

<thead>

<tr>

<th>

Field
</th>

<th>

Description
</th>

</tr>

</thead>

<tbody>

<tr>

<td>

<code>resources</code></br> <em>
<a href="https://v1-18.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#resourcerequirements-v1-core">
Kubernetes core/v1.ResourceRequirements </a> </em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

<tr>

<td>

<code>imagePullPolicy</code></br> <em>
<a href="https://v1-18.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#pullpolicy-v1-core">
Kubernetes core/v1.PullPolicy </a> </em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

<tr>

<td>

<code>securityContext</code></br> <em>
<a href="https://v1-18.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#securitycontext-v1-core">
Kubernetes core/v1.SecurityContext </a> </em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

<tr>

<td>

<code>env</code></br> <em>
<a href="https://v1-18.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#envvar-v1-core">
\[\]Kubernetes core/v1.EnvVar </a> </em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

<tr>

<td>

<code>envFrom</code></br> <em>
<a href="https://v1-18.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#envfromsource-v1-core">
\[\]Kubernetes core/v1.EnvFromSource </a> </em>
</td>

<td>

<em>(Optional)</em>
</td>

</tr>

</tbody>

</table>

<h3 id="kynomesh.kyno.sh/v1alpha1.Metadata">

Metadata
</h3>

<p>

(<em>Appears on:</em>
<a href="#kynomesh.kyno.sh/v1alpha1.AbstractPodTemplate">AbstractPodTemplate</a>)
</p>

<p>

</p>

<table>

<thead>

<tr>

<th>

Field
</th>

<th>

Description
</th>

</tr>

</thead>

<tbody>

<tr>

<td>

<code>annotations</code></br> <em> map\[string\]string </em>
</td>

<td>

</td>

</tr>

<tr>

<td>

<code>labels</code></br> <em> map\[string\]string </em>
</td>

<td>

</td>

</tr>

</tbody>

</table>

<h3 id="kynomesh.kyno.sh/v1alpha1.Peer">

Peer
</h3>

<p>

(<em>Appears on:</em>
<a href="#kynomesh.kyno.sh/v1alpha1.Topology">Topology</a>)
</p>

<p>

<p>

Peer is a single discoverable agent reference.
</p>

</p>

<table>

<thead>

<tr>

<th>

Field
</th>

<th>

Description
</th>

</tr>

</thead>

<tbody>

<tr>

<td>

<code>name</code></br> <em> string </em>
</td>

<td>

<p>

Name is the short agent name.
</p>

</td>

</tr>

<tr>

<td>

<code>kind</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.PeerKind"> PeerKind </a> </em>
</td>

<td>

<em>(Optional)</em>
<p>

Kind tells the broker how to reach this peer.
</p>

</td>

</tr>

<tr>

<td>

<code>url</code></br> <em> string </em>
</td>

<td>

<em>(Optional)</em>
<p>

URL is the full URL of the peer’s broker. For Managed peers it is
populated by the init container from cluster DNS; for External peers it
must be supplied by the user.
</p>

</td>

</tr>

</tbody>

</table>

<h3 id="kynomesh.kyno.sh/v1alpha1.PeerKind">

PeerKind (<code>string</code> alias)
</p>

</h3>

<p>

(<em>Appears on:</em>
<a href="#kynomesh.kyno.sh/v1alpha1.Peer">Peer</a>)
</p>

<p>

<p>

PeerKind is how the broker should treat a peer entry.
</p>

</p>

<h3 id="kynomesh.kyno.sh/v1alpha1.Probe">

Probe
</h3>

<p>

(<em>Appears on:</em>
<a href="#kynomesh.kyno.sh/v1alpha1.Container">Container</a>)
</p>

<p>

<p>

Probe is used to customize the configuration for Readiness and Liveness
probes.
</p>

</p>

<table>

<thead>

<tr>

<th>

Field
</th>

<th>

Description
</th>

</tr>

</thead>

<tbody>

<tr>

<td>

<code>initialDelaySeconds</code></br> <em> int32 </em>
</td>

<td>

<em>(Optional)</em>
<p>

Number of seconds after the container has started before liveness probes
are initiated. More info:
<a href="https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#container-probes">https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#container-probes</a>
</p>

</td>

</tr>

<tr>

<td>

<code>timeoutSeconds</code></br> <em> int32 </em>
</td>

<td>

<em>(Optional)</em>
<p>

Number of seconds after which the probe times out. More info:
<a href="https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#container-probes">https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#container-probes</a>
</p>

</td>

</tr>

<tr>

<td>

<code>periodSeconds</code></br> <em> int32 </em>
</td>

<td>

<em>(Optional)</em>
<p>

How often (in seconds) to perform the probe.
</p>

</td>

</tr>

<tr>

<td>

<code>successThreshold</code></br> <em> int32 </em>
</td>

<td>

<em>(Optional)</em>
<p>

Minimum consecutive successes for the probe to be considered successful
after having failed. Defaults to 1. Must be 1 for liveness and startup.
Minimum value is 1.
</p>

</td>

</tr>

<tr>

<td>

<code>failureThreshold</code></br> <em> int32 </em>
</td>

<td>

<em>(Optional)</em>
<p>

Minimum consecutive failures for the probe to be considered failed after
having succeeded. Defaults to 3. Minimum value is 1.
</p>

</td>

</tr>

</tbody>

</table>

<h3 id="kynomesh.kyno.sh/v1alpha1.RollingUpdateStrategy">

RollingUpdateStrategy
</h3>

<p>

(<em>Appears on:</em>
<a href="#kynomesh.kyno.sh/v1alpha1.UpdateStrategy">UpdateStrategy</a>)
</p>

<p>

<p>

RollingUpdateStrategy is used to communicate parameter for
RollingUpdateStrategyType.
</p>

</p>

<table>

<thead>

<tr>

<th>

Field
</th>

<th>

Description
</th>

</tr>

</thead>

<tbody>

<tr>

<td>

<code>maxUnavailable</code></br> <em>
k8s.io/apimachinery/pkg/util/intstr.IntOrString </em>
</td>

<td>

<em>(Optional)</em>
<p>

The maximum number of pods that can be unavailable during the update.
Value can be an absolute number (ex: 5) or a percentage of desired pods
(ex: 10%). Absolute number is calculated from percentage by rounding
down. Defaults to 25%. Example: when this is set to 30%, the old pods
can be scaled down to 70% of desired pods immediately when the rolling
update starts. Once new pods are ready, old pods can be scaled down
further, followed by scaling up the new pods, ensuring that the total
number of pods available at all times during the update is at least 70%
of desired pods.
</p>

</td>

</tr>

</tbody>

</table>

<h3 id="kynomesh.kyno.sh/v1alpha1.Scale">

Scale
</h3>

<p>

(<em>Appears on:</em>
<a href="#kynomesh.kyno.sh/v1alpha1.AbstractAgentDeploy">AbstractAgentDeploy</a>)
</p>

<p>

<p>

Scale defines the parameters for autoscaling.
</p>

</p>

<table>

<thead>

<tr>

<th>

Field
</th>

<th>

Description
</th>

</tr>

</thead>

<tbody>

<tr>

<td>

<code>disabled</code></br> <em> bool </em>
</td>

<td>

<em>(Optional)</em>
<p>

Whether to disable autoscaling. Set to “true” when using Kubernetes HPA
or any other 3rd party autoscaling strategies.
</p>

</td>

</tr>

<tr>

<td>

<code>min</code></br> <em> int32 </em>
</td>

<td>

<em>(Optional)</em>
<p>

Minimum replicas.
</p>

</td>

</tr>

<tr>

<td>

<code>max</code></br> <em> int32 </em>
</td>

<td>

<em>(Optional)</em>
<p>

Maximum replicas.
</p>

</td>

</tr>

<tr>

<td>

<code>lookbackSeconds</code></br> <em> uint32 </em>
</td>

<td>

<em>(Optional)</em>
<p>

Lookback seconds to calculate the average pending messages and
processing rate.
</p>

</td>

</tr>

<tr>

<td>

<code>targetProcessingSeconds</code></br> <em> uint32 </em>
</td>

<td>

<em>(Optional)</em>
<p>

TargetProcessingSeconds is used to tune the aggressiveness of
autoscaling for source vertices, it measures how fast you want the
vertex to process all the pending messages. Typically increasing the
value, which leads to lower processing rate, thus less replicas. It’s
only effective for source vertices.
</p>

</td>

</tr>

<tr>

<td>

<code>scaleUpCooldownSeconds</code></br> <em> uint32 </em>
</td>

<td>

<em>(Optional)</em>
<p>

ScaleUpCooldownSeconds defines the cooldown seconds after a scaling
operation, before a follow-up scaling up. It defaults to the
CooldownSeconds if not set.
</p>

</td>

</tr>

<tr>

<td>

<code>scaleDownCooldownSeconds</code></br> <em> uint32 </em>
</td>

<td>

<em>(Optional)</em>
<p>

ScaleDownCooldownSeconds defines the cooldown seconds after a scaling
operation, before a follow-up scaling down. It defaults to the
CooldownSeconds if not set.
</p>

</td>

</tr>

<tr>

<td>

<code>replicasPerScaleUp</code></br> <em> uint32 </em>
</td>

<td>

<em>(Optional)</em>
<p>

ReplicasPerScaleUp defines the number of maximum replicas that can be
changed in a single scaled up operation. The is use to prevent from too
aggressive scaling up operations
</p>

</td>

</tr>

<tr>

<td>

<code>replicasPerScaleDown</code></br> <em> uint32 </em>
</td>

<td>

<em>(Optional)</em>
<p>

ReplicasPerScaleDown defines the number of maximum replicas that can be
changed in a single scaled down operation. The is use to prevent from
too aggressive scaling down operations
</p>

</td>

</tr>

</tbody>

</table>

<h3 id="kynomesh.kyno.sh/v1alpha1.Status">

Status
</h3>

<p>

(<em>Appears on:</em>
<a href="#kynomesh.kyno.sh/v1alpha1.AgentDeployStatus">AgentDeployStatus</a>,
<a href="#kynomesh.kyno.sh/v1alpha1.AgentSetStatus">AgentSetStatus</a>)
</p>

<p>

<p>

Status is a common structure which can be used for Status field.
</p>

</p>

<table>

<thead>

<tr>

<th>

Field
</th>

<th>

Description
</th>

</tr>

</thead>

<tbody>

<tr>

<td>

<code>conditions</code></br> <em>
<a href="https://v1-18.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#condition-v1-meta">
\[\]Kubernetes meta/v1.Condition </a> </em>
</td>

<td>

<em>(Optional)</em>
<p>

Conditions are the latest available observations of a resource’s current
state.
</p>

</td>

</tr>

</tbody>

</table>

<h3 id="kynomesh.kyno.sh/v1alpha1.Templates">

Templates
</h3>

<p>

(<em>Appears on:</em>
<a href="#kynomesh.kyno.sh/v1alpha1.AgentSetSpec">AgentSetSpec</a>)
</p>

<p>

</p>

<table>

<thead>

<tr>

<th>

Field
</th>

<th>

Description
</th>

</tr>

</thead>

<tbody>

<tr>

<td>

<code>agent</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.AgentDeployTemplate">
AgentDeployTemplate </a> </em>
</td>

<td>

</td>

</tr>

</tbody>

</table>

<h3 id="kynomesh.kyno.sh/v1alpha1.Topology">

Topology
</h3>

<p>

(<em>Appears on:</em>
<a href="#kynomesh.kyno.sh/v1alpha1.AgentDeploySpec">AgentDeploySpec</a>)
</p>

<p>

<p>

Topology captures the information an agent needs to know about: the
routing pattern, whether it is the entry agent, and the set of peers it
is allowed to discover.
</p>

</p>

<table>

<thead>

<tr>

<th>

Field
</th>

<th>

Description
</th>

</tr>

</thead>

<tbody>

<tr>

<td>

<code>pattern</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.AgentPattern"> AgentPattern </a>
</em>
</td>

<td>

<em>(Optional)</em>
<p>

Pattern is the AgentSet’s routing pattern.
</p>

</td>

</tr>

<tr>

<td>

<code>isEntry</code></br> <em> bool </em>
</td>

<td>

<em>(Optional)</em>
<p>

IsEntry is true if this agent is the AgentSet’s entry agent.
</p>

</td>

</tr>

<tr>

<td>

<code>peers</code></br> <em> <a href="#kynomesh.kyno.sh/v1alpha1.Peer">
\[\]Peer </a> </em>
</td>

<td>

<em>(Optional)</em>
<p>

Peers lists the agents this agent is allowed to discover, derived from
Pattern. Names are short agent names.
</p>

</td>

</tr>

</tbody>

</table>

<h3 id="kynomesh.kyno.sh/v1alpha1.UpdateStrategy">

UpdateStrategy
</h3>

<p>

(<em>Appears on:</em>
<a href="#kynomesh.kyno.sh/v1alpha1.AbstractAgentDeploy">AbstractAgentDeploy</a>)
</p>

<p>

<p>

UpdateStrategy indicates the strategy that the controller will use to
perform updates for Vertex or MonoVertex.
</p>

</p>

<table>

<thead>

<tr>

<th>

Field
</th>

<th>

Description
</th>

</tr>

</thead>

<tbody>

<tr>

<td>

<code>type</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.UpdateStrategyType">
UpdateStrategyType </a> </em>
</td>

<td>

<em>(Optional)</em>
<p>

Type indicates the type of the StatefulSetUpdateStrategy. Default is
RollingUpdate.
</p>

</td>

</tr>

<tr>

<td>

<code>rollingUpdate</code></br> <em>
<a href="#kynomesh.kyno.sh/v1alpha1.RollingUpdateStrategy">
RollingUpdateStrategy </a> </em>
</td>

<td>

<em>(Optional)</em>
<p>

RollingUpdate is used to communicate parameters when Type is
RollingUpdateStrategy.
</p>

</td>

</tr>

</tbody>

</table>

<h3 id="kynomesh.kyno.sh/v1alpha1.UpdateStrategyType">

UpdateStrategyType (<code>string</code> alias)
</p>

</h3>

<p>

(<em>Appears on:</em>
<a href="#kynomesh.kyno.sh/v1alpha1.UpdateStrategy">UpdateStrategy</a>)
</p>

<p>

<p>

UpdateStrategyType is a string enumeration type that enumerates all
possible update strategies.
</p>

</p>

<hr/>

<p>

<em> Generated with <code>gen-crd-api-reference-docs</code>. </em>
</p>
