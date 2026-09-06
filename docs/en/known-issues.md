# Known issues

> [Deutsch](../de/known-issues.md) · Leading version: German

This list is deliberately complete and deliberately unvarnished. Whoever works
on the broker should know the mines before walking into them.

**The long form of each finding:** `cfk8s-platform/FINDINGS.md`. That is the
measurement log with observation, verified cause and proposal, sorted by
severity and by the run it came from. Here you get the short form with the code
location — and, which is missing there, the classification: **does it block a
target platform or only the development platform?** What separates the two is in
[target-platforms.md](target-platforms.md).

## Functional gaps

**One open point blocks every target platform: the target namespace.**

The broker derives the namespace of the operator resources from the space GUID.
On a platform that creates a Kubernetes namespace per space, that works out.
**Cloud Foundry creates none** — spaces are records in the Cloud Controller, not
Kubernetes objects. Every provision ends there with

```
Service broker error: apply Cluster "osb-...": namespaces "<space-guid>" not found
```

That is not carelessness but an undecided question. Three directions, all with
consequences for tenant separation:

| Way | What it costs |
|---|---|
| one fixed namespace for all instances | simple, removes the separation between tenants |
| one namespace per org or space, **created by the broker itself** | the broker needs the right to create namespaces — and has to decide who cleans them up |
| a mapping the operator maintains | no automation, but explicit and auditable |

The code path is `namespaceOf` in `internal/handlers`; where the GUID comes from
is in `internal/broker/context.go`. While the decision is open, the development
platform creates the namespace by hand — a crutch, and it is labelled as one
there.

Apart from that, no open point blocks a target platform.

## Structural problems

## Definitions and deployment

**One readiness path is checked against its operator's CRD, not against a
running operator.** `seaweedfs-s3` carries `status.conditions` according to the
schema — whether the operator really writes `Ready` there is something a schema
does not say. Computed against a real CR are `cnpg-postgresql` and
`rabbitmq-cluster`, whose operators run in the development platform.

When a path misses, `last_operation` reports the reason together with the
condition names the operator actually publishes, and the run ends in `failed`
after `timeoutSeconds` instead of an endless poll.

**The `Dockerfile` declares `EXPOSE 8080`** while the chart with TLS listens on
8443. Harmless, but misleading.

## Dead code

None of it does harm, all of it costs reading time:

| Location | State |
|---|---|
| `internal/definition/operator.go` | `ApplyCR` and `ApplyManifests` only called from tests; `jsonField` unused |
| `internal/definition/render.go` | the methods `instanceID()` and `safeName()` are unreachable; the mechanism is `lowerCase()` |
| `internal/definition/engine.go`, `internal/handlers/*` | `var _ = …` as import keepers |
| `internal/handlers/engine.go` | `NewEngineHolder` takes a namespace and does not use it |
| `.github/workflows/ci.yml` | `actions/setup-go` appears twice in the `conformance` job |

## The development platform

The development platform is **real Cloud Foundry on kind** —
`cloud_controller_ng`, UAA, Diego, gorouter. What it says about the protocol
holds on a target system too, because it is the same software.

**Two things it still cannot say.** The broker sits in the same Kubernetes
cluster as the platform there; on a target system it runs separately
([ADR 0009](adr/0009-deployment-model.md)). And the Cloud Controller does not
validate the broker's certificate there — it runs with `skip_cert_verify: true`.
A successful registration over `https` is therefore **no** statement about the
trust anchor. Classification in [target-platforms.md](target-platforms.md).

## Suggested order

1. **A run against a target system.** All evidence so far comes from the
   development platform. What cannot be decided there is the **trust anchor**:
   a foreign platform validates the broker's certificate against its own store,
   and which of the three routes applies is the target system operator's call —
   an agreement, not code.

   The shape, by contrast, is settled: the broker runs as a Kubernetes
   Deployment in the operators' cluster
   ([ADR 0009](adr/0009-deployment-model.md)). That also settles that it may be
   a controller.
2. **What a managed service needs.** Quotas, deletion protection and the
   inventory breakdown are in place; backup and restore, point-in-time
   recovery and upgrades of existing instances are open. The list with the
   state of each is in [target-platforms.md](target-platforms.md); since
   [ADR 0008](adr/0008-depth-over-breadth.md) the effort goes there rather than
   into further catalogue entries.

   **Upgrades go through the protocol.** `maintenanceInfo` per plan: the
   catalogue names the state, the instance remembers the one applied,
   `cf services` shows the difference as `upgrade available`,
   `cf upgrade-service` triggers it, and a stale state is
   `422 MaintenanceInfoConflict`. The broker **offers** rather than imposes, and
   changes no instance without a request.

   **What falls away with it is an observation.** A periodic run also reported
   records without a definition and resources that had vanished. Those numbers
   are gone; a one-shot check on demand would be the way to get them back
   without getting the timer back.

   **No shipped definition names a state.** Without `maintenanceInfo` on a plan
   there is nothing to compare, and `cf services` never shows an upgrade. A state
   is a promise about the rendered output — it belongs on a definition whose
   transitions are proven.

   **The plan change is possible and is not promised.** The promise sits on each
   plan and applies to the plan an instance *leaves* — that is where the needed
   direction comes from: leaving the small plan is safe, leaving the large one
   would shrink storage that CloudNativePG cannot shrink, and would cost the
   instance its deletion guard. No shipped definition sets `planUpdateable`,
   because no transition is proven against a running operator; until then the
   change stays `422`.
3. **`seaweedfs-s3` against a running operator** — the CRD schema says
   `status.conditions` exists, not that the operator writes `Ready` there.
