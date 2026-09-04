# ADR 0003: Replace the HTTP layer, keep the engine

> [Deutsch](../../de/adr/0003-replace-http-layer.md) · Leading version: German

**Status:** **proposed** — not decided · **Affects:** `internal/handlers`, `internal/broker/broker.go`, `internal/store`

> This document records a proposal, not a decision that has been taken.

## Context

Several of the open findings share one cause: **there are two complete broker
implementations in the same process.**

Every handler branches on its own through `resolveDefinition`
(`internal/handlers/definition_instances.go:17`). If it returns a definition,
the request goes through the engine; if it returns `nil` — including on error —
the request falls back silently to `internal/broker/broker.go`, a second broker
with its own instance and binding maps and a fake catalogue from
`internal/store`.

The immediate consequences:

- The demo catalogue appears in every production catalogue.
- The project's own conformance suite exercises the fallback path, because it
  takes the first service from the catalogue — which is why the missing binding
  persistence went unnoticed.
- `GET instance` and `GET binding` **always** go through the fallback path, even
  for definition services.
- Async was never wired up: `accepts_incomplete` is read in the wrong place, and
  the `last_operation` machinery runs empty.
- The HTTP status is guessed from error text.

The details are in [known-issues.md](../known-issues.md), the impact per target
platform in [target-platforms.md](../target-platforms.md). **Four of these
points are blockers for production Cloud Foundry and TAS**, and all four sit in
the same layer.

## Out of scope

The state store is **not** affected. It lives in dedicated resource kinds with
`RetryOnConflict` and carries
([ADR 0001](0001-kubernetes-as-state-store.md)). What is up for decision is the
HTTP layer alone, and the fallback broker behind it.

## Proposal

**Replace the HTTP layer, keep the engine.** Concretely:

- **One path instead of two.** The fallback broker and the demo catalogue are
  removed without replacement. A service that matches no definition is an error,
  not a silent fallback.
- **Real async** through a persisted operation record: read
  `accepts_incomplete` from the query, answer `202 Accepted` with an
  `operation`, answer `last_operation` from the real readiness state.
- **Persist bindings**, so that `GET binding` and idempotency are correct.
- **Typed errors** instead of `strings.Contains` on error text.

**No rewrite of the repository.** The engine is the expensive part, and it
carries.

## Why it pays off

The actual argument is not elegance but the cost of the alternative. Fixing the
open findings individually is **already** a cut through six files inside the
dual-path structure — you would pay almost the price of the rebuild and keep the
structure that produces the problem.

That the engine carries the rebuild is demonstrated: two operators with
different CRD groups, condition types and credential layouts run through it
without `internal/definition` knowing anything per service. The rebuild
therefore leaves it alone.

## Scope

Of 6,560 lines of production code:

| Part | Lines | Verdict |
|---|---|---|
| `internal/definition` | 1,493 | keep |
| `internal/broker/crdstate.go` and around it | ~700 | keep |
| `internal/config`, `server`, `auth` | 1,011 | keep |
| `internal/apis/v1alpha1` | 309 | keep |
| `cmd/osb-checker` | 658 | keep |
| logging, metrics, docs endpoints | ~290 | keep |
| dual path in the handlers | ~580 | replace |
| `internal/broker/broker.go` | 414 | replace |
| `internal/store` | 131 | remove |

Roughly 1,100 lines to replace, a good 4,400 stay.

## Consequences if accepted

- The conformance suite will exercise the engine afterwards, because that is all
  there is. Expect further deviations to become visible that are hidden behind
  the demo service today.
- `internal/auth`, `internal/server` and `internal/config` speak `net/http`
  rather than gin and survive the change unmodified. That is exactly what they
  are cut for.
- The rebuild is a break inwards, not outwards: the OSB API stays what it is —
  see [ADR 0006](0006-platform-independence.md).

## Consequences if rejected

The four blockers remain, and the broker stays usable on the development
platform and unusable on a target platform. That is a legitimate decision as
long as it is made knowingly.
