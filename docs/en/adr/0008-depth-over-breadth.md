# ADR 0008: Depth over breadth — the catalogue grows along a demand

> [Deutsch](../../de/adr/0008-depth-over-breadth.md) · Leading version: German

**Status:** accepted · **Affects:** the offering, not the engine

## Context

The broker makes software available to third parties as a *managed service*.
Whether a service belongs in a catalogue is therefore decided by three
questions, and all three lie **outside** the broker:

1. Does the licence permit offering it as a managed service?
2. Does the operator carry an evaluable status in its CR?
3. Does the operator create the credentials secret **itself**?

Of seven shipped ServiceDefinitions, four answered one of these with no: Redis
and Redpanda on question 1 (RSALv2/SSPLv1 and BSL 1.1 forbid exactly this use
case), MinIO on an abandoned project, Valkey on question 3. Redis also fails
question 2 — its CRD carries a `status` with no properties at all.

**The catalogue therefore names three offerings, two of which are proven end to
end against a running operator.** None of these rejections has a cause in the
broker's code. A more capable engine would have changed none of them.

That yields an insight about the promise "a new service is a YAML file": it is
true and still not something the broker can deliver on its own. The file is
written quickly — whether it becomes an offerable service is decided by licence
holders and operator authors.

## Decision

**The catalogue grows only along a named demand.** A service is added when both
hold:

1. **A concrete workload asks for it.** Not "would be nice to have" but a team
   with a use case. A catalogue entry costs an operator in the cluster, RBAC, a
   definition and a readiness path, permanently; one without a customer is
   maintenance load without return.
2. **It satisfies the three criteria above.** Checked before the definition is
   written, not after.

The effort goes into the **depth of the few services** instead: what a managed
service needs and does not have today is in
[target-platforms.md](../target-platforms.md).

## Consequences

**Good:**

- What is in the catalogue can be ordered lawfully and works. An offering an
  operator is not allowed to offer is worse than none.
- The criteria are explicit and come **before** the work. Four definitions were
  written before anybody asked question 1 or 3.
- Effort goes where an operator actually feels a gap — backup, restore,
  upgrades, quotas, behaviour under load.
- The conformance check gains weight: under this goal the OSB surface is the
  product boundary, not the number of offerings.

**Price:**

- **The story becomes less impressive.** "Generic broker engine for arbitrary
  Kubernetes operators" sounds like a platform product, "three services, well
  operated" sounds like operations. Whoever ties visibility to the first story
  loses something.
- **A new demand is answered with "not yet" at first.** Against that: it
  already is, with or without this decision — the Redpanda definition could not
  be offered, the Valkey definition could not bind. The difference is that this
  says so out loud.
- A service a team urgently needs costs lead time for checking the three
  questions.

## Scope

**This does not revoke [ADR 0002](0002-declarative-service-definitions.md).**
The mechanism — one engine plus N declarative definitions — remains right and
remains unchanged. It is the fitting shape for three services too: no code per
service, no second deployment, one test path for all.

What is limited is not the engine but what operators and licences provide. This
decision changes the **goal**, not the **architecture**.

Equally untouched: [ADR 0006](0006-platform-independence.md) — the OSB API
remains the only coupling to the consuming platform, and the yardstick for
"done" remains the target system.
