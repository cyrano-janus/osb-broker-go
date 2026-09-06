# ADR 0011: The broker owns the protocol, the operator owns the lifecycle

> [Deutsch](../../de/adr/0011-broker-operator-boundary.md) · Leading version: German

**Status:** accepted · **Affects:** every question of the form "should the broker do this?"

## Context

The broker translates OSB into Kubernetes. On both sides of that translation
sits software that could do the same thing: the broker *can* drag a resource
along, watch states, clean up — and so can the operator, for the service it
runs.

**Without a boundary the broker grows into a second operator.** Every step in
that direction is individually reasonable. A loop that pulls diverging
instances along sounds like diligence. So does a cleaner for orphaned objects.
Only in sum does a piece of software emerge that runs the lifecycle of services
it does not understand — doing twice what the operator already does, and worse,
because it lacks the operator's knowledge.

[ADR 0009](0009-deployment-model.md) decided that the broker runs in the
operators' cluster, and derived from that that it *may* be a controller. What
it *should* do it deliberately left open. This ADR decides that.

## Decision

> **The broker owns the OSB protocol, the operator owns the lifecycle of the
> resource.**
>
> Every change the broker makes to **foreign** state has to be the answer to a
> question somebody has just asked in OSB terms. **What happens even when
> nobody asks is the operator's work.**

**"Foreign state" is the decisive restriction.** What the broker does to itself
does not fall under it: it reloads its certificate periodically, it keeps its
metrics. Neither touches anything outside its process, and both serve only its
ability to answer. The rule applies to operator resources, secrets, namespaces —
to everything somebody else sees.

With that, every case sorts itself:

| What | Who asks? | Whose work |
|---|---|---|
| Catalogue, `free`, `plan_updateable`, plan schemas | `GET /v2/catalog` | **Broker** — the operator knows no plans |
| Checking parameter limits | `PUT`/`PATCH` | **Broker** — at the protocol boundary, otherwise the user gets an apply error instead of a message |
| Reading readiness from the CR | `last_operation` | **Broker** — translation, not a decision |
| Reading credentials from the secret | `PUT binding` | **Broker** — the operator *creates* the secret |
| Reloading the certificate, metrics | — | **Broker** — its own state, see above |
| Dragging existing instances along | nobody | **Operator** |
| Cleaning up orphaned objects | nobody | **Operator** |

## Consequences

- **No timer in the broker that changes foreign state.** A guard keeps the way
  shut: `TestKeinZeitgeberSchreibtOhneRequest` searches the whole source tree
  for the abolished configuration. Whoever revives it has to delete the guard
  first — and thereby makes the decision deliberately.
- **Upgrades run through the protocol.** `maintenanceInfo` per plan: the
  catalogue names the state, the platform shows the difference, the owner
  triggers it. The broker then re-renders inside a request, with
  `last_operation` and an error somebody sees.
- **The broker creates nothing it cannot remove again.** OSB knows the deletion
  of an *instance*, not the end of a tenant. So it creates no namespace unless
  explicitly allowed to ([ADR 0010](0010-instance-namespace.md)), and `delete`
  on namespaces appears in no role.
- **A new service needs an operator that creates its own credentials secret.**
  That is not convenience but the same rule: a broker that *produces*
  credentials instead of passing them through has taken over the lifecycle.
  More candidates have failed on this criterion than on licensing.
- **What the rule costs is an observation.** A periodic run also reported
  records without a definition and resources that had vanished. Those numbers
  do not exist. A one-shot check on demand would be the way to get them back
  without getting the timer back — it would run on request and thus fall on the
  right side.

## Scope

**This forbids no controller.** It forbids a background writer *inside the
broker*. Where controller work is needed it belongs in a component on the
operator's side — with its own lifecycle, its own justification and its own
decision. [ADR 0009](0009-deployment-model.md) stands: the broker *may* be one.
It *should* not be one.

**One gap stays open and is named.** If the pod dies between applying the
manifests and writing the record, an orphaned CR remains; the rollback runs
only while the process lives. Closing that is controller work, and it therefore
sits on the other side of the boundary. It does not wait on this ADR but on a
decision of its own.

**This says nothing about how much the broker offers.** The size of the
catalogue is governed by [ADR 0008](0008-depth-over-breadth.md); here it is
only about who does which work.
