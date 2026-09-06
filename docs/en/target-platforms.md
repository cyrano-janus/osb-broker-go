# Target platforms

> [Deutsch](../de/target-platforms.md) · Leading version: German

The broker is built for the following systems.

| Role | System | Meaning |
|---|---|---|
| **Target platform** | production Cloud Foundry | what the broker is built for |
| **Target platform** | Tanzu TAS | likewise |
| **Target platform** | external marketplaces with an OSB integration | likewise |
| **Development platform** | real Cloud Foundry on kind | test rig, not a target system |

**The distinction is not cosmetic, but it runs differently than one expects.**
The development platform runs `cloud_controller_ng` — the same software a target
system runs. What it says about the **protocol** therefore holds there too:
catalogue, promises, error codes, the update path.

What it **cannot** say is anything about operations. The broker happens to sit
in the same Kubernetes cluster as the platform there; on a target system it runs
separately ([ADR 0009](adr/0009-deployment-model.md)). Target namespace, trust
anchor and network path stay open.

Whoever carries evidence from the development platform to a target system has to
say which of the two halves it falls into.

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
success on the development platform is not yet a success on TAS.

| Topic | Development (CF on kind) | production CF / TAS |
|---|---|---|
| Registration | `cf create-service-broker` | the same |
| Reachability | in-cluster service DNS name | the broker must be reachable from the platform network — route, firewall, possibly its own app instance |
| Certificate trust | **not checked** — the Cloud Controller runs there with `skip_cert_verify: true` | the platform trust store; open whether an internal CA is accepted or a publicly trusted certificate is required |
| Plan visibility | `cf enable-service-access` | the same |
| Target namespace | created by hand, because the broker derives it from the space GUID | **there is none** — spaces are records in the Cloud Controller, not Kubernetes objects |
| Tenancy | one kind cluster, one user | real orgs and spaces, real separation of rights |
| Deployment shape | broker in the same cluster as the platform | platform on BOSH VMs, broker in a separate cluster |
| Load | one developer, one service at a time | many concurrent operations |

**The shape is settled:** the broker runs as a Kubernetes Deployment in the
operators' cluster, and a platform reaches it over a network address
([ADR 0009](adr/0009-deployment-model.md)).

**What stays open is the trust anchor**, and only the target system's operator
can settle it. Three equal routes: a certificate from a CA the platform already
trusts; the cluster CA into its trust store — on TAS a field in Ops Manager, on
Cloud Foundry a BOSH trusted certificate; or mTLS in both directions where the
platform issues client certificates. The broker demands none in particular.
That does not change the code, but it changes the operating instructions.

### Where the broker gets its certificate

It does not fetch one. It reads `TLS_CERT_FILE` and `TLS_KEY_FILE` from disk and
reloads them every `TLS_RELOAD_INTERVAL` — a renewal takes effect without a
restart. Who writes the files is none of its business.

In the chart cert-manager writes them, and `tls.certManager.issuerRef` points at
**any** issuer. An ACME issuer therefore works with no code change:

```yaml
tls:
  certManager:
    issuerRef: {kind: ClusterIssuer, name: acme-dns01}
    duration: ""          # ACME: the server decides the lifetime
    renewBefore: ""       # invalid as soon as it issues shorter than this
    dnsNames:
      - osb-broker.svc.example.com
```

Three things to watch:

1. **`dnsNames` is the name the platform reaches the broker by** — not the
   in-cluster service DNS name. Cloud Foundry validates the certificate against
   the URL from `cf create-service-broker`. With ACME it must also be a name the
   ACME account is authorised for.
2. **HTTP-01 fails for an internal broker.** The challenge requires the ACME
   server to reach `http://<name>/.well-known/acme-challenge/…`. Behind a
   firewall that cannot work — **DNS-01** only needs the DNS provider's API.
3. **Leave `duration` and `renewBefore` empty.** With ACME the server decides
   the lifetime; a value in the `Certificate` does not apply there, and a
   `renewBefore` longer than the actual lifetime makes the `Certificate`
   invalid.

That also settles the [trust anchor](adr/0009-deployment-model.md) via route 1:
an ACME certificate comes from a CA the platform already trusts.

## External marketplaces

The third target case is the one where the broker is not registered with Cloud
Foundry but with any platform that speaks the OSB API. For the broker this is
the same case as CF: a consumer that reads `/v2/catalog` and drives the
lifecycle.

In practice this means three things.

**First, the catalogue counts for more than with Cloud Foundry.** It is the only
thing a marketplace sees of the broker before it uses it — and what is not there
nobody uses. Every offering and every plan therefore carries a `metadata` block
with `displayName`, `longDescription` and links to documentation and support;
every plan states explicitly whether it is free and names its polling deadline.
What the broker can do and declares is in
[reference/osb-api.md](reference/osb-api.md).

**Second, fields that Cloud Foundry generously ignores may be mandatory here** —
a real `dashboard_url`, for instance, instead of the currently hardcoded
`https://dashboard.example.com/instances/<id>`.

**Third, `cmd/osb-gate` is the only tool that exercises this case at all**; here
it stands in for the platform. Two of its checks aim exactly at this:
`catalog-display` says what a marketplace could show and today cannot, and
`catalog-promises` holds every catalogue promise against the behaviour. A
promise the broker does not keep otherwise surfaces at the user.

## State of verification

**Real Cloud Foundry on kind is verified** — `cloud_controller_ng`, UAA, Diego,
gorouter. What is demonstrated there:

| Evidence | Result |
|---|---|
| OSB 2.17 lifecycle over HTTP | integration test covers catalog → provision → last_operation → bind → unbind → deprovision |
| Registration, marketplace, `cf create-service` | against real Cloud Foundry |
| Parameter update through the platform | `cf update-service -c` reaches the broker as a `PATCH`, and the operator's resource really changes |
| Unpromised plan change | the Cloud Controller refuses it itself (`ServicePlanNotUpdateable`) without asking the broker — so the per-plan promise is read |
| Full upgrade path | a plan with `maintenanceInfo` shows as `upgrade available`, `cf upgrade-service` triggers it, afterwards the instance carries the new state |
| Multi-document manifest end to end | `cnpg-pgvector` creates a Cluster **and** a `Database`; `status.extensions[vector].applied=true`, in the database `pg_extension` shows `vector 0.8.1` on PostgreSQL 18.6, and a `<->` comparison returns the neighbour |
| Generic engine end to end | `cf create-service cnpg-postgresql large` creates a real CloudNativePG cluster (3 instances, 10Gi); `psql` in the pod answers, credentials from the operator secret |
| Multi-document manifest end to end | `cnpg-pgvector` creates a Cluster **and** a `Database`; `status.extensions[vector].applied=true`, in the database `pg_extension` shows `vector 0.8.1` on PostgreSQL 18.6, and a `<->` comparison returns the neighbour |
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

**What the development platform cannot check.** It answers the protocol
questions and not a single operational one — the broker sits in the same cluster
as the platform there. `cf marketplace` also shows the Cloud Controller's
catalogue copy, not the broker's catalogue; only a direct `GET /v2/catalog` says
what the running broker really offers.

| Deviation | in development | on production CF / TAS |
|---|---|---|
| **Target namespace from the space GUID** | the namespace is created by hand | **exclusion criterion** — there are no namespaces per space, every provision fails |
| Trust anchor | unchecked, `skip_cert_verify: true` | the platform really validates the broker's certificate |
| `seaweedfs-s3`: readiness path checked against the CRD schema only | the operator is not installed | a path that misses costs one platform timeout per instance |

**The target namespace is an exclusion criterion**, and it is the only open
question of that kind. The broker derives the namespace of the operator
resources from the space GUID; on a platform that creates no namespace per
space, it never exists. That is not carelessness but an undecided question — a
fixed namespace, a namespace per org or space created by the broker itself, or a
mapping the operator maintains; all three have consequences for tenant
separation. The long form is in [known-issues.md](known-issues.md).

All remaining points are functional gaps and diligence on the definitions. The
protocol layer itself carries no exclusion criterion: it consists of one engine
and N definitions, with no second path beside it
([ADR 0003](adr/0003-replace-http-layer.md)).

## What a managed service needs and does not have here

Since [ADR 0008](adr/0008-depth-over-breadth.md) the effort goes into the depth
of the few services rather than into further catalogue entries. This table is
the list for that — not tasks with dates, but the gap as it stands. Each point
weighs more for an operator than a fourth service in the marketplace.

| | State |
|---|---|
| **Quotas** | ✅ `parameterLimits` per plan, enforced on `PUT` and `PATCH` and published as the plan's OSB schema in the catalogue |
| **Deletion protection** | ✅ `retainOnDeprovision` per plan; the instance is given up, the data stays and carries `osb.io/retained-instance` |
| **Inventory** | ✅ `osb_active_instances{service_id,plan_id}` — which offering is used how often |
| **Findability in a marketplace** | ✅ `metadata` per offering and plan, `free`, `maximum_polling_duration`, `instances_retrievable`, `bindings_retrievable` — and `catalog-promises` holds every promise against the behaviour |
| **Plan change** | open, and deliberately so: the broker can do it, promises it for no definition and refuses it with `422`. It is only safe in one direction — CloudNativePG grows storage and cannot shrink it — and a catalogue flag knows no direction |
| **Backup and restore** | open. CloudNativePG can do it (`Backup`, `ScheduledBackup`, Barman) — the broker offers it neither as a plan attribute nor as a service key |
| **Point-in-time recovery on provision** | open; an instance is always created empty |
| **Upgrades of existing instances** | ✅ a per-plan `maintenanceInfo`: the catalogue names the state, the instance carries the applied one, `cf upgrade-service` triggers it. The owner decides; the broker changes nothing unasked |
| **Behaviour under load, real multi-tenancy** | unverified — see the state of verification |

**What the broker deliberately does not measure: the health of the services.**
A broker that rebuilds Postgres metrics duplicates what CloudNativePG and the
RabbitMQ operator export themselves, and would cost one API call per instance
per scrape. It measures what only it knows — the OSB inventory; health is
measured by the operator that runs the service.

**What each of the three open points depends on** — and that is not the same
thing:

- **Backup and PITR** depend on the target-system run. That is where it is
  decided where backups go and who administers them; the broker side cannot be
  sensibly designed without that answer.
- **The plan change** does **not** depend on it. It is only safe in one
  direction, and the promise for it sits on each plan: `planUpdateable` applies
  to the plan an instance *leaves*. What is missing is not the mechanism but a
  transition proven against a running operator.
- **Load and multi-tenancy** are not a feature but a measurement — that needs a
  target system.
