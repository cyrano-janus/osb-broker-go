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
  │        definitionFor(service_id)                         │
  │             ├── matches ──▶ engine path                  │
  │             └── no match ──▶ 400 BadRequest              │
  ├──────────────────────┬──────────────────────────────────┤
  │ internal/definition  │ internal/broker                   │
  │ THE ENGINE           │ state store                       │
  │ YAML ─▶ CR           │ who exists, what is bound         │
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

**There is one path, and that is the first thing to understand.** Every handler
resolves the ServiceDefinition for the `service_id` through `definitionFor`
(`internal/handlers/definition_instances.go`):

```go
func (h *Handlers) definitionFor(serviceID string) (*definition.ServiceDefinition, error) {
	if serviceID == "" {
		return nil, fmt.Errorf("%w: service_id is required", definition.ErrServiceUnknown)
	}
	if h.engine == nil || h.engine.Engine == nil {
		return nil, fmt.Errorf("%w: no service definitions are loaded", definition.ErrServiceUnknown)
	}
	return h.engine.Engine.DefinitionByServiceID(serviceID)
}
```

If the engine does not know the service, that is `ErrServiceUnknown` and
therefore `400` — there is no fallback for a request to drop into, and the
catalogue is exactly what the engine knows. Why it was decided that way, and
what stood here before, is in [ADR 0003](adr/0003-replace-http-layer.md).

## Packages and sizes

**6,419 lines of production code, 6,281 lines of tests** — roughly one to one.

| Package | Lines | Responsibility |
|---|---|---|
| `internal/definition` | 1,586 | **the engine**: load a ServiceDefinition, render the template, apply CRs, evaluate readiness, shape credentials |
| `internal/broker` | 1,026 | state store: access (`broker.go`, 100) and the CRD state store (`crdstate.go`, 486) |
| `internal/handlers` | 1,253 | gin router, OSB endpoints, auth middleware, logging, metrics |
| `internal/config` | 411 | environment variables into one validated struct, fail fast |
| `internal/apis/v1alpha1` | 309 | Go types for the state CRDs |
| `internal/server` | 301 | `http.Server`, TLS, certificate hot reload, signal handling |
| `internal/auth` | 299 | authenticator chain, independent of gin |
| `internal/migrate` | 208 | imports state from a ConfigMap in the `state.json` format |
| `main.go` | 135 | wiring, nothing else |
| `cmd/osb-checker` | 658 | conformance suite, CI gate |
| `cmd/osb-state-migrate` | 66 | tool around `internal/migrate` |

## The layers in detail

### `internal/server` — the listener

An `http.Server` with timeouts set, graceful shutdown on SIGTERM, and a
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

**When a provision aborts, it is rolled back.** Whatever was already created
is deleted — including when only the record write fails. Either case would
otherwise leave objects no record points at: the operator would run an instance
that never existed for the broker, and no deprovision would ever find it again.
The abort reason wins in the message; if the cleanup fails too, that is appended
rather than replacing it.

**Deprovision** works through that bookkeeping in three tiers: first the
`AppliedRefs` (multi-doc, each with its own kind), then `AppliedObjects` (names
only, kind from the definition), finally the fallback to a single CR under
`safeName`. The tiers catch records that do not carry all three fields.

**Update** re-renders with the new effective configuration — plan plus merged
user parameters — and then compares before writing. The reason is in the code: even a write that changes nothing bumps
`resourceVersion` and wakes up the operator's reconcile loop. The comparison
normalises both sides through JSON, because the same number arrives as `int64`
or `float64` depending on the path it took.

The record is updated even when the manifest stays the same: a plan change with
no effect on the manifest, and a parameter the template does not read, both
change the state of the instance without changing its objects.

**Readiness** is **gjson**, not JSONPath. The path runs over the whole CR, not
just `status`, a leading dot is stripped, and a path that is not found means
*not ready yet* — never *error*.

*Not ready yet* turns into *failed* once the deadline has passed:
`timeoutSeconds` is measured from the CR's `creationTimestamp`, an absent value
means the schema default of 600, a negative value switches the deadline off.
The timestamp comes from the API server and survives a broker restart — a clock
inside the process would not. The message still carries the reason from the
readiness check: the timeout says *that* it is stuck, the reason says *what
on*.

### `internal/broker` — two things in one package

The package carries two entirely different responsibilities, and that is the
core of the confusion on a first read:

- **`crdstate.go` (486 lines) is the state store.** One
  `OSBServiceInstance` or `OSBServiceBinding` per record, writes via
  `RetryOnConflict`, credentials in a separate secret with an `OwnerReference`.
  Reasoning in [ADR 0001](adr/0001-kubernetes-as-state-store.md).
- **`broker.go` (100 lines) is the access to it** — read, write, forget, and
  the two OSB answers `GET instance` and `GET binding`.

The store derives object names from the OSB ID as long as that is a valid
DNS-1123 label of at most 63 characters — always the case with Cloud Foundry,
which sends UUIDs. Otherwise `osb-` plus a truncated SHA-256. The real ID is
always in `spec.id` and is re-checked on every read, so that a hash collision
cannot hand back the wrong record.

## Where the line runs

The decision read: **keep the engine, replace the HTTP layer.** The line runs
between the layers, not across them:

| Part | Lines | Verdict |
|---|---|---|
| `internal/definition` | 1,586 | carries |
| `internal/broker/crdstate.go` and around it | ~700 | carries |
| `internal/config`, `server`, `auth` | 1,011 | carries |
| `internal/apis/v1alpha1` | 309 | carries |
| `cmd/osb-checker` | 797 | carries |
| logging, metrics, docs endpoints | ~290 | carries, decoupled cross-cutting concerns |

That the engine carries is demonstrated: two operators with different CRD
groups, condition types and credential layouts run through it without
`internal/definition` knowing anything per service. What was replaced is the
layer above it: one path instead of two, a catalogue built from definitions
instead of demo data, and typed errors instead of `strings.Contains`. The
decision is [ADR 0003](adr/0003-replace-http-layer.md).
