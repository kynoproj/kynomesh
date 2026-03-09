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

<h3 id="kynomesh.kyno.sh/v1alpha1.Status">

Status
</h3>

<p>

(<em>Appears on:</em>
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

<hr/>

<p>

<em> Generated with <code>gen-crd-api-reference-docs</code>. </em>
</p>
