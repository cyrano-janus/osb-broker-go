# OSB API: scope and deviations

> [Deutsch](../../de/reference/osb-api.md) · Leading version: German

**The machine-readable source is `docs/openapi.yaml`.** This document states the
scope and **where the broker deviates from OSB 2.17**. The deviations go
unnoticed on the development platform; their effect per target platform is in
[target-platforms.md](../target-platforms.md).

## Endpoints

All `/v2` routes sit behind authentication and the `X-Broker-API-Version` check.

| Method and path | Behaviour |
|---|---|
| `GET /v2/catalog` | exactly `engine.Catalog()`. An empty list when no definitions are loaded |
| `PUT /v2/service_instances/:id` | Provision. `202` with `operation`; without `?accepts_incomplete=true` **422** `AsyncRequired`; `200` for a known instance, `409` for differing parameters; `400` for an unknown service or plan. Checks `allowedParameters` |
| `PATCH /v2/service_instances/:id` | Plan change. Checks `allowedParameters`, re-renders, writes only on a real change; `200` |
| `DELETE /v2/service_instances/:id` | Deprovision. `410` for an unknown instance, `409` when bindings exist. `service_id` may be omitted, the service then comes from the record |
| `GET /v2/service_instances/:id` | from the state store; `404` for an unknown instance |
| `GET /v2/service_instances/:id/last_operation` | State from the operator's CR. `service_id` may be omitted, the service then comes from the record. `410` for an unknown instance, `failed` when the record exists and the object is gone |
| `PUT …/service_bindings/:bid` | Bind; `201`, `200` for a known binding |
| `DELETE …/service_bindings/:bid` | Unbind; `200`, `410` for an unknown binding |
| `GET …/service_bindings/:bid` | from the state store; `404` for an unknown binding or a foreign instance |
| `GET …/service_bindings/:bid/last_operation` | `succeeded` for a known binding, `410` for an unknown one. Bind is synchronous, there is no operation still running |

Reachable without authentication, deliberately:

| Path | Purpose |
|---|---|
| `GET /healthz` | Liveness and readiness. Registered before auth so a probe needs no certificate |
| `GET /metrics` | Prometheus. The metrics middleware sits **before** auth so that `401`s are counted too |
| `GET /openapi.yaml` | the specification, compiled into the binary |
| `GET /schemas/service-definition.schema.json` | the definition schema, likewise |

## Deviations from OSB 2.17

### `dashboard_url` is a constant

Every instance gets `https://dashboard.example.com/instances/<id>`. Without
consequence for Cloud Foundry, not so for a marketplace that offers the link.

### `readiness.timeoutSeconds` is read and never enforced

An operator that never satisfies the readiness condition makes `last_operation`
report `in progress` indefinitely. Since the readiness diagnostic the
description names the reason, but the state does not switch to `failed`.

## What is correct

So that the picture does not come out crooked — these points are conformant and
tested:

- The catalogue structure, including plans, tags and `bindable`.
- The negotiation via `X-Broker-API-Version`.
- Idempotent provision and bind, as far as the state is known.
- `410 Gone` when deprovisioning an unknown instance.
- One path with no fallback: an unknown `service_id` is `400`, not a silent
  switch into a second implementation.
- Status codes from error values instead of error strings — an unknown plan is
  `400` even on a `DELETE`, not `410`.
- The separation of authenticated and free paths.
- Basic auth with a constant-time comparison; mTLS as an equal alternative.
- The lifecycle end to end against real operators — CloudNativePG and RabbitMQ,
  each on the development platform.
- User parameters: `allowedParameters` per plan, checked on `PUT` and `PATCH`;
  `plan_id` is optional on `PATCH`; a repeated `PUT` with differing parameters
  is a `409`.

One choice OSB 2.17 leaves open: **`PATCH` merges parameters** rather than
replacing them. Keys that are sent override the stored ones, keys that are not
named stay — so `GET /v2/service_instances` reports the same set the instance
actually runs under, even when the platform only sends what changed. Reasoning
in [ADR 0007](../adr/0007-user-parameters.md).

The conformance suite `cmd/osb-checker` runs twice in CI: once over HTTP against
CloudNativePG, once over HTTPS with a client certificate against the Helm chart.
The standalone checker runs alongside it as a blocking counter-check.
