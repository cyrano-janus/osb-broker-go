# ADR 0003: Replace the HTTP layer, keep the engine

> [Deutsch](../../de/adr/0003-replace-http-layer.md) · Leading version: German

**Status:** **accepted** · **Affects:** `internal/handlers`, `internal/broker/broker.go`, `internal/store`

## Context

Several of the open findings went back to a common cause: **there were two
complete broker implementations in the same process.**

Every handler branched individually through `resolveDefinition`. When it
returned a definition, the request went through the engine; when it returned
`nil` — including on error — the request fell through silently to
`internal/broker/broker.go`, a second broker with its own instance and binding
maps and a demo catalogue from `internal/store`.

The immediate consequences:

- The demo catalogue appeared in every production catalogue, with no switch
  against it.
- The project's own conformance suite exercised the fallback path, because it
  took the first service from the catalogue — which is why the missing binding
  persistence had never surfaced.
- `GET instance` and `GET binding` **always** went through the fallback path,
  even for definition services.
- `last_operation` for bindings was a constant.
- The HTTP status was guessed from error strings: every error containing "not
  found" became `410 Gone` on a `DELETE`, so an unknown plan looked exactly
  like an already-deleted instance.

## Scope

The state store is **not** affected by this decision. It lives in its own
resource kinds with `RetryOnConflict` and it holds
([ADR 0001](0001-kubernetes-as-state-store.md)). What was replaced is the HTTP
layer alone and the fallback broker behind it.

## Decision

**The HTTP layer is replaced, the engine stays.** Concretely:

- **One path instead of two.** The fallback broker and the demo catalogue are
  gone with no replacement; `internal/store` no longer exists. A service that
  matches no definition is an error — `400` with `ErrServiceUnknown` — not a
  silent fallback.
- **The catalogue is exactly what the engine knows.** With no definitions
  loaded, `GET /v2/catalog` answers with an empty list, not with invented
  services.
- **Typed errors instead of `strings.Contains`.**
  `internal/definition/errors.go` carries `ErrServiceUnknown`,
  `ErrPlanUnknown`, `ErrResourceGone` and `ErrParameterNotAllowed` under the
  parent category `ErrNotFound`. The HTTP layer maps values, not wordings.
- **`last_operation` for bindings is a real query.** `succeeded` for a known
  binding, `410 Gone` for an unknown one.

**No rewrite of the repository.** The engine is the expensive part, and it
holds.

## Why it paid off

The argument was never elegance, it was the cost of the alternative. Fixing the
open findings one by one was already a cut through six files **inside** the
dual-path structure — you paid almost the price of the rebuild and kept the
structure that produced the problem.

That the engine carries the rebuild is demonstrated: two operators with
different CRD groups, condition types and credential layouts run through it
without `internal/definition` knowing anything per service. The rebuild did not
touch it.

## Extent

| Part | Verdict |
|---|---|
| `internal/definition` | unchanged |
| `internal/broker/crdstate.go` and surroundings | unchanged |
| `internal/config`, `server`, `auth` | unchanged |
| `internal/apis/v1alpha1` | unchanged |
| `cmd/osb-gate` | unchanged |
| Dual path in the handlers | replaced |
| `internal/broker/broker.go` | reduced to state access |
| `internal/store` | deleted |

`internal/broker/broker.go` shrank from 437 to 100 lines, the handler layer
from roughly 580 lines of branching to a single path.

## Consequences

- The conformance suite exercises the engine, because that is all there is. The
  standalone checker no longer needs a `skip_services` list.
- `internal/auth`, `internal/server` and `internal/config` survived the change
  unmodified. That is exactly what they are cut for.
- The rebuild is a break inwards, not outwards: the OSB API remains what it is
  — see [ADR 0006](0006-platform-independence.md).
- Whoever wants to offer a service writes a ServiceDefinition. There is no
  second way any more, and that is the point.
