# Target platforms

> [Deutsch](../de/target-platforms.md) · Leading version: German

The broker is built for the following systems.

| Role | System | Meaning |
|---|---|---|
| **Target platform** | production Cloud Foundry | what the broker is built for |
| **Target platform** | Tanzu TAS | likewise |
| **Target platform** | external marketplaces with an OSB integration | likewise |
| **Development platform** | Korifi on kind | test rig, not a target system |

**The distinction is not cosmetic.** Several deviations from OSB 2.17 are
harmless on Korifi and blockers on production Cloud Foundry or TAS. The
yardstick for "done" is therefore the target system, not the development
platform — and every piece of evidence this repository carries was taken on the
development platform.

## What is the same everywhere

The coupling is the **Open Service Broker API 2.17** and nothing else. The
broker runs as an ordinary Kubernetes Deployment; every platform *consumes* the
same URL. There is no platform-specific code in the broker, and there is not
supposed to be — the reasoning is in
[ADR 0006](adr/0006-platform-independence.md).

The same everywhere, therefore:

- the catalogue under `GET /v2/catalog` and what the marketplace makes of it,
- the lifecycle `provision → bind → unbind → deprovision`,
- registration as a broker with a URL and basic-auth credentials,
- making plans visible before users can see them,
- the `X-Broker-API-Version` negotiation.

A broker that conforms to OSB 2.17 works on all four. That is precisely why
conformance is not an end in itself here — it is the entire product.

## What differs

The differences are not in the API but around it, and they are the reason a
success on Korifi is not yet a success on TAS.

| Topic | Korifi (development) | production CF / TAS |
|---|---|---|
| Registration | `CFServiceBroker` CR or `cf create-service-broker` | `cf create-service-broker` |
| Reachability | in-cluster service DNS name | the broker must be reachable from the platform network — route, firewall, possibly its own app instance |
| Certificate trust | own CA, mounted into Korifi via `SSL_CERT_DIR` | the platform trust store; open whether an internal CA is accepted or a publicly trusted certificate is required |
| Plan visibility | every `CFServicePlan` has to be patched to `public` individually | `cf enable-service-access` |
| Managed services | requires the feature flag `experimental.managedServices.enabled=true` | standard behaviour, no switch |
| Tenancy | one kind cluster, one user | real orgs and spaces, real separation of rights |
| Load | one developer, one service at a time | many concurrent operations |

**Two points have to be settled per target system** and can only be answered
there: how certificate trust is established, and whether the broker runs as a
Kubernetes Deployment beside the platform or as a CF app on it. Neither changes
the code, but both change the operating instructions.

## External marketplaces

The third target case is the one where the broker is not registered with Cloud
Foundry but with any platform that speaks the OSB API. For the broker this is
the same case as CF: a consumer that reads `/v2/catalog` and drives the
lifecycle.

In practice this means two things. First, fields that Cloud Foundry generously
ignores may be mandatory here — a real `dashboard_url`, for instance, instead of
the currently hardcoded `https://dashboard.example.com/instances/<id>`. Second,
the conformance suite `cmd/osb-checker` is the only tool that exercises this
case at all; here it stands in for the platform.

## State of verification

**Korifi v0.18.0 on kind is verified.** What is demonstrated there:

| Evidence | Result |
|---|---|
| OSB 2.17 lifecycle over HTTP | integration test covers catalog → provision → last_operation → bind → unbind → deprovision |
| Registration, marketplace, `cf create-service` | against Korifi on kind |
| Generic engine end to end | `cf create-service cnpg-postgresql large` creates a real CloudNativePG cluster (3 instances, 10Gi); `psql` in the pod answers, credentials from the operator secret |
| Restart persistence | instances and bindings survive kill and rescheduling |
| Asynchronous provisioning | `202` with `operation`, `last_operation` reports `in progress` until the operator is done, then `succeeded`; without `accepts_incomplete=true` the broker answers `422 AsyncRequired` |
| Complete binding lifecycle | bind `201`, repeat `200` with the same credentials, `GET binding` `200`, unbind `200`, unbind of an unknown binding `410` |
| One path, no fallback | the catalogue consists exclusively of ServiceDefinitions; an unknown `service_id` is `400` and does not run into a second implementation |
| Status codes from error values | an unknown plan is `400` even on a `DELETE`, not `410` — the mapping no longer depends on wordings |
| Secret name from `status.binding.name` | the RabbitMQ operator reports `osb-<id>-default-user` and the broker uses it without a name template |
| Target shape via `mapping` | the binding contains exactly `host, password, port, provider, type, uri, username`; `default_user.conf` and `connection_string` stay out |
| Spec-conformant secret | type `servicebinding.io/rabbitmq`, labels for instance and binding, `OwnerReference` on the `RabbitmqCluster` |
| Cleanup on unbind | `cf delete-service-key` removes the projected secret |
| Conformance against the CRD store | 24 of 24 in the kind cluster against real RBAC, deployed through the Helm chart |
| Visibility of the state | `kubectl get osbi` and `osbb` show instance and binding with service, plan and ready |
| Credentials separated | not in the binding CR but in a secret with an `OwnerReference` on it |
| Context complete | `platform`, `spaceGuid` and `organizationGuid` are mapped |
| Conformance over HTTPS with a client certificate | 24 of 24 — and 24 of 24 with the client certificate alone, without basic auth |
| Certificate rotation without a restart | TLS secret swapped in the running pod: the served serial number changes, `restartCount` stays 0 |
| Registration over `https://` | `CFServiceBroker` becomes ready with `trustInsecureServiceBrokers=false` — the platform verifies the certificate |
| Full lifecycle over HTTPS | `cf create-service` through `cf delete-service` including real credentials |
| mTLS authorization | a client certificate signed by the same CA but not on the allowlist gets a 401 |
| Probes stay open | `/healthz` reachable without a client certificate |

Everything else is not demonstrated. There is

- no run against production Cloud Foundry,
- none against Tanzu TAS,
- none against an external marketplace,
- none under load or with real tenant separation.

That is the state of the work, not a gap in the description.

## How maturity is measured

This table is the actual work list. It shows which of the known deviations pass
unnoticed on the development platform and which do not pass on a target system.
The long form of each point is in [known-issues.md](known-issues.md), the code
locations in [reference/osb-api.md](reference/osb-api.md).

| Deviation | on Korifi | on production CF / TAS |
|---|---|---|
| Five readiness paths are unverified | the operators are not installed at all | a path that misses costs one platform timeout per instance |

None of these deviations is an exclusion criterion. The points that were sat in
the HTTP layer and were replaced along with it —
[ADR 0003](adr/0003-replace-http-layer.md). What remains are functional gaps
and diligence on the definitions.

## What happens to Korifi

Korifi is being archived upstream. The Cloud Foundry Foundation decided in
**RFC 0060** (`toc/rfc/rfc-0060-archive-cf-on-k8s-wg.md`, status `Accepted`) to
archive the `CF on K8S` working group and the Korifi repositories; CI is already
switched off.

**For this project that is a tooling problem, not a product problem.** The
target platforms are untouched, and the only coupling to Korifi is the OSB API,
which lives on. In practice:

- The existing development platform keeps running but receives no further
  upstream fixes. The artefacts needed for the current state are mirrored
  locally.
- In the medium term it has to be decided what to develop against. The RFC names
  `cloudfoundry/kind-deployment` as the successor — real Cloud Foundry on kind,
  which would be closer to the target platform than Korifi ever was.
- Changing the development platform changes nothing about the broker. That is
  the point of [ADR 0006](adr/0006-platform-independence.md).
