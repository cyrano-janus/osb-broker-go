# ADR 0002: Declarative ServiceDefinitions instead of code per service

> [Deutsch](../../de/adr/0002-declarative-service-definitions.md) · Leading version: German

**Status:** accepted · **Affects:** `internal/definition`, `definitions/`, `schemas/`

## Context

The first version of the project had one broker **per service type**: a
repository for PostgreSQL, one for Redis, and so on. Each of them was 90 per
cent the same code — OSB endpoints, state handling, authentication — and 10 per
cent operator-specific.

That does not scale. With N services you get N codebases, N deployments, N
security updates and N places where an OSB subtlety can be implemented wrongly.

## Decision

**One broker engine, N YAML definitions.**

```
v1:  one broker per service type          →  N codebases, N deployments
v2:  one engine + N ServiceDefinitions    →  1 codebase, configuration instead of code
```

A service is described entirely by a ServiceDefinition: the catalogue offering
with its plans, the template for the Kubernetes objects to create, the criterion
for readiness and the origin of the credentials. No Go code per service.

The fields are described in
[service-definitions.md](../service-definitions.md); the machine-readable source
is `schemas/service-definition.schema.json`.

## The precondition that makes this possible

The approach works because Kubernetes operators follow a recurring pattern. An
operator can be integrated if it satisfies **all three** points:

1. a CRD for service instances,
2. credentials as a Kubernetes secret,
3. a status field for readiness.

That is at the same time the limit of this decision. Of the seven shipped
definitions four cannot bind, because their operator creates no credential
secret. The promise is not "any operator with just YAML" but "any operator that
follows the three-part pattern, with just YAML".

## Consequences

**Good:**

- A new service is a file, not a deployment.
- A mistake in the OSB implementation is fixed once and takes effect everywhere.
- The RabbitMQ run proved the genericity: an operator with a different CRD
  group, different condition types and a different credential layout required
  **not a single** change to `internal/definition`. That was the open objection
  against deciding on the evidence of one service, and it has been answered.

**Price:**

- **Templates instead of types.** A mistake in a template shows up at render
  time, not at compile time. Mitigated by `missingkey=error` and by the fact
  that a definition which does not parse aborts the broker's start rather than
  the first request.
- **The engine has to be more general than any single case.** Multi-document
  templates, number normalisation on comparison, no-op detection, three tiers on
  deprovision — all of that exists only because no service-specific code is
  allowed to absorb the special cases.
- **Dependence on conventions.** Where an operator puts its credential secret
  was initially guessed. The answer to that is
  [ADR 0005](0005-cncf-service-binding-spec.md).

## Rejected alternatives

| Option | Why not |
|---|---|
| One broker per service (the v1 status quo) | N codebases, see context |
| A Go plugin interface | code per service again, only with more ceremony |
| Crossplane compositions as the substrate | solves a similar problem, but would add a second large dependency and a second templating language |
