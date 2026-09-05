# Configuration

> [Deutsch](../../de/reference/configuration.md) · Leading version: German

The broker reads its configuration exclusively from environment variables.
`internal/config` reads them in one place, turns them into **one** validated
struct and aborts at start-up if something is wrong.

**Fail fast instead of a silent fallback.** An unknown value in `STORE_BACKEND`
is a start-up error. A silent fallback to the in-memory store would be the more
dangerous answer: the broker would run and lose all state on restart.

## Environment variables

### Basics

| Variable | Default | Effect |
|---|---|---|
| `PORT` | `8080` | Listening port. With TLS the chart sets `8443`. |
| `STORE_BACKEND` | `memory` | `crd` or `memory`. `k8s` is an alias for `crd` and produces a warning. Anything else is a **start-up error**. |
| `POD_NAMESPACE` | empty | Namespace holding the state CRs. Required with `STORE_BACKEND=crd`; the chart fills it via `fieldRef`. |
| `DEFINITIONS_DIR` | empty | Directory holding the ServiceDefinitions. Empty means an empty catalogue; the broker offers nothing. |
| `METRICS_ENABLED` | on | **Only the exact value `0` turns it off.** No `ParseBool`: `false` leaves metrics on. |

### Authentication

| Variable | Default | Effect |
|---|---|---|
| `AUTH_METHODS` | derived | Comma list of `basic`, `mtls`. Empty: derived from what is configured. |
| `AUTH_REALM` | `osb-broker` | Realm in the `WWW-Authenticate` response. |
| `BROKER_AUTH_USER` | empty | Basic-auth user. Empty means no basic auth. |
| `BROKER_AUTH_PASSWORD` | empty | The matching password. |

### mTLS

| Variable | Default | Effect |
|---|---|---|
| `MTLS_ENABLED` | `false` | Client certificates as an authentication method. |
| `MTLS_REQUIRE` | `false` | Enforces a valid client certificate at the TLS layer. |
| `MTLS_CLIENT_CA_FILE` | empty | CA that client certificates are verified against. |
| `MTLS_ALLOWED_CNS` | empty | Allowlist by common name. |
| `MTLS_ALLOWED_DNS_NAMES` | empty | Allowlist by SAN DNS names. |
| `MTLS_ALLOWED_URIS` | empty | Allowlist by SAN URIs. |

**The allowlists are authorization, not authentication.** If they are empty,
every certificate signed by the CA is accepted. Each list only checks its own
field.

**`MTLS_REQUIRE=true` only takes effect when mTLS is the sole method.**
Otherwise it is downgraded to `VerifyClientCertIfGiven` and a warning is issued —
otherwise nobody could authenticate with basic auth any more.
`RequireAnyClientCert` is never used, because it demands a certificate without
verifying it.

With `MTLS_REQUIRE=true` the Kubernetes probes have to be switched as well; the
chart then puts them on `tcpSocket`.

### TLS

| Variable | Default | Effect |
|---|---|---|
| `TLS_ENABLED` | `false` | TLS termination in the broker itself. |
| `TLS_CERT_FILE` | empty | Server certificate. |
| `TLS_KEY_FILE` | empty | The matching key. |
| `TLS_MIN_VERSION` | `1.2` | `1.2` or `1.3`. |
| `TLS_RELOAD_INTERVAL` | `30s` | Poll interval of the certificate reloader. |

Cipher suites are deliberately not pinned — Go's defaults are more current than
any frozen list. Reasoning in
[ADR 0004](../adr/0004-tls-and-mtls-no-oauth2.md).

### Server timeouts

| Variable | Default |
|---|---|
| `SERVER_READ_HEADER_TIMEOUT` | `10s` |
| `SERVER_READ_TIMEOUT` | `30s` |
| `SERVER_WRITE_TIMEOUT` | `60s` |
| `SERVER_IDLE_TIMEOUT` | `120s` |
| `SERVER_SHUTDOWN_TIMEOUT` | `15s` |

### Warnings at start-up

Five states are not fatal but are reported: in-memory store active, no
authentication configured, no TLS, empty mTLS allowlist, and a downgraded
`MTLS_REQUIRE`. Whoever operates the broker should read the first log output.

## Helm chart

The chart lives under `deploy/helm/osb-broker-go`. The most important values:

| Value | Default | Effect |
|---|---|---|
| `image.repository` | `ghcr.io/cyrano-janus/osb-broker-go` | |
| `image.tag` | empty | empty means `.Chart.AppVersion` |
| `service.port` | `443` | port of the ClusterIP service |
| `tls.enabled` | `true` | |
| `tls.certManager.enabled` | `true` | certificate through cert-manager |
| `tls.certManager.issuerRef.name` | empty | **mandatory** |
| `tls.minVersion` | `1.2` | |
| `tls.reloadInterval` | `30s` | |
| `auth.create` | `true` | creates the secret `<fullname>-auth` |
| `auth.username` / `auth.password` | `broker-user` / `change-me` | to be overridden in operation |
| `auth.mtls.enabled` | `false` | |
| `config.storeBackend` | `crd` | |
| `definitions.create` | `true` | creates `<fullname>-definitions` from `definitions.files` |
| `rbac.operatorCRDs` | six groups | see the warning below |
| `rbac.secretsAcrossNamespaces` | `true` | reading secrets across namespaces, for bind |
| `rbac.projectedBindingSecrets` | `false` | required for `spec.bind.projectSecret` |
| `metrics.enabled` | `true` | |
| `podSecurityContext` | nonroot 65532 with `fsGroup` | the `fsGroup` is needed so the process can read the certificate files mounted with mode `0440` |

### Three traps in the chart

**With its defaults the chart does not render.** `tls.certManager.enabled: true`
meets an empty `issuerRef.name` and therefore a `{{ required }}`. That is
intentional — a certificate without an issuer would be pointless — but it means
`helm install` without your own values fails.

**`rbac.operatorCRDs` covers exactly the shipped definitions** — no fewer and
no more. `internal/chart/sync_test.go` checks both directions: a missing group
would be a `403` on provision, so a failure that only surfaces at the user; a
superfluous one is a cluster-wide right too many. The list stays hand-maintained
because the resource name cannot be derived safely from the kind — the plural of
`Redis` is `redis`.

**`config.logRequests` switches the access log** via `LOG_REQUESTS`. The default
is on: a broker running silently cannot be traced when something goes wrong.

### The state CRDs are not in the chart

```bash
kubectl apply -f deploy/crds/
kubectl wait --for condition=established --timeout=60s \
  crd/osbserviceinstances.broker.osb.io crd/osbservicebindings.broker.osb.io
```

That is deliberate: cluster-wide objects in a namespaced release collide between
releases, and Helm never updates the `crds/` directory on `helm upgrade`.
Reasoning in [ADR 0001](../adr/0001-kubernetes-as-state-store.md).

### Shipped value files

| File | Purpose |
|---|---|
| `values.yaml` | defaults, see the trap above |
| `values-kind.yaml` | local development; carries the definitions embedded |
| `values-ci.yaml` | the TLS/mTLS gate in CI |

**`values-kind.yaml` has drifted away from `definitions/`.** The RabbitMQ
definition embedded there is missing `provisionedService`, `mapping` and `type`,
and three keys occur twice. Nothing tests this file. See [known-issues.md](../known-issues.md).

## Not configurable

Hardcoded, so that nobody goes looking:

| Value | Location |
|---|---|
| mount path `/definitions` | `deployment.yaml` |
| secret keys `username` / `password` | auth secret |
| secret key `credentials.json` | `crdstate.go` |
| credential secret name `<objectname>-credentials` | `crdstate.go` |
| projected secret suffix `-binding` | `servicebinding.go` |
| status path `.status.binding.name` | `servicebinding.go`, deliberately |
| `https://dashboard.example.com/instances/<id>` | both broker paths |
| fallback namespace `default` | `definition_instances.go` |
