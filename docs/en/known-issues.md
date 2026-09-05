# Known issues

> [Deutsch](../de/known-issues.md) · Leading version: German

This list is deliberately complete and deliberately unvarnished. Whoever works
on the broker should know the mines before walking into them.

**The long form of each finding:** `korifi-platform/FINDINGS.md`. That is the
measurement log with observation, verified cause and proposal, sorted by
severity and by the run it came from. Here you get the short form with the code
location — and, which is missing there, the classification: **does it block a
target platform or only the development platform?** What separates the two is in
[target-platforms.md](target-platforms.md).

## Functional gaps

None of the open points currently blocks a target platform.

## Structural problems

## Definitions and deployment

**Two readiness paths are checked against their operator's CRD, not against a
running operator.** `valkey-cluster` and `seaweedfs-s3` carry
`status.conditions` according to the schema — whether the operator really
writes `Ready` there is something a schema does not say. Computed against a real
CR are only `cnpg-postgresql` and `rabbitmq-cluster`, whose operators run in the
development platform.

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

Korifi is being archived upstream — Cloud Foundry RFC 0060
(`toc/rfc/rfc-0060-archive-cf-on-k8s-wg.md`, status `Accepted`) archives the
`CF on K8S` working group and the Korifi repositories; CI is already switched
off.

**This is a tooling problem, not a product problem.** The target platforms are
untouched and the only coupling is the OSB API. The artefacts needed for the
current state — Helm chart, the three images by digest, the source tree — are
mirrored locally. What is open is what to develop against in the medium term;
the RFC names `cloudfoundry/kind-deployment` as the successor, which would be
closer to the target platform than Korifi. Classification in
[target-platforms.md](target-platforms.md).

## Suggested order

The points are connected; worked through in this order each one makes the next
visible or cheaper.

1. **`valkey-cluster` and `seaweedfs-s3` against a running operator** — the
   CRD schema says `status.conditions` exists, not that the operator writes
   `Ready` there. Needs those operators in the cluster.
2. **A run against a target system** — everything up to here is evidence from
   the development platform, and that is not the same as being deployable. See
   [target-platforms.md](target-platforms.md).
