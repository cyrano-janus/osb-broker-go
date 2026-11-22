# PostgreSQL: three major versions to choose from

> [Deutsch](../de/postgres-major-versions.md) · Leading version: German

The `cnpg-postgresql` offering carries three major versions at once. The
owner of an instance picks one and moves on when they choose to. This page
states what applies, who decides what, how the window moves and what a move
costs. The decision behind it is [ADR 0012](adr/0012-postgres-version-window.md).

## What applies

| Question | Answer |
|---|---|
| Which versions | the newest major and the two before it, today **17, 18, 19** |
| Default | the second newest, today **18** |
| How to choose | `cf create-service cnpg-postgresql small db -c '{"majorVersion":"19"}'` |
| How to move | `cf update-service db -c '{"majorVersion":"19"}'` — upwards only |
| Who maintains the minor | the operator of the platform, rolling, without the owner's involvement |
| What `cnpg-pgvector` does | stays bound to PostgreSQL 18 as long as there is no pgvector image for 19 |

The bound is a `oneOf` in the definition and therefore an `enum` in the
plan schema of the catalogue. A major outside the window is a `400`:

```
parameter "majorVersion" in plan "small" must be one of [17, 18, 19], got "16"
```

## Who decides what

The definition names only the **major**. Which image stands behind it is
said by a `ClusterImageCatalog` named `postgresql` that the broker's Helm
chart ships (`postgresImageCatalog` in `values.yaml`, where the tags live).
The operator raises a minor as a chart value; the development platform
passes its values through from `versions.env`. Whoever provides the
catalogue themselves says so with `postgresImageCatalog.existing: true` —
without either, the chart refuses to render as soon as the definition is
included.

| Matter | Decided by | Mechanism | Effect on running instances |
|---|---|---|---|
| Major of an instance | its owner | parameter `majorVersion` | in-place upgrade, about a minute of downtime |
| Minor per major | broker operator | change the chart value `postgresImageCatalog.images` | rolling restart of every cluster on that major |
| Window and default | broker operator | `oneOf` and `params` in the definition, `maintenanceInfo` rises | `upgrade available`, the owner triggers |
| Operator version | platform operator | `CNPG_VERSION` | restart of every instance pod, measured: under a minute |

The direction is the one from [ADR 0011](adr/0011-broker-operator-boundary.md):
the broker owns the protocol and passes the owner's wish to the operator as
a field. The upgrade itself is carried out by CloudNativePG.

## How the window moves

PostgreSQL ships one new major a year and maintains each for five years.
The window follows it like this:

1. **A new major N appears.** As soon as ghcr.io carries a `-system-trixie`
   image for it, the catalogue in `values.yaml` gets an entry `N`.
2. **The definition opens N and closes N-3.** `oneOf` becomes `[N-2, N-1, N]`,
   the default becomes N-1, `maintenanceInfo.version` rises by a minor. Both
   in one commit, because separately the catalogue would for a moment be
   larger or smaller than the window.
3. **Existing instances on N-3 keep running.** Nothing pulls them along. The
   catalogue entry N-3 stays as long as an instance uses it — without it the
   instance would get no more minors, and the cluster would stand at
   `Ready=False` although the database keeps answering (measured with the
   whole catalogue gone: connections work, Ready is missing). That would be
   a silent penalty. It
   is removed once the inventory (`osb_instances` by plan) counts none. A
   test keeps `oneOf` and the catalogue on the same list; whoever opens N
   removes N-3 in the same commit, or it does not build.
4. **Instances on the old default see `upgrade available`.** Whoever never
   set the major follows the new default with `cf upgrade-service` — that is
   a major upgrade with downtime, and the `description` of the state says
   so. Whoever set it keeps it; `cf upgrade-service` re-renders with their
   parameters and changes nothing.

That gives every version about two years in which it can be chosen and at
least three in which it runs. Missing the deadline means no new instance on
the old version — nothing more.

## What a major upgrade costs

Measured on 2026-11-22 on the development platform, CloudNativePG 1.30.2,
via `cf update-service`:

| Instance | Data | Instances | Jump | Downtime until the primary | until all replicas are up |
|---|---|---|---|---|---|
| small | 16 MB | 1 | 17 → 18 | 51 s | — |
| small | 16 MB | 1 | 18 → 19 | 51 s | — |
| small, 5Gi | 1.5 GB | 1 | 17 → 18 | 56 s | — |
| large | 456 MB | 3 | 17 → 18 | 50 s | 94 s |

The upgrade job itself takes about 30 seconds in every case; the rest is
shutting down and coming up. For comparison, the two operations that are not
a major upgrade, measured the same day: an existing instance still running
without the catalogue moved to its major's catalogue entry (18.4 → 18.7) via
`cf upgrade-service` with **17 seconds** of restart; an update of the
operator (1.30.0 → 1.30.2) restarted every instance pod, rolling with a
switchover at three instances, in under a minute. The row count afterwards was the one from
before in every case, planner statistics were preserved (`pg_stats`
populated), cumulative statistics (`pg_stat_user_tables`) were at zero.

What happens, in the operator's order: every pod of the cluster is stopped,
a job runs `pg_upgrade --link` on the primary's data directory, the primary
starts with the new image, and the replicas are **re-cloned** — their
volumes are no longer valid after the upgrade and are discarded.

## What is different at 20 terabytes

The question whether the data has to be migrated has two answers.

**The data itself: no.** `pg_upgrade --link` copies no table data. It creates
a second directory entry for every file and rewrites only the system
catalogues. The duration depends on the number of objects, not their size —
between 16 MB and 1.5 GB above lay five seconds. For 20 TB on one instance,
minutes are to be expected, not hours. That is not measured; the measurement
here ends at 1.5 GB.

**The replicas: yes, and that is the item.** Every replica is re-cloned from
the primary by `pg_basebackup` after the upgrade. At 456 MB that took 44
seconds for two replicas; at 20 TB it is hours, over the network, and during
that time the primary runs **without failover protection**. A plan with three
instances and 20 TB therefore needs a maintenance window the owner knows
about — and that is stated nowhere in the catalogue today.

**The application: that is the actual migration.** What a major upgrade
demands of the application is drivers, extensions and deprecated features.
That is what the window is for: the owner tests their application against a
new instance on N and moves the existing one once it passes.

**What is missing before every upgrade: a backup.** CloudNativePG recommends
one explicitly, and the broker does not offer it — see
[target-platforms.md](target-platforms.md). If the upgrade fails, the
operator sets the major back; the operator removes the job and starts the old
version, the data is untouched. That is the documented path of the operator
and not measured here.

## What is still missing

- **Cloud Foundry reports `update succeeded` before the database is back.**
  The broker answers the update synchronously as soon as the object is
  written; measured, the database was down for another 45 seconds. An
  asynchronous update with `last_operation` would be the protocol's way.
- **No maintenance window in the catalogue.** The `description` of the state
  says "about a minute"; for large clusters with replicas that is wrong.
- **`cnpg-pgvector` has no window** until there is a pgvector image for 19.
  The definition says so in the catalogue.
