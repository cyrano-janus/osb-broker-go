# Architecture

> [Deutsch](../de/architecture.md) · Leading version: German

The broker is a single process that speaks OSB API 2.17 and turns it into
Kubernetes objects. What has to happen per service is not in the code but in a
YAML file — see [service-definitions.md](service-definitions.md).

## The path of a request

```
  Cloud Foundry / TAS / OSB client
            │  HTTPS, basic auth or mTLS
            ▼
  ┌─────────────────────────────────────────────────────────┐
  │ internal/server    http.Server, TLS, certificate reload  │
  │ internal/auth      authenticator chain (basic, mtls)     │
  ├─────────────────────────────────────────────────────────┤
  │ internal/handlers  gin router, OSB endpoints             │
  │                                                          │
  │        resolveDefinition(service_id)                     │
  │             ├── matches ──▶ engine path                  │
  │             └── no match ──▶ legacy path                 │
  ├──────────────────────┬──────────────────────────────────┤
  │ internal/definition  │ internal/broker                   │
  │ THE ENGINE           │ legacy broker + state store       │
  │ YAML ─▶ CR           │ internal/store: demo catalogue    │
  └──────────┬───────────┴───────────┬──────────────────────┘
             │                       │
             ▼                       ▼
   operator CRs                OSBServiceInstance
   (Cluster, RabbitmqCluster)  OSBServiceBinding
   + their credential secrets  + credentials secret
             │                       │
             └───────────┬───────────┘
                         ▼
                    Kubernetes
```

**The branching point is the first thing to understand.** It sits in
`internal/handlers/definition_instances.go:17`:

```go
func (h *Handlers) resolveDefinition(serviceID string) (*definition.ServiceDefinition, error) {
	if h.engine == nil || h.engine.Engine == nil { return nil, nil }
	sd, err := h.engine.Engine.DefinitionByServiceID(serviceID)
	if err != nil { return nil, nil } // not a definition service -> legacy path
	return sd, nil
}
```

If it returns a definition, the request goes through the engine. If it returns
`nil` — including on error — the request falls back **silently** to a second,
entirely separate broker in `internal/broker/broker.go`. Both paths exist side
by side and every handler branches on its own. This is the central structural
legacy of the repository; it is described in
[known-issues.md](known-issues.md) and in
[ADR 0003](adr/0003-replace-http-layer.md).

## Packages and sizes

As of this document: **6,560 lines of production code, 6,497 lines of tests** —
roughly one to one.

| Package | Lines | Responsibility |
|---|---|---|
| `internal/definition` | 1,493 | **the engine**: load a ServiceDefinition, render the template, apply CRs, evaluate readiness, shape credentials |
| `internal/broker` | 1,284 | the legacy broker (`broker.go`, 414) **and** the CRD state store (`crdstate.go`, 486) |
| `internal/handlers` | 1,253 | gin router, OSB endpoints, auth middleware, logging, metrics |
| `internal/config` | 411 | environment variables into one validated struct, fail fast |
| `internal/apis/v1alpha1` | 309 | Go types for the state CRDs |
| `internal/server` | 301 | `http.Server`, TLS, certificate hot reload, signal handling |
| `internal/auth` | 299 | authenticator chain, independent of gin |
| `internal/migrate` | 208 | one-shot import from the retired state ConfigMap |
| `internal/store` | 131 | static demo catalogue |
| `main.go` | 135 | wiring, nothing else |
| `cmd/osb-checker` | 658 | conformance suite, CI gate |
| `cmd/osb-state-migrate` | 66 | tool around `internal/migrate` |

## The layers in detail

### `internal/server` — the listener

Replaces the earlier `router.Run(":"+port)` since phase 4.5. A real
`http.Server` with timeouts set, graceful shutdown on SIGTERM, and a
`CertReloader` that periodically re-reads certificate, key and client CA.

**Why polling and not inotify:** Kubernetes projects a secret through an
atomically swapped `..data` symlink. An inotify watch on the leaf path goes
quiet after the first such swap. The reloader compares SHA-256 digests instead;
if a reload fails, the previous material stays valid and only a log line is
written. Reasoning in [ADR 0004](adr/0004-tls-and-mtls-no-oauth2.md).

### `internal/auth` — the chain

`Authenticator` with a three-valued error contract: `nil` (success),
`ErrNoCredentials` (the chain continues), `ErrInvalidCredentials` (the chain
remembers and continues anyway). Only at the end does it decide.

The chain speaks `net/http`, not gin — deliberately framework-agnostic, so that
replacing the HTTP layer does not drag it along. Basic auth compares SHA-256
digests with `subtle.ConstantTimeCompare`. The error response is identical for
every failure mode: saying which method failed would tell an unauthenticated
caller which methods are enabled at all.

### `internal/handlers` — the HTTP layer

The middleware order is load-bearing and documented in the code:

```
gin.New()
  → Recovery
  → structured logging              (one JSON line per request)
  → GET /healthz                    ← registered before auth, therefore free
  → GET /openapi.yaml, /schemas/…   ← free as well
  → GET /metrics + metrics middleware  ← middleware BEFORE auth, so 401s are counted
  → auth middleware                 ← authenticated from here on
  → API version middleware
  → the /v2 routes
```

Which endpoints exist and where they deviate from OSB 2.17 is in
[reference/osb-api.md](reference/osb-api.md).

**Error mapping** (`errors.go`): the HTTP status is guessed from the *text* of
the error — `strings.Contains` on "has existing bindings", "not found" and
similar. Central, and therefore in one place, but wrong: every DELETE error with
"not found" in its text becomes `410`, including "service not found".

**Correlation ID:** every request gets one, either from the incoming
`X-Correlation-ID` header or freshly generated, and it is echoed in the response
header. It appears in every log line — the entry point for
[how-to/debugging.md](how-to/debugging.md).

### `internal/definition` — the engine

The part that carries. This is where the actual work happens, and where the
reason lives that a new service needs no code.

| File | Responsibility |
|---|---|
| `definition.go` | types, `Parse`, `Validate`, parameter allowlist |
| `render.go` | `SanitizeInstanceName`, template data, `RenderProvision`, `SplitManifests` |
| `operator.go` | apply, delete and read CRs; secrets; multi-doc decoding |
| `engine.go` | orchestration, `InstanceRegistry`, `Catalog()` |
| `readiness.go` | readiness via gjson, credential extraction |
| `servicebinding.go` | CNCF Service Binding Specification |
| `update.go` | plan change and no-op detection |
| `load.go` | read the directory, sorted |

**Provision, step by step:**

1. `DefinitionByServiceID(service_id)`, then `PlanByID(plan_id)`.
2. `RenderProvision` fills `{{ .safeName }}`, `{{ .instanceID }}` and
   `{{ .plan.* }}` into the template.
3. `ApplyManifestRefs` splits the result at `\n---`, decodes every document,
   fills in missing `apiVersion`, `kind` and `namespace` from the definition and
   creates or updates. A document without `metadata.name` is a hard error.
4. The created objects are recorded on the state record as `ObjectRef` (group,
   version, kind, namespace, name) — the bookkeeping deprovision later relies on
   to know what to delete.

**Deprovision** works through that bookkeeping in three tiers: first the
`AppliedRefs` (multi-doc, each with its own kind), then the older
`AppliedObjects` (names only, kind from the definition), finally the fallback to
a single CR under `safeName`. The tiers exist because older records do not carry
the newer fields.

**Update** re-renders with the new plan's parameters and then compares before
writing. The reason is in the code: even a write that changes nothing bumps
`resourceVersion` and wakes up the operator's reconcile loop. The comparison
normalises both sides through JSON, because the same number arrives as `int64`
or `float64` depending on the path it took.

**Readiness** is **gjson**, not JSONPath. The path runs over the whole CR, not
just `status`, a leading dot is stripped, and a path that is not found means
*not ready yet* — never *error*. There is no `failed` state in the engine, and
`timeoutSeconds` is read but never enforced.

### `internal/broker` — two things in one package

The package carries two entirely different responsibilities, and that is the
core of the confusion on a first read:

- **`crdstate.go` (486 lines) is the state store** and is current. One
  `OSBServiceInstance` or `OSBServiceBinding` per record, writes via
  `RetryOnConflict`, credentials in a separate secret with an `OwnerReference`.
  Reasoning in [ADR 0001](adr/0001-kubernetes-as-state-store.md).
- **`broker.go` (414 lines) is the legacy broker** — a second, complete OSB
  implementation with its own catalogue from `internal/store`. It serves
  everything `resolveDefinition` does not recognise, and its demo services
  `service-1` and `service-2` appear in every catalogue.

The store derives object names from the OSB ID as long as that is a valid
DNS-1123 label of at most 63 characters — always the case with Cloud Foundry,
which sends UUIDs. Otherwise `osb-` plus a truncated SHA-256. The real ID is
always in `spec.id` and is re-checked on every read, so that a hash collision
cannot hand back the wrong record.

## Where the line runs

The recommendation from the architecture review reads: **keep the engine,
replace the HTTP layer.** What has happened since it was written belongs with
it — the state half of the recommendation is already done:

| Part | Lines | Verdict |
|---|---|---|
| `internal/definition` | 1,493 | carries |
| `internal/broker/crdstate.go` and around it | ~700 | carries, new in phase 5 |
| `internal/config`, `server`, `auth` | 1,011 | carries, new in phase 4.5 |
| `internal/apis/v1alpha1` | 309 | carries |
| `cmd/osb-checker` | 658 | carries |
| logging, metrics, docs endpoints | ~290 | carries, decoupled cross-cutting concerns |
| **dual path in the handlers** | ~580 | to be replaced |
| **`broker.go` (legacy broker)** | 414 | to be replaced |
| **`store.go` (demo catalogue)** | 131 | to be replaced |

The RabbitMQ run proved the engine half: an operator with a different CRD group,
different condition types and a different credential layout required **not a
single** change to `internal/definition`. What is missing is one path instead of
two, real async through a persisted operation record, and typed errors instead
of `strings.Contains`. The proposal for that is
[ADR 0003](adr/0003-replace-http-layer.md), status *proposed*.
