# Troubleshooting

> [Deutsch](../../de/how-to/debugging.md) · Leading version: German

From the outside in: first the API, then the state, then the objects in the
cluster, finally the logs.

## First move: the catalogue without Cloud Foundry

Most failures that look like a Cloud Foundry problem are not one. Ask the broker
directly:

```bash
cd ../korifi-platform && make broker-catalog
```

The script pulls the CA from the TLS secret and the credentials from the auth
secret, opens a port-forward and calls `/v2/catalog` with a matching server name
— deliberately using `--resolve` instead of `-k`, because a call that does not
verify the certificate verifies nothing at all.

| Result | Meaning |
|---|---|
| Catalogue arrives, service missing | the pod was not restarted after the definition change |
| `401` | the credentials do not match |
| Connection refused | the pod is not running or listens on a different port |
| Certificate error | the TLS secret does not match the requested name |

## The state

The broker creates one CR per record. The state is therefore visible with
`kubectl`.

```bash
kubectl get osbi,osbb -A                       # short names for instances and bindings
kubectl get osbi -n osb-broker -o wide
kubectl describe osbi <name> -n osb-broker
```

The real OSB ID is in `spec.id`, not in the object name — the name is derived
from it and may be hashed:

```bash
kubectl get osbi -n osb-broker \
  -o custom-columns='OBJECT:.metadata.name,OSB-ID:.spec.id,NS:.spec.namespace,READY:.spec.ready'
```

A binding's credentials are **not** in the binding CR but in a separate secret
next to it:

```bash
kubectl get secret <binding-object-name>-credentials -n osb-broker \
  -o jsonpath='{.data.credentials\.json}' | base64 -d | jq
```

**For services that run through a definition there is no binding record**: the
definition path does not persist bindings, and `GET binding` answers 404. See
[reference/osb-api.md](../reference/osb-api.md).

## The objects in the cluster

```bash
kubectl get clusters.postgresql.cnpg.io -A      # or the operator's kind
kubectl describe cluster <osb-name> -n <space-ns>
```

Instances land in the **space namespace**, not in the broker namespace. The
object name starts with `osb-`.

**The readiness path is the most common source of error.** Check it on the
living object against the value in the definition:

```bash
kubectl get cluster <osb-name> -n <ns> -o json | \
  jq '.status.conditions[] | {type, status}'
```

If there is no entry with the type named in `statusJSONPath`, the broker reports
`in progress` forever — a gjson path that cannot be found means "not ready yet",
never "misconfigured". That is the difference between a service that is stuck
and a definition that is wrong, and from the outside they look the same.

## The logs

One JSON line per request. The entry point is the correlation ID:

```bash
kubectl logs -n osb-broker deploy/osb-broker -f | jq -c \
  '{t:.timestamp, cid:.correlation_id, m:.method, p:.path, s:.status, ms:.duration_ms, auth:.auth_method}'
```

Following a single operation:

```bash
kubectl logs -n osb-broker deploy/osb-broker | jq -c 'select(.correlation_id=="<id>")'
```

The ID comes from the incoming `X-Correlation-ID` header or is generated, and it
also appears in the response header. A caller can supply it:

```bash
curl -H 'X-Correlation-ID: my-search-42' ...
```

**Read the first log output after start-up.** `internal/config` reports five
non-fatal states as warnings: in-memory store active, no authentication, no TLS,
empty mTLS allowlist, downgraded `MTLS_REQUIRE`. Each of them explains some
strange behaviour later.

## Common failure patterns

| Symptom | Likely cause |
|---|---|
| New service missing from the catalogue | pod not restarted after the definition change |
| `cf marketplace` empty although the catalogue is right | plans not made visible |
| Provision ends with 403 | `rbac.operatorCRDs` does not cover the CRD |
| Provision ends with 500 on the first try | rights on the state CRDs missing, or the CRDs are not installed |
| Instance stuck at "in progress" | readiness path points at a condition that does not exist |
| `cf service-key` returns 404 | definition bindings are not persisted |
| Binding contains configuration files | `mapping` missing in the definition |
| Instance immediately counts as ready, bind fails | provision answers `201` synchronously; the operator is not there yet |
| `cf create-service -c '{…}'` ends with 400 | the key is not in the plan's `allowedParameters` |
| Pod does not start, error while loading | a file under `definitions/` does not parse |
| Certificate files unreadable | `fsGroup` missing; the files are mounted with mode `0440` |

The permanent ones among these are described with their code location in
[known-issues.md](../known-issues.md).

## Metrics

```bash
kubectl port-forward -n osb-broker deploy/osb-broker 9090:8443
curl -sk https://localhost:9090/metrics | grep '^osb_'
```

Nine collectors on their own registry rather than the default one. Useful are
`osb_requests_total` by status and `osb_last_operation_state`.

**`osb_active_instances{service_id,plan_id}` and
`osb_active_bindings{service_id}` are counted at scrape time**, not carried
along — so they survive a broker restart and also catch
changes that did not go through this process. If both are missing from a
scrape, the state store could not be read; how often is in
`osb_state_read_errors_total`. The gap in the graph is deliberate: a number
left standing would not be recognisable as a fault.

## When the platform itself is the suspect

The development platform has two tools of its own:

```bash
cd ../korifi-platform
make status      # current state at a glance
make doctor      # diagnosis when status shows something red
```

`make doctor` checks, among other things, volume references to secrets that do
not exist — the failure pattern left behind by a `cf unbind-service`, which
makes the app hang on its next start.
