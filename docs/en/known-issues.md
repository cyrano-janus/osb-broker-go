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

**User parameters do not reach the template.** `Engine.ProvisionInstance`
accepts `parameters` and does not use them; `RenderProvision` only sets
`InstanceID`, `SafeName` and `Plan`. `TemplateData.Parameters` is populated
nowhere. `cf create-service -c` has no effect, and a `{{ .parameters.x }}` in a
template fails because of `missingkey=error`. *FINDINGS #17.*

**`readiness.timeoutSeconds` is never enforced.** A stuck operator makes the
instance report `in progress` forever. `last_operation` only reports `failed`
when the record exists and the object is gone — an operator that never satisfies
the readiness condition is not covered by it.

**A failed provision leaves orphaned CRs behind.** If applying breaks between
two documents, the first one stays without any record pointing at it.
*FINDINGS #6.*

**`osb_active_instances` and `osb_active_bindings` are never set.** Both gauges
are registered and permanently report 0.

## Structural problems

**Nothing demonstrates that the gate's checks can fail.** `cmd/osb-checker`
has no mutation suite: only `pickService` and `checkServiceBindingSpec` are
tested, not the checks themselves. A gate whose checks are ineffective is
indistinguishable from a green one — which is exactly what happened while the
selection always hit the demo service (*FINDINGS #20*, fixed). The standalone
checker has such a suite.

## Definitions and deployment

**The readiness paths of five definitions are unverified.**
`minio-objectstorage`, `redis-standalone`, `valkey-cluster`, `redpanda-cluster`
and `seaweedfs-s3` all name `status.conditions.#(type=="Ready").status` without
ever having been held against a CR of the respective operator — those operators
are not installed in the development platform. Only `cnpg-postgresql` and
`rabbitmq-cluster` are backed by evidence.

When a path misses, `last_operation` reports the reason together with the
condition names the operator actually publishes, thanks to the diagnostic in
`EvaluateReadiness`. The provisioning operation still runs into the platform's
timeout; the difference is that afterwards you know why.

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

1. **User parameters** — `cf create-service -c` has no effect as long as
   `TemplateData.Parameters` is populated by no caller. The check against
   `allowedParameters` is already in place, it just has nothing to check yet.
2. **Enforce `readiness.timeoutSeconds`** — until then a stuck operator reports
   `in progress` until the platform itself gives up. Only after that is a wrong
   readiness path visible even when nobody is looking.
3. **The five unverified readiness paths** — create one CR per operator and
   compute the path against it. Needs the operators in the cluster.
4. **Orphaned CRs after a failed provision** — attached to 2., because both
   concern the engine's abort path.
5. **A mutation suite for `cmd/osb-checker`** — the standalone checker has one,
   and on its first run it found two ineffective checks.
