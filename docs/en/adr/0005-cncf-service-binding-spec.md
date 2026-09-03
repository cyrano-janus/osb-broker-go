# ADR 0005: The CNCF Service Binding Specification as the target format

> [Deutsch](../../de/adr/0005-cncf-service-binding-spec.md) · Leading version: German

**Status:** accepted · **Affects:** `internal/definition/servicebinding.go`, `definitions/`

## Context

Until phase 6 the broker determined the name of the credential secret from a
name template in the definition: `{{ .safeName }}-app` for CloudNativePG,
`{{ .safeName }}-default-user` for RabbitMQ, and so on. That is guessed, not
known — the broker reconstructs a naming scheme that every operator invents for
itself and can change at any time.

Second, the broker passed through **all** keys of the secret. With the RabbitMQ
operator that put `default_user.conf` — a configuration file — and
`connection_string` into the binding. Neither belongs there, and the application
has to guess which keys it may use.

## Decision

**The CNCF Service Binding Specification becomes the target format.** Three
parts:

### 1. The operator says itself where the credentials are

With `provisionedService: true` the broker reads the secret name from
`.status.binding.name` of the provisioned CR — the specification's "Provisioned
Service" duck type. The operator provides the information instead of the broker
reconstructing it.

**The path is deliberately not configurable.** The comment in the code puts it
briefly: were it configurable, it would be a convention again and not a
standard. Falling back into an own convention was exactly the problem.

An empty or missing field falls back to `credentialsFromSecret` — for operator
versions that do not implement the specification yet. A value that exists but is
not a string, on the other hand, is a hard error and is not treated as "absent".

### 2. `mapping` defines the target shape, it does not extend it

If `mapping` is set, the result consists **exactly** of the named keys plus
`type` and `provider`. An adapter that additionally passes through all original
keys makes the result unpredictable and defeats the purpose — a defined target
shape.

- `from` copies a key. **If it is missing at bind time, that is a hard error**,
  not a silent omission: an application missing a field otherwise gets an
  incomprehensible downstream failure.
- `value` is a Go template over `.credentials`, for instance to compose a URI
  from several fields. These templates are parsed when the definition is
  **loaded**, not when a binding is created.

`type` and `provider` are set **after** the mapping, so that a mapping entry
named `type` cannot override the value from the definition.

### 3. Optionally a spec-conformant secret in the target namespace

With `projectSecret: true` the broker additionally writes the credentials as a
secret of type `servicebinding.io/<type>` into the instance's namespace — for
consumers that are not Cloud Foundry. The secret carries an `OwnerReference` on
the provisioned CR: if the instance is deleted, Kubernetes cleans it up too,
even without a prior unbind.

That is why `type` is mandatory with `projectSecret` — the specification
requires a type on every binding secret.

## Consequences

**Good:**

- The secret name is known instead of guessed, wherever the operator plays
  along.
- The binding has a defined shape. An application can rely on `username`,
  `password`, `host`, `port`, `uri`, `type`.
- The audience grows beyond Cloud Foundry: a projected secret is usable by any
  Kubernetes workload.
- Backwards compatible — existing definitions with `credentialsFromSecret` keep
  working unchanged.

**Price:**

- `projectSecret` requires cluster-wide write access to secrets
  (`rbac.projectedBindingSecrets: true`). That is a noticeable right and
  therefore off by default.
- Two routes to the same goal remain side by side as long as operators do not
  implement the specification across the board.
- A definition without `mapping` still behaves as before and passes everything
  through. That is intentional, but it means the improvement has to be applied
  per definition.

## Rejected alternatives

| Option | Why not |
|---|---|
| Keep maintaining an own convention | exactly the problem: it breaks with every new operator |
| CEL expressions for the mapping | more powerful than templates, but a second expression language in the schema; the templates have sufficed so far |
| Only `credentialKeys` as a filter | it filters but does not shape — a URI could not be composed with it |
