# Contributing

> [Deutsch](CONTRIBUTING.de.md) · Leading version: German

## The shortest route to a green state

```bash
go vet ./...
go test ./... -count=1
go build ./...
```

These are exactly the three steps the CI gate `L1` runs, there additionally with
`-race`. **No cluster is needed for them** — everything that talks to Kubernetes
runs against controller-runtime's fake client in the tests.

How to test against real operators is in
[docs/en/how-to/local-development.md](docs/en/how-to/local-development.md).

## The guard tests

Five tests fail on changes that do not look like test changes. Knowing them
saves the guesswork:

| Test | Requires |
|---|---|
| `internal/definition/schema_sync_test.go` | every JSON tag of the definition types appears in `schemas/service-definition.schema.json` |
| `internal/definition/catalog_test.go` | every file under `definitions/` parses |
| `internal/handlers/docs_sync_test.go` | `docs/openapi.yaml` and the schema are byte-identical to the embedded copies |
| `internal/apis/v1alpha1/crd_schema_test.go` | every Go field of the state types appears in the CRD manifest |
| `internal/docs/sync_test.go` | `docs/de` and `docs/en` are structurally identical, no dead links, and no document narrates its own history |

Each of them secures a coupling that would otherwise come loose unnoticed — and
that nobody verifies while reading.

**Whoever changes `docs/openapi.yaml` or the schema has to carry the copy
along** — copy it, do not edit one of the two:

```bash
cp docs/openapi.yaml internal/handlers/docs/openapi.yaml
cp schemas/service-definition.schema.json internal/handlers/docs/service-definition.schema.json
```

## Language

- **Comments, commit messages and new test names: German.**
- **Identifiers, field names, OSB-level error messages: English.**
- The existing code is mixed. It is **not** unified retroactively — such a commit
  ruins every `git blame` and buys nothing.
- The documentation exists in both languages. **German is the leading version**:
  write there first, then bring the English one along. The structure guard
  reports when one side is missing.

## Reasons belong in the file

The most important convention of this repository. Every non-obvious line carries
a *why* as a comment — not in the commit message, where nobody finds it while
reading the code.

What that looks like is visible in `internal/broker/crdstate.go`,
`internal/config/config.go` and `internal/server/certreload.go`. An example from
the existing code:

```go
// Bewusst nicht konfigurierbar: waere er es, waere es wieder eine Konvention
// und kein Standard.
var bindingSecretPath = []string{"status", "binding", "name"}
```

A decision that reaches beyond a single line belongs in `docs/de/adr/` as an
architecture decision record — with context, decision, consequences and status.

## Tests

The ratio of production to test code is about one to one, and it should stay
that way. There is no mocking framework and no build tags; what is used is
`testify`, `httptest` and the fake client.

Two patterns from the existing code that have proven themselves:

- **Contract suites** instead of duplicated tests. Both state stores pass the
  same suite (`internal/broker/statestore_contract_test.go`).
- **Configuration through a lookup function** rather than the process
  environment (`config.LoadFrom`). Tests therefore never mutate global state.

## A new ServiceDefinition

The route is described in
[docs/en/how-to/add-a-service.md](docs/en/how-to/add-a-service.md). Two points
that go wrong particularly easily:

- **Determine the readiness path on the living object**, not from the operator's
  documentation. A gjson path that cannot be found means "not ready yet", never
  "misconfigured" — the mistake is invisible.
- **`offering.id` and the plan IDs are forever.** Cloud Foundry stores them;
  changing them makes existing instances unfindable.

## A new field in the schema

1. Add the field to the Go type in `internal/definition/definition.go`, with a
   JSON tag.
2. Update `schemas/service-definition.schema.json`, otherwise
   `schema_sync_test.go` fails.
3. Copy the embedded copy, otherwise `docs_sync_test.go` fails.
4. Describe the field in [docs/de/service-definitions.md](docs/de/service-definitions.md)
   and in the English version.
5. Add validation if there are values that make no sense. An error on load is
   better than one on a customer request.

## Commits

German, in the existing style: a terse subject line, then a paragraph saying
**why**, not what.

```
docs: Mandantentrennung über Space-Namespaces dokumentieren (#3/#16/#7)

Ein eigener Abschnitt zu Namespaces: worauf abgebildet wird, dass beide von
der Spezifikation erlaubten Quellen für die Space-GUID ausgewertet werden,
warum der Namespace am Datensatz gespeichert werden muss statt ihn aus dem
Request abzuleiten, und was das für die RBAC bedeutet.
```

Intermediate states are committed, not accumulated.

## What to read before a larger rebuild

- [docs/en/architecture.md](docs/en/architecture.md) — how a request finds its
  way through, and where the line between the layers runs.
- [docs/en/known-issues.md](docs/en/known-issues.md) — what is known to be open,
  so nothing gets discovered twice.
- [docs/en/adr/0003-replace-http-layer.md](docs/en/adr/0003-replace-http-layer.md)
  — why there is one path and no fallback.
- [docs/en/target-platforms.md](docs/en/target-platforms.md) — what "done" is
  measured against.
