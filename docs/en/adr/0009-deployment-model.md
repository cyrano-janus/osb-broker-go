# ADR 0009: The broker runs in the operators' cluster

> [Deutsch](../../de/adr/0009-deployment-model.md) · Leading version: German

**Status:** accepted · **Affects:** deployment model, certificate trust, and with it the shape of the reconcile loop

## Context

The broker runs today as a Kubernetes Deployment beside the operators it
drives, and Korifi addresses it over an HTTPS URL. For a productive Cloud
Foundry or Tanzu TAS this does not settle **where** it runs — and that question
blocks more than it first appears: a reconcile loop (C2) looks entirely
different depending on whether the broker may be a Kubernetes controller or an
application polling from the outside.

**What the broker already does**, and what is therefore *not* up for decision:

- **It terminates TLS itself.** `internal/server` drives an `http.Server` with
  `ServeTLS`; the certificate arrives through a reloader's callbacks so a
  cert-manager renewal takes effect without a restart. mTLS with an allowlist
  on CN, DNS name and URI is the same path.
- **It finds its Kubernetes access either way.** `main.go` uses
  `k8sconfig.GetConfig()`: the ServiceAccount token inside a pod, `$KUBECONFIG`
  or `~/.kube/config` outside. Running outside the cluster does not fail on the
  code.

**What is open** are three questions, and none of them is a code question:

1. **Who owns the certificate?** A platform validates the broker's certificate
   against its own trust anchor. Korifi trusts the CA this cluster issues; a
   foreign Cloud Foundry does not.
2. **Where does Kubernetes access come from?** Running outside the cluster, the
   broker needs a token that does not expire — or one that is reloaded.
3. **Who operates the thing?** A Kubernetes Deployment and a CF app have
   different operators, different log paths and different restart semantics.

## Decision

**The broker runs as a Kubernetes Deployment in the operators' cluster.** A
consuming platform — Korifi, productive Cloud Foundry, TAS or another
marketplace — reaches it over a network address and knows it solely through the
OSB API.

**This also settles that the broker may be a Kubernetes controller.** A
reconcile loop with controller-runtime, watches on the operators' CRs and
leader election is a permitted shape. That was the question actually blocked.

### Why not a CF application

The broker as an app on the consuming platform solves question 1 by itself: a
CF route carries a certificate the platform trusts anyway. But it trades a
solved problem for an unsolved one.

A broker outside the cluster needs a Kubernetes token. A literal `token:` in a
kubeconfig expires and is never reloaded — client-go only re-reads `tokenFile:`
periodically (`transport.NewCachedFileTokenSource`). Whoever wants the app
variant must answer who renews that file inside a CF container. Inside a pod
the question does not arise: the projected token rotates, and the same
mechanism reads it back.

On top of that, the broker would then have to see two networks at once — the
platform's and the Kubernetes API's. As a Deployment it sees one and is reached
from the other.

### Why not a BOSH release

The TAS-native route, and the most expensive one. It pays into none of the
three open questions and binds the broker to one platform, while
[ADR 0006](0006-platform-independence.md) deliberately frees it from all of
them.

## What follows from this

**Certificate trust is an operations task, not a code task.** Three routes, in
ascending effort:

1. **A certificate from a CA the platform already trusts** — the corporate PKI
   or a public issuer. Nothing further to do.
2. **The cluster's CA into the platform's trust store.** On TAS that is a field
   in Ops Manager, on Cloud Foundry an entry in the BOSH trusted certificates.
   No code, but an agreement.
3. **mTLS in both directions**, where the platform issues client certificates.
   The broker can already do this; whether a platform uses it is its own call.

Which route applies is the target system operator's decision. The broker
demands none in particular.

**Kubernetes access is the in-cluster ServiceAccount token.** It rotates by
itself and client-go reloads it. An RBAC scope per definition already ships in
the chart and is held against the shipped CRDs by a test.

## Consequences

**Gain:**

- The hardest question — a token that does not expire — does not arise.
- The reconcile loop may use the tools built for this job: watches instead of
  polling, leader election instead of an assumption about the replica count.
- The shape that runs and is verified here is the same one that runs on the
  target system. What changes is the consumer, not the shape — which is exactly
  the point of [ADR 0006](0006-platform-independence.md).

**Price:**

- **The broker requires a Kubernetes cluster.** That already holds: it
  provisions custom resources. What this rules out is a future in which it
  drives backends without Kubernetes.
- **An operator has to serve two platforms.** The broker belongs to the
  Kubernetes team, the registration to the Cloud Foundry team. The agreement
  about the trust anchor sits between them and has no natural owner.
- **The network path has to exist.** Cloud Foundry must be able to reach the
  cluster. On a locked-down TAS that is a clearance, not a given.

## Scope

This does **not** decide which trust anchor applies — only the target system
operator can, and the three routes above stand as equals.

This does **not** decide that the reconcile loop gets built, only that it *may*
be a controller. What it should do is in [known-issues.md](../known-issues.md).

Untouched: [ADR 0004](0004-tls-and-mtls-no-oauth2.md) — TLS and mTLS remain the
authentication toward the platform, OAuth2 stays out.
