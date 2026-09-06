# ADR 0010: The target namespace comes from configuration, not from an assumption

> [Deutsch](../../de/adr/0010-instance-namespace.md) · Leading version: German

**Status:** accepted · **Affects:** `targetNamespace`, the chart, the rights

## Context

The broker creates an instance's resources in a Kubernetes namespace. Today it
derives it from the space GUID: `targetNamespace` in
`internal/handlers/definition_instances.go` returns `ctx.SpaceGUID`, otherwise
`default`.

**That is an assumption about the platform, and it does not hold.** It works out
when the platform creates a Kubernetes namespace per space with exactly that
name. Cloud Foundry does not — spaces are records in the Cloud Controller, not
Kubernetes objects. Every provision ends there with

```
Service broker error: apply Cluster "osb-…": namespaces "<space-guid>" not found
```

Tanzu TAS makes it plainer still: there the platform runs on BOSH VMs, and per
[ADR 0009](0009-deployment-model.md) the broker sits in a cluster of **its own**.
Between a CF space and a namespace of that cluster there is no connection anyone
established.

**What the platform really delivers.** On provision, Cloud Foundry sends a
`context` block with `platform`, `organization_guid`, `organization_name`,
`space_guid`, `space_name`, `instance_name` as well as
`organization_annotations` and `space_annotations` (`context_hash` in
`lib/services/service_brokers/v2/client.rb`). So the tenant identity is there —
only the mapping onto a namespace is missing, and OSB cannot supply it, because
OSB knows nothing about Kubernetes.

**Two constraints every solution has to keep.** First, only the provision
carries a context; deprovision, bind and `last_operation` carry none. The
mapping therefore has to be computed **once** and stored — which the broker
already does, `Instance.Namespace`, with `instanceNamespace` resolving it again
in three steps (FINDINGS #7/#16). Second, a namespace name is an RFC 1123 label;
org and space *names* are free text.

## Decision

**The operator configures the mapping, the broker applies it. Two values, both
with a safe default:**

| Value | Default | Effect |
|---|---|---|
| `instanceNamespace.template` | `osb-instances` | Go template over the provision context: `.orgGUID`, `.orgName`, `.spaceGUID`, `.spaceName`, `.platform`. Without placeholders, a fixed name. |
| `instanceNamespace.create` | `false` | Does the broker create a missing namespace? |

**The default is a fixed namespace, and the broker does not create it.** It
works on every platform, needs not a single new right, and a misconfigured
broker fails on the first provision with a message naming the missing namespace
— rather than quietly creating something nobody ordered.

**The result is validated, not bent into shape.** If the template yields no
valid RFC 1123 name — because a space is called "Team Grün (Q3)" — the provision
is an error saying exactly that. A silently mangled name puts two tenants into
the same namespace, and nobody notices.

**`create: true` is the explicit way out**, not a default. Whoever sets it gives
the broker `create` on `namespaces` and accepts that **nobody ever cleans them
up**: OSB knows only the deletion of an instance, not the end of a tenant. The
broker creates in that case and leaves standing.

## Consequences

- **No new right in the default.** The broker needs `create namespaces` only if
  someone sets `create: true`. With a fixed namespace the rights on
  `rbac.operatorCRDs`, cluster-wide today, can even be narrowed to that one
  namespace — the blast radius of a compromised broker pod shrinks.
- **Existing instances are untouched.** Their namespace is in
  `Instance.Namespace` and is read from there. The rule applies to new
  provisions only; there is no migration and none is needed.
- **A fixed namespace does not separate tenants at the Kubernetes level.** That
  is defensible and still has to be said: the consumers are CF developers who
  never see `kubectl` — their boundary is the OSB API, not the namespace. Whoever
  shares the cluster with foreign workloads or needs quotas per tenant sets a
  template with `.orgGUID` and creates the namespaces up front.
- **There are no name collisions.** Object names derive from the instance ID
  (`osb-<guid>`), which is globally unique; a shared namespace changes nothing
  about that.
- **The operator has to do something.** Without configuration and without a
  created namespace, nothing runs. That is deliberate: a platform where
  instances land without anyone having decided where is worse than one that
  stops on the first attempt.

## Scope

**Not decided is whether the broker ever becomes a controller.** A controller
could own namespaces and clean them up again. It sits behind the target-system
run, and this decision does not wait for it.

**Explicitly rejected: annotations as the placement source.** Cloud Foundry
passes `space_annotations` and `organization_annotations` through, and it is
tempting to have the namespace written there — declarative, without broker
configuration, per tenant. **But space annotations are set by the space manager,
that is, by the tenant.** They could pick another tenant's namespace with them. A
placement decision belongs to the operator of the platform, not to its user.

**Explicitly rejected: a maintained mapping table per space.** It would be
precise and it would be a ticket per space — the same shape the Valkey
definition failed on: a service whose provisioning needs a manual step does not
belong in a marketplace.
