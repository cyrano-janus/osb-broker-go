# ADR 0006: The OSB API is the only coupling to the platform

> [Deutsch](../../de/adr/0006-platform-independence.md) · Leading version: German

**Status:** accepted · **Affects:** the entire repository

## Context

The broker is built for **production Cloud Foundry**, **Tanzu TAS** and
integration with **external marketplaces that speak the service broker API**. It
is developed and tested against **Korifi on kind** — a development platform that
is explicitly not a target system.

This constellation invites a mistake: it would be more convenient in many places
to program against a peculiarity of the development platform. Korifi forgives
several deviations from OSB 2.17 — synchronous responses, missing binding
persistence — that are blockers on a target system. Measuring only against
Korifi means believing the broker is finished when it is not.

## Decision

**The OSB API 2.17 is the only coupling to the consuming platform. There is no
platform-specific code in the broker, and there is not supposed to be.**

Three rules follow from that:

1. **The broker runs as an ordinary Kubernetes Deployment.** It is usable
   without Cloud Foundry — via `curl`, via `cmd/osb-checker` or from any OSB
   platform. Every platform *consumes* the same URL; there is no second
   deployment per platform.
2. **No `if korifi` in the code.** Where a platform behaves oddly, that is
   handled in a specification-conformant way, not a platform-specific one. The
   most visible example: Cloud Foundry sends the space GUID exclusively in the
   deprecated top-level field of the request, not in the nested `context`. The
   broker evaluates **both** sources the specification permits, preferring the
   newer one — which is conformant and works everywhere, instead of being a
   Korifi special case.
3. **The yardstick for "done" is the target system.** A compromise on OSB
   conformance is measured by what production Cloud Foundry or TAS does with it —
   not by whether Korifi lets it pass.

Where adaptations to a platform are necessary, they belong **outside** the
broker: in the Helm chart's value file, in the registration, in the operating
instructions. Not in the Go code.

## Consequences

**Good:**

- Changing the development platform changes nothing about the broker. That is no
  longer theoretical: Korifi is being archived upstream (RFC 0060), and that is
  precisely why it is a tooling problem and not a product problem.
- The external marketplace is not a special case but the same case as Cloud
  Foundry — a consumer that reads `/v2/catalog` and drives the lifecycle.
- The conformance suite `cmd/osb-checker` is therefore a meaningful gate and not
  a formality: it tests exactly the one coupling that exists.

**Price:**

- Deviations from the specification are more expensive than they feel. They go
  unnoticed on the development platform and therefore have to be found by
  reading the code, not by observing behaviour. The list is in
  [reference/osb-api.md](../reference/osb-api.md).
- Convenient shortcuts are unavailable. Where Korifi ignores a field, the broker
  still has to handle it correctly.
- Work remains that only a real run against a target platform can do:
  reachability, certificate trust, behaviour under load. Only Korifi on kind has
  been verified so far, and that has to be said plainly — see
  [target-platforms.md](../target-platforms.md).

## Scope of this decision

This decision does **not** say that the broker is agnostic towards *Kubernetes*.
It is deeply coupled to Kubernetes: state lives in CRDs
([ADR 0001](0001-kubernetes-as-state-store.md)), the services are operators, the
bindings follow a Kubernetes specification
([ADR 0005](0005-cncf-service-binding-spec.md)). It is independent of the
platform that **consumes** it, not of the one it **runs on**.
