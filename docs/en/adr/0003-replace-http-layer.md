# ADR 0003: Replace the HTTP layer, keep the engine

> [Deutsch](../../de/adr/0003-replace-http-layer.md) · Leading version: German

**Status:** **proposed** — not decided · **Affects:** `internal/handlers`, `internal/broker/broker.go`, `internal/store`

> This document records a recommendation, not a decision that has been taken. It
> is here so the reasoning can be read when the decision comes up.

## Context

A systematic reading of the code and two lifecycle runs against real operators
produced several findings that share one cause: **there are two complete broker
implementations in the same process.**

Every handler branches on its own through `resolveDefinition`
(`internal/handlers/definition_instances.go:17`). If it returns a definition,
the request goes through the engine; if it returns `nil` — including on error —
the request falls back silently to `internal/broker/broker.go`, a second broker
with its own instance and binding maps and a fake catalogue from
`internal/store`.

The immediate consequences:

- The demo catalogue appears in every production catalogue.
- The project's own conformance suite exercises the legacy path, because it
  takes the first service from the catalogue — which is why the missing binding
  persistence went unnoticed.
- `GET instance` and `GET binding` **always** go through the legacy path, even
  for definition services.
- Async was never wired up: `accepts_incomplete` is read in the wrong place, and
  the `last_operation` machinery runs empty.
- The HTTP status is guessed from error text.

The details are in [known-issues.md](../known-issues.md), the impact per target
platform in [target-platforms.md](../target-platforms.md). **Four of these
points are blockers for production Cloud Foundry and TAS**, and all four sit in
the same layer.

## What has changed since it was written

The original recommendation read "replace the HTTP **and** state layer". The
state half has been carried out in phase 5: the ConfigMap store is gone and
dedicated resource kinds with `RetryOnConflict` took its place — see
[ADR 0001](0001-kubernetes-as-state-store.md). What remains open is the HTTP
layer and the legacy broker behind it.

## Proposal

**Replace the HTTP layer, keep the engine.** Concretely:

- **One path instead of two.** The legacy broker and the demo catalogue are
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

The counter-check on the engine half has been run: the RabbitMQ run brought an
operator with a different CRD group, different condition types and a different
credential layout — and required **not a single** change to
`internal/definition`. That was the open objection against deciding on the
evidence of one service. It has been removed.

## Scope

As of this document, 6,560 lines of production code:

| Part | Lines | Verdict |
|---|---|---|
| `internal/definition` | 1,493 | keep |
| `internal/broker/crdstate.go` and around it | ~700 | keep, phase 5 |
| `internal/config`, `server`, `auth` | 1,011 | keep, phase 4.5 |
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
- `internal/auth`, `internal/server` and `internal/config` are deliberately
  written framework-agnostically and survive the change unmodified. That was the
  intent when phase 4.5 was built.
- The rebuild is a break inwards, not outwards: the OSB API stays what it is —
  see [ADR 0006](0006-platform-independence.md).

## Consequences if rejected

The four blockers remain, and the broker stays usable on the development
platform and unusable on a target platform. That is a legitimate decision as
long as it is made knowingly.
