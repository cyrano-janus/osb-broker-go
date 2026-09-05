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

`cmd/osb-gate` tests the broker against OSB 2.17 and is the gate that
changes to the HTTP layer are measured by. **The name states the role, not the
scope:** the checks themselves know no service id and no catalogue entry of
this repository and hold for any OSB broker. What makes it the *gate* is only
where it runs — in this repository's CI, blocking, on every push.

```bash
go build -o /tmp/osb-gate ./cmd/osb-gate/
/tmp/osb-gate --url http://localhost:8080 --user dev --pass dev
```

Over HTTPS with a client certificate:

```bash
/tmp/osb-gate \
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
/tmp/osb-gate --url http://localhost:8080 --user dev --pass dev \
  --service-id f48a9e21-cnpg-0000-0000-000000000001 \
  --plan-id plan-small-0000-0000-000000000001
```

`--plan-id` may be omitted, in which case the service's first plan is used. A
`--service-id` that is not in the catalogue is a failure, not a silent fallback —
otherwise CI would happily audit something other than intended. Which service
was chosen appears as the first `PASS` line of the report.

### Checking the update path for real

`cf update-service -c '{...}'` sends a `PATCH` carrying **only parameters** and
no `plan_id` — in OSB 2.17 that field is optional there. The `update-parameters`
check covers exactly that shape: the request must not fail over the missing
`plan_id`, and whatever the broker accepts, `GET /v2/service_instances` must
report.

Without a hint it probes with an invented key. A broker with
`allowedParameters` rightly rejects that with `400` — the check then says
nothing and is skipped. Name a permitted key like this:

```bash
/tmp/osb-gate --url http://localhost:8080 --user dev --pass dev \
  --service-id f48a9e21-cnpg-0000-0000-000000000001 \
  --plan-id plan-small-0000-0000-000000000001 \
  --update-parameter storageSize=2Gi
```

**This check exists because the development platform cannot stand in for it.**
Korifi does not forward a `cf update-service -c` to the broker at all: the CLI
reports "Update of service instance complete" and no `PATCH` ever arrives.
Checked through the CLI, that path is unchecked — on a target system a breakage
then reaches a customer first. The development platform has `make conformance`
for it, which runs exactly this audit against the deployed broker.

## Two checkers, two roles

Besides the built-in one there is a standalone tool,
`github.com/cyrano-janus/osb-checker`. The roles are fixed so that the two do
not drift apart:

| Tool | What it is | Checks | Runs |
|---|---|---|---|
| `cmd/osb-gate` | this repository's gate | **this** broker on every push | L2 and L2b, blocking |
| `osb-checker` | the independent second opinion, own repository, public, MIT | **any** OSB broker | L2, blocking |

The difference is the **role**, not the capability. Both check the same
specification; `osb-gate` is tied to this build and decides whether it goes
through, `osb-checker` is a tool in its own right and usable against foreign
brokers.

**If the two disagree, the specification wins, not the tool.** A disagreement is
a finding for [known-issues.md](../known-issues.md), not a reason to adjust
either suite.

Both runs block. A counter-check that only reports gets skimmed past; and a
tool whose verdict has no consequence is indistinguishable from a broken one.

**Both prove that their checks can fire.** A gate whose checks are ineffective
is indistinguishable from a green one — it reports the same colour and thereby
says nothing. `osb-gate` carries that proof in `checks/mockbroker_test.go`: a conformant httptest broker must yield zero
failures, each of the 31 mutations — exactly one violated rule — must trigger
exactly the check responsible for it, and against a closed server **nothing**
may pass. The last promise is the most important one: a negative check that
reads a transport error as "the broker rejected it" reports an unreachable
broker as conformant.

```bash
go test ./cmd/osb-gate/checks/ -run TestMock -v
```

Currently: **31 mutations**, 37 assertions against a conformant broker.

Two of them are counter-checks rather than violations: an undeclared
retrievability and an honestly refused plan change must **not** fail. Without
them a check would be a rule the specification does not know.

Whoever adds a check adds its mutation. Otherwise the check itself is unchecked.

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

## Adding a field to a definition

The path is short, but there are five places, and a test flags four of them
if you forget:

1. **The Go type** in `internal/definition/definition.go` — `Offering` or
   `Plan`.
2. **The JSON schema** `schemas/service-definition.schema.json` — this is what
   a user validates their definition against offline, before rolling it out.
3. **The embedded copy** `internal/handlers/docs/service-definition.schema.json`
   that the broker serves under `/schemas/…`. A `cp` is enough.
4. **The catalogue** `internal/definition/engine.go` — but only if the field
   belongs on the outside. See below.
5. **These docs** — `service-definitions.md`, in both language trees.

`TestSchema_PlanDecktDenGoTyp`, `TestSchema_OfferingDecktDenGoTyp` and their
counterparts check steps 1 and 2 **in both directions**: a Go field without a
schema entry is flagged, and so is a schema key without a Go field — that would
be a promise to users the broker does not keep.
`TestDocsSync_ServiceDefinitionSchemaMatchesEmbeddedCopy` checks step 3.

**Step 4 is the one that gets missed.** `Offering` and `Plan` describe what may
appear in the YAML file; `CatalogEntry` and `CatalogPlan` describe what goes
over the wire. Those are two types, and a field that only exists in the first is
read and then disappears. `free` was exactly that: in the Go type, in the
schema, in the docs — and never in the catalogue.

No test can demand this step in general, because not every definition field
belongs on the outside: `readiness.statusJSONPath`, for instance, is nobody's
business out there. **So the question is: should a platform know this?** If yes,
a test in `internal/handlers/catalog_promises_test.go` belongs beside it,
holding the promise against the behaviour — and, where it is checkable over
HTTP, a check in `cmd/osb-gate`.

**The trade-off behind it:** a promise the broker does not keep fails at the
user, on a system nobody here reproduces. A capability it has and never declares
goes unused. Both failures are silent, and both surface only when someone holds
them against each other.

`Plan` does not sit under `definitions/` in the schema but inline in
`offering.properties.plans.items` — which is why it needs its own guard, and has
one. The same holds for `Offering`.

## The chart stays in step with the repository

`values-kind.yaml` embeds the definitions as strings. Two copies of the same
content drift apart as soon as somebody touches only one — which is why the
file is **generated**, not hand-maintained:

```bash
go test ./internal/chart/          # checks it
```

`internal/chart/sync_test.go` holds four promises: the embedded copies are
byte-identical to `definitions/`, no key appears twice (the YAML parser would
swallow that), `rbac.operatorCRDs` covers **exactly** the CRD groups the
definitions touch — a missing one is a `403` on provision, a superfluous one is
a cluster-wide right too many — and every value under `config` arrives in the
pod as an environment variable.

Plus two rendering checks: every shipped values file must render, and the
defaults must fail with a clear message. The latter is deliberate — the issuer
of the broker certificate is site-specific, and a `{{ required }}` is how Helm
says "you have to decide this". Rendering silently with an empty issuer would be
worse: the deployment would go through and the broker would never get a
certificate.

## Conventions

- **Language:** comments, commit messages and new test names in German.
  Identifiers and field names stay English.
- **Reasons belong in the file, not in the commit message.** Every non-obvious
  line carries a *why* as a comment. The existing code does this consistently —
  reading `crdstate.go` or `config.go` shows it.
- **Tests first** where possible. The ratio of production to test code is about
  one to one, and it should stay that way.
- **New fields need a schema entry**, otherwise `schema_sync_test.go` fails.
