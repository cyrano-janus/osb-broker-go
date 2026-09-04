# Developing locally

> [Deutsch](../../de/how-to/local-development.md) · Leading version: German

## The important part first

**The test suite needs no cluster.** Everything that talks to Kubernetes runs
against controller-runtime's fake client in the tests. A `git clone` and a Go
toolchain are enough to run the full suite.

```bash
git clone https://github.com/cyrano-janus/osb-broker-go
cd osb-broker-go
go test ./... -count=1
```

A cluster is only needed when you want to test against real operators.

## Prerequisites

| Tool | For |
|---|---|
| Go (see `go.mod`) | building and testing |
| Docker | building the image |
| kind, kubectl, helm | only for the run against real operators |
| `cf` CLI | only for the run through Cloud Foundry |

## The loop

```bash
go vet ./...                 # first; it catches more than you would think
go test ./... -count=1       # -count=1 bypasses the test cache
go build ./...
```

These are exactly the three steps the CI gate `L1` runs, there additionally with
`-race`. Whoever has them green locally has the first stage behind them.

Individual areas:

```bash
go test ./internal/definition/ -run TestBind -v
go test ./internal/handlers/ -count=1
go test ./internal/docs/            # the guard over this documentation
```

## The guards that have a say

These tests fail on changes that do not look like test changes. Knowing them
saves the guesswork:

| Test | Requires |
|---|---|
| `internal/definition/schema_sync_test.go` | every JSON tag of the definition types appears in `schemas/service-definition.schema.json` |
| `internal/definition/catalog_test.go` | every file under `definitions/` parses, and three named definitions are present |
| `internal/handlers/docs_sync_test.go` | `docs/openapi.yaml` and the schema are byte-identical to the embedded copies under `internal/handlers/docs/` |
| `internal/apis/v1alpha1/crd_schema_test.go` | every Go field of the state types appears in the CRD manifest |
| `internal/docs/sync_test.go` | `docs/de` and `docs/en` are structurally identical, no dead links, and no document narrates its own history |

**Whoever changes `docs/openapi.yaml` has to carry the copy along** — copy it, do
not edit one of the two:

```bash
cp docs/openapi.yaml internal/handlers/docs/openapi.yaml
cp schemas/service-definition.schema.json internal/handlers/docs/service-definition.schema.json
```

## Starting without a cluster

```bash
go build -o broker .
DEFINITIONS_DIR=./definitions \
BROKER_AUTH_USER=dev BROKER_AUTH_PASSWORD=dev \
./broker
```

State then lives in memory and is gone on the next start — enough for a look at
the catalogue:

```bash
curl -s -u dev:dev -H 'X-Broker-API-Version: 2.17' \
  http://localhost:8080/v2/catalog | jq '.services[].name'
```

Provision will fail without a cluster as soon as the engine tries to create a
CR. That is expected behaviour, not a bug.

## Against real operators

For that there is the development platform in the neighbouring repository
`korifi-platform`. It builds a kind cluster with Korifi, the operators and the
broker declaratively. It is described there; here only what matters for the
broker:

```bash
cd ../korifi-platform
make up                 # cluster, dependencies, Korifi, buildpacks
make services           # the backing service operators
make broker             # build the image, load it into kind, roll it out via Helm
make register           # register with Korifi
```

The dev loop afterwards:

```bash
make dev-broker         # go test, build image, load into kind, rollout
make broker-catalog     # query the OSB catalogue directly, without Cloud Foundry
```

**`make broker-image` runs `go test ./...` as a precondition.** An image with a
red suite never comes into existence.

Two things that surprise people:

- The image is **loaded into the kind node**, not pushed to a registry. Hence
  `pullPolicy: IfNotPresent` — otherwise Kubernetes looks on the network.
- The broker reads the definitions **at start-up**. A definition change only
  takes effect after the pod restarts; `make broker-deploy` forces it.

**Remember that Korifi is only the development platform.** What passes there is
not yet evidence for a target system — see
[target-platforms.md](../target-platforms.md).

## The conformance suite

`cmd/osb-checker` tests the broker against OSB 2.17 and is the gate that
changes to the HTTP layer are measured by.

```bash
go build -o /tmp/osb-checker ./cmd/osb-checker/
/tmp/osb-checker --url http://localhost:8080 --user dev --pass dev
```

Over HTTPS with a client certificate:

```bash
/tmp/osb-checker \
  --url https://localhost:8443 \
  --user dev --pass dev \
  --ca-cert ca.crt --client-cert client.crt --client-key client.key
```

The exit code is the number of failures. The output is annotated for GitHub
Actions.

**Which service the suite audits decides what the result is worth.** Without a
choice it takes the first service that is **not** a demo offering — that is, a
ServiceDefinition, and therefore the engine. To choose deliberately:

```bash
/tmp/osb-checker --url http://localhost:8080 --user dev --pass dev \
  --service-id f48a9e21-cnpg-0000-0000-000000000001 \
  --plan-id plan-small-0000-0000-000000000001
```

`--plan-id` may be omitted, in which case the service's first plan is used. A
`--service-id` that is not in the catalogue is a failure, not a silent fallback —
otherwise CI would happily audit something other than intended. Which service
was chosen appears as the first `PASS` line of the report.

## Two checkers, two roles

Besides the built-in one there is a standalone tool,
`github.com/cyrano-janus/osb-checker`. The roles are fixed so that the two do
not drift apart:

| Tool | Role | Runs |
|---|---|---|
| `cmd/osb-checker` | fast gate, blocking | L2 and L2b, every push |
| standalone `osb-checker` | full audit, independent counter-check | L2, blocking |

**If the two disagree, the specification wins, not the tool.** A disagreement is
a finding for [known-issues.md](../known-issues.md), not a reason to adjust
either suite.

Both runs block. A counter-check that only reports gets skimmed past; and a
tool whose verdict has no consequence is indistinguishable from a broken one.

The checker repository is public; CI clones it without credentials and without
a pinned revision — the counter-check should run whatever the current checker
is. The price is known: a commit in the checker repository can turn this
repository's CI red without anything here having changed. That is exactly when
somebody should look.

Locally:

```bash
git clone https://github.com/cyrano-janus/osb-checker
cd osb-checker && cp config.yaml configs/config.yaml
# configs/ is gitignored: it holds real credentials
go build -o osb-checker . && ./osb-checker -f configs/config.yaml
```

## Conventions

- **Language:** comments, commit messages and new test names in German.
  Identifiers and field names stay English.
- **Reasons belong in the file, not in the commit message.** Every non-obvious
  line carries a *why* as a comment. The existing code does this consistently —
  reading `crdstate.go` or `config.go` shows it.
- **Tests first** where possible. The ratio of production to test code is about
  one to one, and it should stay that way.
- **New fields need a schema entry**, otherwise `schema_sync_test.go` fails.
