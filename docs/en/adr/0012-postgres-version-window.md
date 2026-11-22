# ADR 0012: Three major versions to choose from, the minor belongs to the operator

> [Deutsch](../../de/adr/0012-postgres-version-window.md) · Leading version: German

**Status:** accepted · **Affects:** `definitions/cnpg-postgresql.yaml`, the platform's `ClusterImageCatalog`, `internal/definition/postgres_major_test.go`

## Context

A definition that names no image gets the operator's default. For
`cnpg-postgresql` that meant: the catalogue promised "PostgreSQL", the
instance got whatever CloudNativePG 1.30.0 brought along, and with the next
operator release a new instance would have got a different version — without
a single line of the catalogue changing. Yet the major version of a database
is the decision that drivers, extensions and deprecated features of the
application hang on. It belongs to the owner of the application, not to the
accident of an operator update.

At the same time PostgreSQL ships a new major every year and maintains each
for five years. An offering that knows only **one** major forces every owner
to move at the same moment — or leaves everyone on one version until it
expires.

Three things need deciding: who picks the major, who maintains the minor,
and how many majors can be chosen at once.

## Decision

**The owner picks the major, from a window of three.** It is a plan
parameter `majorVersion` with a `oneOf` over the newest major and the two
before it; the default is the second newest. Moving is an ordinary
`cf update-service -c` and leads to an in-place upgrade by the operator.

**The operator of the platform maintains the minor, through the operator's
catalogue.** The definition names only the major; the image behind it lives
in a `ClusterImageCatalog` named `postgresql` maintained by the platform. A
changed entry rolls every cluster on that major, without the owner and
without a detour through the broker.

**The window moves with maintenanceInfo.** When the definition opens a new
major, the plan's state rises; existing instances see an offer, not an
event. Whoever set the major keeps it; whoever took the default follows it
on their own command.

**A test holds this in place.** Every definition that creates a CloudNativePG
cluster must name the major explicitly — as a parameter via the catalogue or
as a pinned image with a major. One that does not, does not build.

## Consequences

- **One offering instead of three, two plans instead of six.** The version
  is a knob in the plan, not a plan per version.
- **The platform has one more duty.** Without the catalogue `postgresql`
  every provision fails at the readiness timeout. That is an installation
  prerequisite of the broker and is stated in the definition.
- **Minors arrive unasked, majors never.** That is a deliberate asymmetry: a
  security release is not the owner's decision, a major upgrade with downtime
  is.
- **The broker's operator has a yearly task:** catalogue entry, `oneOf`,
  default and state in one commit. How, is in
  [postgres-major-versions.md](../postgres-major-versions.md).
- **`cnpg-pgvector` stays outside** as long as the extension dictates the
  major and there is no image for the newest one.
- **Large clusters with replicas need a maintenance window** the catalogue
  does not name: the replicas are re-cloned after the upgrade, and that
  takes time proportional to the data. Open.

## Boundary

The upgrade is carried out by the operator, not the broker — the broker
passes a field through ([ADR 0011](0011-broker-operator-boundary.md)). What
CloudNativePG does in the process is not promised by this ADR; it is measured
and recorded in [postgres-major-versions.md](../postgres-major-versions.md).

A backup before the move is not part of this decision. The broker offers
none ([target-platforms.md](../target-platforms.md)), and that stays so until
a target system says where it belongs.
