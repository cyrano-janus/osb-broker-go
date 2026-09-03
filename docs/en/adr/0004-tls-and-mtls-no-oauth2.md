# ADR 0004: TLS and mTLS in the broker, no OAuth2

> [Deutsch](../../de/adr/0004-tls-and-mtls-no-oauth2.md) · Leading version: German

**Status:** accepted · **Affects:** `internal/server`, `internal/auth`, `internal/config`, `deploy/helm`

## Context

The broker reads production database credentials out of secrets and hands them
to the platform. Until phase 4.5 it did so over **unencrypted HTTP** (`main.go`
called `router.Run(":"+port)`), and the only authentication was HTTP basic auth.
That was the most serious open weakness: anyone reading the traffic has the
credentials of every instance.

mTLS and OAuth2 had originally been parked as "optional".

## Decision

**The broker terminates TLS itself and supports mTLS as an equal alternative to
basic auth. OAuth2 is out of scope.**

### TLS in the broker, not in front of it

A sidecar or an ingress terminator would move the problem, not solve it: the
last hop to the broker would stay unencrypted, and the broker would depend on an
environment that puts something in front of it. It is supposed to be operable
without such preconditions.

Instead of `router.Run` there is a real `http.Server` with timeouts set and
graceful shutdown on SIGTERM.

### Certificate rotation by polling, not by inotify

The `CertReloader` periodically re-reads certificate, key and client CA and
compares SHA-256 digests.

**The reason is a Kubernetes peculiarity:** a secret is projected into the pod
through an atomically swapped `..data` symlink. An inotify watch on the leaf
path goes quiet after the first such swap — it then watches a file that no
longer exists. Polling is the more robust option here and, at a 30 second
interval, cheap enough.

A failed reload leaves the previous material valid and only writes a log line. A
broken new certificate must not kill a running broker.

The chart deliberately carries **no** checksum annotation on the pod template: a
new certificate should specifically **not** trigger a restart. The evidence that
this works is a `restartCount` of 0 after a rotation.

### mTLS as an equal method

The authenticator chain knows basic and mTLS side by side. The contract is
three-valued: success, no credentials presented (the chain continues), invalid
credentials (the chain remembers and continues anyway). Only at the end is a
decision made.

Three subtleties that matter in operation:

- **The error response is identical for every failure mode.** Saying which
  method failed would tell an unauthenticated caller which methods are enabled
  at all.
- **The allowlists are authorization, not authentication.** If
  `MTLS_ALLOWED_CNS` and its relatives are empty, every certificate signed by
  the CA is accepted. Each list only checks its own field.
- **`MTLS_REQUIRE=true` only takes effect when mTLS is the sole method.**
  Otherwise it is downgraded to `VerifyClientCertIfGiven` with a warning —
  otherwise nobody could authenticate with basic auth any more.
  `RequireAnyClientCert` is never used: it demands a certificate without
  verifying it, which is security theatre.

With enforced mTLS the Kubernetes probes have to be switched too; the chart then
puts them on `tcpSocket`.

### Cipher suites stay open

Not pinned. Go's defaults are more current than any list frozen in the
repository, and such a list ages unnoticed right up to the moment it becomes a
problem. `TLS_MIN_VERSION` is configurable, defaulting to 1.2.

### OAuth2 is out

The broker has to be operable without an identity provider. OAuth2 would add a
runtime dependency on a system that is not guaranteed to exist in the target
context, and basic auth over TLS plus mTLS covers what Cloud Foundry and TAS
require of a broker.

## Consequences

**Good:**

- Credentials are protected in transit without anything in front.
- Certificate rotation without a restart, demonstrated.
- `internal/auth`, `internal/server` and `internal/config` speak `net/http`
  rather than gin and therefore survive a replacement of the HTTP layer
  ([ADR 0003](0003-replace-http-layer.md)) unmodified.

**Price:**

- A certificate has to come from somewhere. The chart uses cert-manager and
  requires an issuer — which is why it does not render with its defaults.
- The platform has to trust the issuer. On the development platform that is
  solved with an own CA; what it looks like on a target platform is open — see
  [target-platforms.md](../target-platforms.md).
- The certificate files are mounted with mode `0440`. Without `fsGroup` in the
  `podSecurityContext` the nonroot process cannot read them.
- The CI job `L2b` is the only one that exercises the Helm chart at all.
