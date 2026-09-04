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

### Blockers for production Cloud Foundry and TAS

**Provision always answers synchronously.** `accepts_incomplete` is modelled in
`internal/broker/types.go:24` as a field in the request body; OSB transmits it as
a query parameter. The branch in `internal/handlers/service_instances.go:42` is
therefore unreachable and `StatusAccepted` never occurs in the repository. The
broker reports "done" as soon as the CR is created — with CloudNativePG that is
minutes too early. The entire `last_operation` machinery exists and runs empty.
*FINDINGS #4 and #15.*

**Bindings on the definition path are not persisted.** `bindDefinition` never
calls `state.PutBinding`. `GET …/service_bindings/:bid` always goes through the
state store and therefore returns 404; `cf service-key` fails on it. A repeated
bind incorrectly answers `201`, and the 409 check "instance still has bindings"
can never fire for definition services. *FINDINGS #28.*

**`last_operation` for bindings is a constant.** `GetLastBindingOperation`
returns `succeeded` regardless of anything, including for unknown IDs.

**Two demo services in the production catalogue.** `internal/store` provides a
hardcoded catalogue with `service-1` and `service-2` that is prepended to every
`GET /v2/catalog`. There is no switch against it. *FINDINGS #9.*

### Functionally missing, not a breach

**User parameters do not reach the template.** `Engine.ProvisionInstance`
accepts `parameters` and does not use them; `RenderProvision` only sets
`InstanceID`, `SafeName` and `Plan`. `TemplateData.Parameters` is populated
nowhere. `cf create-service -c` has no effect, and a `{{ .parameters.x }}` in a
template fails because of `missingkey=error`. *FINDINGS #17.*

**`allowedParameters` is only checked on update.** `UpdateServiceInstance`
validates, `ProvisionServiceInstance` does not. On provision arbitrary
parameters are accepted and then discarded — not an error, just no effect, and
that is the more unpleasant variant.

**`readiness.timeoutSeconds` is never enforced.** A stuck operator makes the
instance report `in progress` forever. The engine has no `failed` state at all.

**A failed provision leaves orphaned CRs behind.** If applying breaks between
two documents, the first one stays without any record pointing at it.
*FINDINGS #6.*

**Unbind does not delete the record.** For definition services unbind only
removes the projected secret and does not call `broker.Unbind`.

**`osb_active_instances` and `osb_active_bindings` are never set.** Both gauges
are registered and permanently report 0.

## Structural problems

**Two complete broker implementations side by side.** Every handler branches on
its own through `resolveDefinition`
(`internal/handlers/definition_instances.go:17`); if it returns `nil` — including
on error — the request falls back silently to `internal/broker/broker.go`, a
second complete broker with its own catalogue. This is the root of several of
the points above. *FINDINGS #13.*

**The project's own conformance suite tests the wrong path.** `pickService` in
`cmd/osb-checker/checks/checks.go` takes the first service from the catalogue,
and that is always the demo service `service-1`. The suite therefore measures
the fallback path, not the engine — which is why the missing binding persistence
went unnoticed. *FINDINGS #20.*

**The HTTP status is guessed from error text.**
`internal/handlers/errors.go` decides via `strings.Contains`. Every DELETE error
with "not found" in its text becomes `410`, including "service not found".
*FINDINGS #18.*

**What follows from this:** the proposal to replace the HTTP layer and keep the
engine is recorded as [ADR 0003](adr/0003-replace-http-layer.md) with status
*proposed*. The state store is not affected by it.

## Definitions and deployment

**The RabbitMQ definition checks a condition that does not exist.**
`definitions/rabbitmq-cluster.yaml` names `type=="Ready"`; the operator
publishes `AllReplicasReady` and `ClusterAvailable`. *FINDINGS #22.*

**`values-kind.yaml` has drifted away from `definitions/`.** The file duplicates
all definitions as embedded YAML strings. The embedded RabbitMQ definition is
missing `provisionedService`, `mapping` and `type` — deploying with it silently
yields unshaped bindings.
On top of that three keys appear twice (`cnpg-postgresql.yaml`,
`minio-objectstorage.yaml`, `redis-standalone.yaml`), and the Valkey definition
names a different CRD group than the file under `definitions/`. **Nothing tests
this file.** *FINDINGS #21.*

**`rbac.operatorCRDs` does not cover all shipped definitions.**
`minio.min.io/tenants` and `redis.redis.opstreelabs.in/redis` are missing; both
services would get a 403 on provision.

**The chart does not render with its own defaults.**
`tls.certManager.enabled: true` meets an empty `issuerRef.name` and therefore a
`{{ required }}`. Intended, but surprising.

**The counter-check in CI does not block yet.** The standalone checker runs in
the `conformance` job with `continue-on-error: true`, because it trips over the
blockers listed above — bind and `GET binding` answer 404. Its report is in the
job summary and as an artifact. Once the blockers are gone, `continue-on-error`
goes with them.

**`config.logRequests` is read by no template** and has no corresponding
environment variable.

**The image version is pinned inconsistently.** `deploy/k8s/broker.yaml` says
`v14`, `Chart.yaml` has `appVersion: v9`, `values-kind.yaml` pins `v9`.

**The module path is a placeholder.** `go.mod` says
`github.com/example/osb-broker`, while `schemas/service-definition.schema.json`
(`$id`), `docs/openapi.yaml` (`contact.url`) and the chart's image reference
point at `github.com/cyrano-janus/osb-broker-go`.

**The `Dockerfile` declares `EXPOSE 8080`** while the chart with TLS listens on
8443. Harmless, but misleading.

## Dead code

None of it does harm, all of it costs reading time:

| Location | State |
|---|---|
| `internal/handlers/definition_instances.go` | `deprovisionDefinition` is never called |
| `internal/broker/broker.go` | `createOperation` and the whole `operations` map unused |
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

1. **Async** — read `accepts_incomplete` from the query, answer `202`, answer
   `last_operation` from the real readiness state. The most expensive open point
   and the only one that blocks a target platform immediately.
2. **The RabbitMQ condition** — the async fix is what makes it visible, because
   today nobody waits for readiness. It belongs in the same round.
3. **Dual path and conformance suite** — as long as the suite exercises the
   fallback path, all further work is measured against nothing.
4. **Binding persistence** — structurally attached to the dual path and falls
   with it.
5. **User parameters and `allowedParameters`** — together, because both concern
   the same route through the engine.
