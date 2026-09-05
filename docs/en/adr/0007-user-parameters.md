# ADR 0007: User parameters overlay the plan, an update merges

> [Deutsch](../../de/adr/0007-user-parameters.md) · Leading version: German

**Status:** accepted · **Affects:** `internal/definition`, `internal/handlers`, all ServiceDefinitions

## Context

A ServiceDefinition knows two sources for values rendered into the manifest: the
`params` of the chosen plan, which the operator sets, and the `parameters` from
the request, which the user sends with `cf create-service -c`. Which keys they
may send at all is listed in the plan's `allowedParameters`.

The template dot offers a separate view for each, `.plan` and `.parameters`.
That leaves two questions OSB 2.17 does not answer.

**First: what does the template see?** A strict separation — `.plan` holds plan
values only, `.parameters` user values only — sounds clean but moves the work
into every template. `missingkey=error` applies, so
`{{ .parameters.storageSize }}` is not an empty value but a render error as soon
as the user does not send the parameter. Every optional parameter would need a
branch with a fallback value, which then lives in *two* places in the
repository: once in `params`, once in the template.

**Second: what happens on `PATCH`?** An update usually carries only the keys
that changed. The broker stores the parameters per instance and returns them on
`GET /v2/service_instances`, so it has to decide whether the object sent is the
complete new configuration or an addition to it.

## Decision

**A permitted user parameter overrides the plan value of the same name under
`.plan`. `.parameters` remains alongside it as the pure user view.**

The plan therefore supplies the default, the allow list decides which of those
defaults is negotiable, and the template reads a single place:

```yaml
params:
  storageSize: 1Gi      # default
  instances: 1          # not overridable
allowedParameters: [storageSize]
```

```gotemplate
size: {{ .plan.storageSize }}   # 1Gi, or the user's value
```

**On `PATCH`, parameters are merged, not replaced.** Keys that are sent override
the stored ones, keys that are not named stay. When `plan_id` is absent, the
plan the instance was created under applies. What is validated against
`allowedParameters` is the **merged** set, not just the new keys: after a plan
change the entire configuration must be permitted in the target plan.

## Consequences

**Good:**

- The seven shipped definitions did not have to be touched for this capability.
  A value becomes overridable by adding its name to `allowedParameters`, not by
  rewriting the template.
- The default lives in exactly one place, in `params`. There is no second copy
  in the template that could drift away from it.
- A partial update loses nothing. `GET /v2/service_instances` reports the set
  the instance actually runs under.
- A repeated `PUT` with differing parameters can be answered with `409`, because
  the stored set is comparable.

**Price:**

- A parameter can only be changed via `PATCH`, not removed. Going back to the
  plan value means setting it to that value explicitly.
- `.plan` no longer means "what the plan says" but "what applies". A template
  that must tell the origin of a value apart needs `.parameters` — and has to
  deal with `missingkey=error` there.
- An operator who adds a key to `allowedParameters` gives up the plan boundary
  for that key. That is why the shipped definitions list only `storageSize` and
  not `instances` or `replicas`: negotiating a size is a different thing from
  circumventing the topology of a plan.

## Scope

The decision says nothing about whether an operator accepts a change at all.
CloudNativePG lets storage grow but not shrink; a `PATCH` to a smaller value is
applied by the broker and rejected by the operator. The reason then shows up in
`last_operation` — see [known-issues.md](../known-issues.md) on how long that is
allowed to take.
