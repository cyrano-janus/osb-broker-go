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

**State lives in Kubernetes objects, not in a database.**

This was implemented in two stages:

1. **First as a ConfigMap.** A single object carrying the entire state as JSON.
   Simple, and sufficient for a proof.
2. **Since phase 5 as dedicated resource kinds** — one `OSBServiceInstance` or
   `OSBServiceBinding` per record, group `broker.osb.io`, version `v1alpha1`.

The second stage was not a matter of taste. The ConfigMap had three concrete
defects:

- **The 1 MiB limit** of ConfigMaps is enough for roughly 514 instances. Beyond
  that every further write fails — and it fails for the *entire* state at once.
- **Every call rewrote the complete state.** That does not scale with the number
  of instances, it scales against it.
- **Writes were lost silently.** There was no mutex, and `save` read the
  `resourceVersion` freshly instead of taking it from the `load`. Two
  overlapping provisions overwrote each other without a conflict. CI never
  noticed, because `STORE_BACKEND=memory` was set there — the ConfigMap store
  was **never** exercised.

On top of that the ConfigMap required an RBAC rule Kubernetes does not grant in
that form: `create` cannot be restricted through `resourceNames`, because on
creation the object name is in the request body and no name exists at
authorization time to check against.

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

- No size limit any more, no rewriting of the entire state per call.
- Write conflicts are resolved cleanly through `RetryOnConflict`.
- The state is visible with `kubectl get osbi,osbb` — an operational advantage
  no database offers.
- Granular RBAC per resource kind instead of one rule on a single object.

**Price:**

- Two CRDs must be installed before the first start, otherwise every provision
  fails.
- The migration needed a tool (`cmd/osb-state-migrate`), and that tool had to
  **redeclare** the old structures: the earlier types had no JSON tags while the
  embedded context did. Read with today's types, the result would silently be an
  empty record.
- The in-memory store remains as a second implementation. Both have to pass the
  same contract suite (`internal/broker/statestore_contract_test.go`).

## Rejected alternatives

| Option | Why not |
|---|---|
| External database (PostgreSQL, MySQL) | contradicts the "no external store" goal; a second operational object |
| SQLite on a PVC | no server needed, but a storage dependency and no `kubectl` visibility |
| Many small ConfigMaps or secrets | solves the size limit, but without a query layer and without resource-kind RBAC |
