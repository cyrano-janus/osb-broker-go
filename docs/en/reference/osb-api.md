# OSB API: scope and deviations

> [Deutsch](../../de/reference/osb-api.md) · Leading version: German

**The machine-readable source is `docs/openapi.yaml`.** This document states the
scope and, more importantly, **where the broker deviates from OSB 2.17**. Those
deviations are the reason it exists: several of them go unnoticed on the
development platform and block on a target platform. Which ones is in
[target-platforms.md](../target-platforms.md).

## Endpoints

All `/v2` routes sit behind authentication and the `X-Broker-API-Version` check.

| Method and path | Behaviour |
|---|---|
| `GET /v2/catalog` | Union of the static demo catalogue and `engine.Catalog()` |
| `PUT /v2/service_instances/:id` | Provision. Definition path: `202` with `operation`, without `?accepts_incomplete=true` **422** `AsyncRequired`; `200` for a known instance, `409` for differing parameters. Fallback path synchronous `201`/`200` |
| `PATCH /v2/service_instances/:id` | Plan change. Checks `allowedParameters`, re-renders, writes only on a real change; `200` |
| `DELETE /v2/service_instances/:id` | Deprovision. `410` for an unknown instance, `409` when bindings exist (fallback path only) |
| `GET /v2/service_instances/:id` | **Always** through the fallback path, i.e. through the state store |
| `GET /v2/service_instances/:id/last_operation` | State from the operator's CR. `service_id` may be omitted, the service then comes from the record. `410` for an unknown instance, `failed` when the record exists and the object is gone |
| `PUT …/service_bindings/:bid` | Bind; `201`, `200` for a known binding |
| `DELETE …/service_bindings/:bid` | Unbind; `200`, `410` for an unknown binding |
| `GET …/service_bindings/:bid` | **Always** through the state store |
| `GET …/service_bindings/:bid/last_operation` | hardcoded `succeeded` |

Reachable without authentication, deliberately:

| Path | Purpose |
|---|---|
| `GET /healthz` | Liveness and readiness. Registered before auth so a probe needs no certificate |
| `GET /metrics` | Prometheus. The metrics middleware sits **before** auth so that `401`s are counted too |
| `GET /openapi.yaml` | the specification, compiled into the binary |
| `GET /schemas/service-definition.schema.json` | the definition schema, likewise |

## Deviations from OSB 2.17

### `last_operation` for bindings is a constant

`GetLastBindingOperation` returns `succeeded` regardless of anything, including
for unknown IDs.

### User parameters do not reach the template

`Engine.ProvisionInstance` accepts a `parameters` argument and does not use it;
`RenderProvision` only sets `InstanceID`, `SafeName` and `Plan`.
`cf create-service -c '{...}'` therefore has no effect.

### `allowedParameters` is only checked on update

`UpdateServiceInstance` calls `ValidatePlanParamsForService`,
`ProvisionServiceInstance` does not. On provision arbitrary parameters are
accepted and then discarded. This is the more unpleasant variant of point four:
not an error, just no effect.

### The HTTP status is guessed from error text

`internal/handlers/errors.go` decides via `strings.Contains`:

| Text in the error | Status |
|---|---|
| `has existing bindings` | 409 |
| `already exists with different` | 409 |
| `instance not found` | 404 |
| `not found` | 400 |
| otherwise | 500 |

`respondOSBError` then overrides every **DELETE** error with "not found" in its
text to `410 Gone` — including "service not found", where `400` would be right.

### Two demo services in the catalogue

`internal/store` provides a hardcoded catalogue with `service-1`
(`example-service`) and `service-2` (`database-service`), which is prepended to
the definition services on every `GET /v2/catalog`. There is no switch against
it.

This has a second, more unpleasant effect: the project's own conformance suite
`cmd/osb-checker` takes the **first** match from the catalogue in `pickService` —
and that is always `service-1`. The suite therefore exercises the fallback path,
not the engine. This is why the missing binding persistence went unnoticed.

### `dashboard_url` is a constant

Both paths set `https://dashboard.example.com/instances/<id>`. Harmless for
Cloud Foundry, not harmless for a marketplace that offers the link.

## What is correct

So that the picture does not come out crooked — these points are conformant and
tested:

- The catalogue structure, including plans, tags and `bindable`.
- The negotiation via `X-Broker-API-Version`.
- Idempotent provision and bind, as far as the state is known.
- `410 Gone` when deprovisioning an unknown instance.
- The separation of authenticated and free paths.
- Basic auth with a constant-time comparison; mTLS as an equal alternative.
- The lifecycle end to end against real operators — CloudNativePG and RabbitMQ,
  each on the development platform.

The conformance suite `cmd/osb-checker` runs 24 checks and runs twice in CI:
once over HTTP against CloudNativePG, once over HTTPS with a client certificate
against the Helm chart.
