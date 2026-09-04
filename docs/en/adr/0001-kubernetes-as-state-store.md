# ADR 0001: Kubernetes is the only state store

> [Deutsch](../../de/adr/0001-kubernetes-as-state-store.md) · Leading version: German

**Status:** accepted · **Affects:** `internal/broker`, `internal/apis/v1alpha1`, `deploy/crds`

## Context

A service broker has to remember which instances and bindings it created.
Without that memory it can neither deprovision nor answer idempotently after a
restart.

The obvious route would be a database. That contradicts the goal: the broker is
meant to be a single process running next to the operators in the cluster,
rolled out by creating a Deployment. A database would be a second operational
object with its own backup, its own failover and its own lifecycle — for a
memory that amounts to a few kilobytes per instance.

## Decision

**State lives in Kubernetes objects, not in a database — one object per
record.**

The resource kinds are `OSBServiceInstance` and `OSBServiceBinding`, group
`broker.osb.io`, version `v1alpha1`. A provision creates one object, a
deprovision removes it; no call touches the state of other instances.

Four requirements follow from that and are the reason for this shape:

- **No size limit.** State grows with the number of instances, not towards a
  fixed ceiling.
- **No rewriting of the entire state per call.** A provision writes exactly one
  object.
- **Conflict handling instead of silent overwriting.** Writes go through
  `RetryOnConflict` and replace only `.Spec`; `resourceVersion` and third-party
  annotations stay in place.
- **RBAC that can grant `create` at all.** Rights apply per resource kind, not
  per object name — Kubernetes cannot restrict `create` through `resourceNames`,
  because on creation the object name is in the request body and no name exists
  at authorization time to check against.

## Detail decisions

**Credentials live in a separate secret, not in the binding CR.** A CR is
readable by anyone who may read the resource kind; a secret has its own RBAC
layer. The secret is named `<objectname>-credentials`, carries an
`OwnerReference` on the binding, and is **additionally** deleted explicitly —
garbage collection runs asynchronously, and a leftover secret contains real
database credentials.

**A missing credential secret on read is not an error.** A hard error would make
the binding undeletable.

**No status subresource.** The broker is not a controller; nobody reconciles
these objects. A status subresource would suggest a separation that does not
exist.

**Object names are derived from the OSB ID**, as long as that is a valid
DNS-1123 label of at most 63 characters — always the case with Cloud Foundry,
which sends UUIDs. Otherwise `osb-` plus a truncated SHA-256. The real ID is
always in `spec.id` and is re-checked on every read, so that a hash collision
cannot hand back the wrong record.

**The CRDs are not in the Helm chart.** Cluster-wide objects in a namespaced
release collide between releases, and Helm never updates the `crds/` directory
on a `helm upgrade`. They are installed with `kubectl apply -f deploy/crds/`.

## Consequences

**Good:**

- The state is visible with `kubectl get osbi,osbb` — an operational advantage
  no database offers.
- Granular RBAC per resource kind instead of one rule on a single object.
- A broken record affects one record, not the whole holding.

**Price:**

- Two CRDs must be installed before the first start, otherwise every provision
  fails.
- There are two implementations of the interface — the CRD-backed one and the
  in-memory one for tests. Both have to pass the same contract suite
  (`internal/broker/statestore_contract_test.go`).
- State in a different format cannot simply be read in; `cmd/osb-state-migrate`
  exists for that.

## Rejected alternatives

**A single shared object for the entire state** — a ConfigMap holding JSON — is
the most obvious route and fails in four places at once: the 1 MiB limit is
enough for roughly 514 instances and makes *every* write fail beyond that; every
call rewrites the whole state and therefore scales against the number of
instances; two overlapping provisions overwrite each other without a conflict as
soon as the `resourceVersion` is fetched anew between read and write; and RBAC
cannot restrict `create` to an object name, so the right applies to the whole
resource kind anyway.

| Option | Why not |
|---|---|
| One ConfigMap for the entire state | size limit, whole-state write per call, no usable conflict handling, no meaningful RBAC |
| External database (PostgreSQL, MySQL) | contradicts the "no external store" goal; a second operational object |
| SQLite on a PVC | no server needed, but a storage dependency and no `kubectl` visibility |
| Many small ConfigMaps or secrets | solves the size limit, but without a query layer and without resource-kind RBAC |
