# OSB Broker — a generic service broker for Kubernetes operators

[![Go](https://img.shields.io/badge/go-1.26-blue.svg)](https://golang.org)
[![OSB API](https://img.shields.io/badge/OSB%20API-2.17-green.svg)](https://github.com/openservicebrokerapi/servicebroker/blob/v2.17/spec.md)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

> [Deutsch](README.de.md) · Leading version: German

One hardened process exposes Kubernetes operators through the Open Service
Broker API. A new service is a YAML file, not a new broker: no code per
service, no external database.

**Three services are offered** — PostgreSQL, RabbitMQ and SeaweedFS — two of
them proven end to end against a running operator. The catalogue grows along a
named demand, not along what is possible: a service is added when a concrete
workload asks for it **and** its operator satisfies the three-part pattern. Why
that is so is in [ADR 0008](docs/en/adr/0008-depth-over-breadth.md).

## What this is built for

| Role | System |
|---|---|
| **Target platform** | production Cloud Foundry |
| **Target platform** | Tanzu TAS |
| **Target platform** | external marketplaces with an OSB integration |
| **Development platform** | Korifi on kind — a test rig, not a target system |

The broker is verified against Korifi v0.18.0 on kind, that is, against the
development platform; against production Cloud Foundry, TAS or an external
marketplace there is no run. Several known deviations from OSB 2.17 are harmless
on the development platform and are blockers on a target system. The evidence in
detail, and what follows from it:
[docs/en/target-platforms.md](docs/en/target-platforms.md).

## The approach

One engine, N ServiceDefinitions: one codebase, configuration instead of code.

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
| `seaweedfs-s3.yaml` | SeaweedFS | readiness checked against the CRD schema only |

**Four more sit under `definitions/unsupported/` and are not loaded** — Redis
and Redpanda because their licence forbids offering the software as a managed
service, MinIO because it is AGPLv3 and unmaintained since late 2025, Valkey
because its operator creates no credentials secret. The reasoning per case is
in [definitions/unsupported/README.md](definitions/unsupported/README.md); what
makes an operator usable at all is in
[service-definitions.md](docs/en/service-definitions.md).

## License

MIT — see [LICENSE](LICENSE).
