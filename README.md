# OSB Broker — a generic service broker for Kubernetes operators

[![Go](https://img.shields.io/badge/go-1.26-blue.svg)](https://golang.org)
[![OSB API](https://img.shields.io/badge/OSB%20API-2.17-green.svg)](https://github.com/openservicebrokerapi/servicebroker/blob/v2.17/spec.md)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

> [Deutsch](README.de.md) · Leading version: German

One hardened process exposes arbitrary Kubernetes operators through the Open
Service Broker API. A new service is a YAML file, not a new broker: no code per
service, no external database.

## What this is built for

| Role | System |
|---|---|
| **Target platform** | production Cloud Foundry |
| **Target platform** | Tanzu TAS |
| **Target platform** | external marketplaces with an OSB integration |
| **Development platform** | Korifi on kind — a test rig, not a target system |

This is at the top because it determines how everything below should be read:
all evidence in this document was recorded against the **development
platform**. Several known deviations from OSB 2.17 are harmless there and are
blockers on a target system. What that means in detail is in
[docs/en/target-platforms.md](docs/en/target-platforms.md).

## The approach

```
v1:  one broker per service type            →  N codebases, N deployments
v2:  one engine + N ServiceDefinitions      →  1 codebase, configuration instead of code
```

```
  Cloud Foundry / TAS / OSB client
            │  OSB 2.17 over HTTPS
            ▼
  ┌──────────────────────────────────────────┐
  │  osb-broker-go                            │
  │  ServiceDefinition (YAML) ──▶ operator CR │
  │  state in dedicated CRDs                  │
  └──────────────┬───────────────────────────┘
                 ▼
   CloudNativePG · RabbitMQ · further operators
```

An operator can be integrated if it provides three things: a CRD for instances,
credentials as a secret, and a status field for readiness.

## Quickstart

Without a cluster, for a look at the catalogue:

```bash
go build -o broker .
DEFINITIONS_DIR=./definitions \
BROKER_AUTH_USER=dev BROKER_AUTH_PASSWORD=dev \
./broker &

curl -s -u dev:dev -H 'X-Broker-API-Version: 2.17' \
  http://localhost:8080/v2/catalog | jq '.services[].name'
```

With a cluster, against real operators — the development platform in the
neighbouring repository `korifi-platform` builds everything declaratively:

```bash
cd ../korifi-platform
make up          # cluster, dependencies, Korifi, buildpacks
make services    # backing service operators
make broker      # build the image, load it into kind, roll it out via Helm
make register    # register with Korifi

cf marketplace
cf create-service cnpg-postgresql small my-db
cf create-service-key my-db k1
```

The state CRDs have to be installed before the first start:

```bash
kubectl apply -f deploy/crds/
```

In detail in
[docs/en/how-to/local-development.md](docs/en/how-to/local-development.md).

## State of verification

**All of the evidence below was recorded against Korifi v0.18.0 on kind**, that
is, against the development platform. It shows that the described flow really
ran there — not that it runs on a target platform. Against production Cloud
Foundry, TAS or an external marketplace there has been **no** run so far.

### Lifecycle (2026-08-24)

| Evidence | Result |
|---|---|
| OSB 2.17 lifecycle over HTTP | integration test covers catalog → provision → last_operation → bind → unbind → deprovision |
| Live against Korifi on kind | registration, marketplace, `cf create-service` |
| Generic engine end to end | `cf create-service cnpg-postgresql large my-real-pg` created a real CloudNativePG cluster (3 instances, 10Gi); `psql` in the pod answered, credentials from the operator secret |
| Restart persistence | instances and bindings survive kill and rescheduling |

### Service Binding Specification (2026-09-02)

Against the real RabbitMQ cluster operator, whose CRD documents the provisioned
service duck type:

| Evidence | Result |
|---|---|
| Secret name from `status.binding.name` | the operator reported `osb-<id>-default-user` and the broker used it without a name template |
| Target shape via `mapping` | the binding contains exactly `host, password, port, provider, type, uri, username` — `default_user.conf` and `connection_string` stay out |
| Spec-conformant secret | type `servicebinding.io/rabbitmq`, labels for instance and binding, `OwnerReference` on the `RabbitmqCluster` |
| Cleanup on unbind | `cf delete-service-key` removes the projected secret |

### State store (2026-09-02)

| Evidence | Result |
|---|---|
| Conformance against the CRD store | 24 of 24 in the kind cluster against real RBAC, deployed through the Helm chart |
| Visibility | `kubectl get osbi` and `osbb` show instance and binding with service, plan and ready |
| Credentials separated | not in the binding CR but in a secret with an `OwnerReference` on it |
| Restart persistence | instance, binding and credentials survive deleting the pod |
| Context complete | `platform`, `spaceGuid` and `organizationGuid` are mapped |

### TLS and mTLS (2026-09-02)

| Evidence | Result |
|---|---|
| Conformance over HTTPS with a client certificate | 24 of 24 — and 24 of 24 with the client certificate alone, without basic auth |
| Certificate rotation without a restart | TLS secret swapped in the running pod: the served serial number changes, `restartCount` stays 0 |
| Registration over `https://` | `CFServiceBroker` becomes ready with `trustInsecureServiceBrokers=false` — the platform verified the certificate |
| Full lifecycle over HTTPS | `cf create-service` through `cf delete-service` including real credentials |
| mTLS authorization | a client certificate signed by the same CA but not on the allowlist gets a 401 |
| Probes stay open | `/healthz` reachable without a client certificate |

## Documentation

| If you want to know … | read |
|---|---|
| what the broker is built for | [target-platforms.md](docs/en/target-platforms.md) |
| how the layers fit together | [architecture.md](docs/en/architecture.md) |
| how a service is described | [service-definitions.md](docs/en/service-definitions.md) |
| how to integrate a new operator | [how-to/add-a-service.md](docs/en/how-to/add-a-service.md) |
| how to develop and test locally | [how-to/local-development.md](docs/en/how-to/local-development.md) |
| why something is not working | [how-to/debugging.md](docs/en/how-to/debugging.md) |
| which endpoints exist and where they deviate | [reference/osb-api.md](docs/en/reference/osb-api.md) |
| which knobs there are | [reference/configuration.md](docs/en/reference/configuration.md) |
| which mines are lying around | [known-issues.md](docs/en/known-issues.md) |
| why something was decided this way | [docs/en/adr/](docs/en/adr/0001-kubernetes-as-state-store.md) |
| how to contribute | [CONTRIBUTING.md](CONTRIBUTING.md) |

Machine-readable and embedded in the binary:
[`docs/openapi.yaml`](docs/openapi.yaml) and
[`schemas/service-definition.schema.json`](schemas/service-definition.schema.json),
served at runtime under `/openapi.yaml` and
`/schemas/service-definition.schema.json` without authentication.

## Shipped definitions

| File | Operator | State |
|---|---|---|
| `cnpg-postgresql.yaml` | CloudNativePG | verified end to end |
| `rabbitmq-cluster.yaml` | RabbitMQ cluster operator | verified end to end |
| `redis-standalone.yaml` | Redis operator | the secret is not created by the operator |
| `seaweedfs-s3.yaml` | SeaweedFS | not measured through |
| `valkey-cluster.yaml` | Hyperspike Valkey | the operator creates no credential secret |
| `redpanda-cluster.yaml` | Redpanda | likewise |
| `minio-objectstorage.yaml` | MinIO | DEPRECATED |

Four of seven cannot bind — not because of the definitions but because the
operators do not satisfy the three-part pattern. See
[service-definitions.md](docs/en/service-definitions.md).

## License

MIT — see [LICENSE](LICENSE).
