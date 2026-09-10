# ServiceDefinitions

> [Deutsch](../de/service-definitions.md) · Leading version: German

A ServiceDefinition is a YAML file that exposes a Kubernetes operator through
the OSB API. It is the only extension point of the broker: a new service means a
new file, not new code.

**The machine-readable source is `schemas/service-definition.schema.json`.**
This document explains the fields, it does not enumerate them. If schema and
text disagree, the schema wins — `internal/definition/schema_sync_test.go` holds
it against the Go type, whereas nobody holds this text to anything except the
reader.

## What an operator has to provide

Not every operator can be integrated. Three things are needed, and all three
must be present together:

1. **A CRD for service instances.** Something the broker can create and that
   means "an instance" to the operator.
2. **Credentials as a Kubernetes secret.** The operator must write the
   credentials into a secret whose name the broker can determine — either via
   `.status.binding.name` on the CR or via a predictable naming scheme.
3. **A status field for readiness.** Something in `status` that flips from
   "being created" to "done".

**Where the "just YAML" promise breaks.** If one of the three is missing, the
best definition does not help. Three definitions are shipped, two of them
proven end to end:

| Definition | State |
|---|---|
| `cnpg-postgresql` | verified end to end, the reference for the simple shape |
| `rabbitmq-cluster` | verified end to end, the reference for the complete shape |
| `cnpg-pgvector` | verified end to end, the reference for multi-document manifests |

Five more sit under `definitions/unsupported/` and are not loaded. Two fail on
their licence (Redis and Redpanda forbid offering the software as a managed
service), one on an abandoned project (MinIO), and **two because their operator
creates no credential secret** (Valkey, SeaweedFS). The third criterion trips
more candidates than licensing does.

That is not a weakness of the definitions but of the operators and their
licences — and the reason the question about the three-part pattern comes
**before** writing a definition.

## Structure

```yaml
apiVersion: broker.osb.io/v1alpha1     # required, exactly this constant
kind: ServiceDefinition                # required, exactly this constant
metadata:
  name: <name>                         # required, [a-z0-9-]+, internal identifier
spec:
  offering:   # required — the catalogue entry
  provision:  # required — the objects to create
  readiness:  # required — how last_operation recognises "done"
  bind:       # required — where the credentials come from
```

All four `spec` blocks are required. `Parse` validates on load; a broken file
aborts the **start of the broker**, not just the first request. That is
deliberate: a broker that comes up with half a catalogue is worse than one that
does not come up at all.

## `spec.offering`

| Field | Required | Effect |
|---|---|---|
| `id` | yes | The OSB `service_id`. **Must stay stable forever** — Cloud Foundry stores it, and `definitionFor` looks it up. Changing it makes existing instances unfindable. |
| `name` | yes | The name in the marketplace: `cf create-service <name> …`. |
| `description` | no | Catalogue text. |
| `bindable` | no | Defaults to **true**. |
| `tags` | no | Catalogue tags. |
| `metadata` | no | **The marketplace display block.** See below. |
| `planUpdateable` | no | Defaults to **false**. Promises that a user may switch plans; a plan can override it. See below. |
| `plans` | yes | At least one, IDs unique. |

### Plans

| Field | Required | Effect |
|---|---|---|
| `id` | yes | The OSB `plan_id`, unique within the offering. |
| `name` | yes | `cf create-service <svc> <name> …`. |
| `description` | no | Catalogue text. |
| `params` | no | **The sizing knobs.** They arrive in the template as `{{ .plan.<key> }}`. |
| `allowedParameters` | no | Which of those knobs the user may set themselves. See below. |
| `parameterLimits` | no | **Quotas.** Bounds for the values they may set. See below. |
| `retainOnDeprovision` | no | Leaves the operator's resources standing on delete. See below. |
| `planUpdateable` | no | Promises that an instance may leave **this** plan, overriding the offering. Absent means the offering decides. See below. |
| `maintenanceInfo` | no | **The plan's state.** `version` (semver 2.0, required) and `description`. See below. |
| `free` | no | Defaults to **true**. Always present in the catalogue, `false` included — omit it there and OSB reads `true`, so a paid plan would advertise itself as free. |
| `metadata` | no | **The plan's display block.** See below. |

**The type of a `params` value matters.** YAML reads `1` as a number and `1Gi`
as a string. A template such as `{{ if eq .plan.replicas 1 }}` compares
type-strictly and fails if the value was written as `1.0`.

**`allowedParameters` is the allow list, and it applies on both paths.** A key
from the list overrides the `params` value of the same name in the template;
any key not in it is a `400` — on `PUT` just as on `PATCH`. A missing or empty
list means this plan takes no user parameters.

```yaml
- id: plan-small-0000-0000-000000000001
  name: small
  params:
    storageSize: 1Gi      # default
    instances: 1          # not overridable
  allowedParameters: [storageSize]
```

`cf create-service pg small db -c '{"storageSize":"5Gi"}'` renders `5Gi`,
`cf create-service pg small db` renders `1Gi`, and `-c '{"instances":3}'` is a
`400`.

**`parameterLimits` turns "may set" into a quota.** Without bounds a plan
describes its sizes but does not enforce them: the `small` plan with 1Gi could
be provisioned with `10Ti`, and the operator would see it on the bill.

```yaml
allowedParameters: [storageSize, instances, tier]
parameterLimits:
  storageSize: {max: 5Gi}            # quantity
  instances:   {min: "1", max: "3"}  # number
  tier:        {oneOf: [bronze, silber]}
```

Comparison goes through `resource.Quantity` — the same notation covers a plain
number and a Kubernetes quantity. Numbers arrive from JSON as `float64` and
from YAML as `int`; both are normalised, otherwise the same bound would bite
differently depending on where the value came from. A value that cannot be
compared is **rejected**, not waved through — otherwise `"lots"` would bypass
every bound.

`oneOf` excludes max/min; both together is a load error. Two further mistakes
also surface at load time rather than on the first provision: a bound on a key
that is not in `allowedParameters` (it could never apply and would feign
protection), and a bound that cannot be read.

**The bounds also appear in the catalogue.** OSB 2.17 provides a `schemas`
block per plan; a platform can reject with it before the broker is asked, and a
UI can build a form from it. The broker derives it from `allowedParameters` and
`parameterLimits` — two sources for the same statement would drift apart:

| Definition | in the plan schema |
|---|---|
| `allowedParameters` | `properties` plus `additionalProperties: false` |
| `oneOf` | `enum` |
| `max`/`min` as a plain number | `maximum`/`minimum` |
| `max`/`min` as a quantity | `description` — `10Gi` is not a JSON Schema number |

**`retainOnDeprovision` protects data from a keystroke.** `cf delete-service`
otherwise removes the backing resource immediately. For a development plan
that is right; for a production plan it is the irreversible deletion of a
database — and OSB carries no way to confirm a deprovision. The request has
neither a body nor a parameter with which a user could say "yes, really".
**So the plan decides.**

```yaml
- id: plan-large-0000-0000-000000000002
  name: large
  description: "HA, 3 instances. Deleting the service keeps the data."
  retainOnDeprovision: true
```

The broker still gives the instance up: the record is removed, deprovision is
`200`, a second one `410`. OSB knows no "partly deleted", and the platform must
be able to finish. Only the data stays — and the resources carry
`osb.io/retained-instance` and `osb.io/retained-at` so an operator can find
them:

```bash
kubectl get clusters.postgresql.cnpg.io -A -l osb.io/retained-instance
```

**Without a record it deletes.** If the broker no longer knows an instance's
plan it does not retain: that would be an assumption about a plan it does not
know, and every lost bookkeeping entry would silently pile up resources. And:
write the intent into the `description` — that is the text a user sees in
`cf marketplace`.

### What the catalogue says on its own

The catalogue is the only thing a marketplace sees of the broker before it uses
it. A capability that is not there stays unused: the platform rejects the
request before the broker is ever asked.

**`metadata` is the display block.** Without it a marketplace tile shows the
technical name and nothing else. OSB prescribes no keys; Cloud Foundry and
Tanzu Apps Manager read these:

```yaml
offering:
  metadata:
    displayName: "PostgreSQL"
    longDescription: >-
      A PostgreSQL cluster run by CloudNativePG.
    providerDisplayName: "CloudNativePG"
    documentationUrl: "https://cloudnative-pg.io/documentation/"
    supportUrl: "https://github.com/cloudnative-pg/cloudnative-pg/issues"
  plans:
    - id: …
      metadata:
        displayName: "Small"
        bullets:
          - "1 instance, no failover"
```

The block passes through unchanged. A broker that reshapes it would deliver a
tile the operator never wrote.

**`planUpdateable` promises the plan change — and without the promise the
broker refuses it.** Technically it could: a `PATCH` with a new `plan_id`
re-renders the manifest with the new plan's values. Whether the operator goes
along only the definition knows — CloudNativePG grows storage and cannot shrink
it. And a change alters more than sizes: it can move an instance onto a plan
with `retainOnDeprovision`, so a later deprovision leaves the data standing.

A change without the promise is therefore `422 PlanChangeNotSupported`. The
same plan is not a change, and a `PATCH` without `plan_id` is untouched —
`cf update-service -c` must not fail because the plan is immutable.

**The promise has a direction, and it belongs on the plan.** Both risks above
hang off the *large* plan: leaving it would shrink storage, and leaving it would
cost the instance its deletion guard. The same change is harmless the other way
round. A single flag on the offering cannot express that; a flag on the plan
can:

```yaml
plans:
  - id: …
    name: small
    planUpdateable: true      # leaving small: yes
  - id: …
    name: large
    retainOnDeprovision: true # leaving large: no (absent, defaults to false)
```

What counts is the plan the instance is on **today**, not the target. That is
how OSB 2.17 puts it — the platform may request the change "on a Service
Instance using the given Service Plan" — and how Cloud Foundry reads it: the
instance's plan first, the offering as the fallback. An unpromised change fails
there with `ServicePlanNotUpdateable` before the broker is asked; the broker's
`422` is for platforms that do not pre-check.

### `maintenanceInfo` — offering a new state instead of imposing it

An operator changes a plan: new base image, different default size. What happens
to the instances that already exist?

**Without `maintenanceInfo` there is no good answer.** Either they stay behind,
or something drags them along unasked — and either way their owner learns
nothing. Cloud Foundry has a protocol path for exactly this, and it reverses the
direction: the broker **offers**, the owner **decides**.

```yaml
plans:
  - id: …
    name: small
    maintenanceInfo:
      version: "1.4.0"                       # semver 2.0, required
      description: "PostgreSQL 18.6, rolling restart"
```

How it runs:

1. The catalogue names the plan's state; the instance carries the one it was
   last rendered under.
2. When they differ, `cf services` marks the instance `upgrade available`, and
   `description` tells the owner what to expect.
3. `cf upgrade-service` sends a `PATCH` with the new state. The broker
   re-renders and writes the state onto the instance.

**The state is checked at load time.** OSB requires semantic versioning 2.0 — a
version that is not one gets compared as a string, and strings have no ordering.
An unusable value therefore surfaces at start-up, not on the first catalogue
fetch.

**A state that does not match the plan is `422 MaintenanceInfoConflict`** — on
provision as on update. The case is mundane and the consequence is not: the
platform holds a copy of the catalogue, and a stale copy orders a state that no
longer exists. Without the refusal an instance would come into being whose state
nobody knows. If the platform sends no state at all, nothing is checked — it
does not *have* to know the field.

**What is stored is what was applied, not what the request claimed.** The
difference between instance and plan is the whole statement; an instance that
mirrors its plan's state back would never report an upgrade.

Absent, the field is left out of the catalogue entirely. An empty block would be
a statement — it would mean "there is a state", and the platform would compare
it.

**In the catalogue the offering promises only what every plan holds.** A
platform that does not implement the plan-level override reads the offering —
were it `true` while a plan withdraws the promise, that platform would permit
exactly the change the plan forbids. The offering's promise is therefore the AND
across the plans, and every plan carries its own resolved value.

**Two fields are fixed in the catalogue rather than in the definition.**
`instances_retrievable` and `bindings_retrievable` are statements about the
broker, not the operator: the GET endpoints are registered for every
definition. A test holds the promise against the route.

**`maximum_polling_duration` per plan comes from `readiness.timeoutSeconds`.**
The broker gives up the readiness check after that deadline. Poll longer and
the platform waits for an answer that will not come; stop earlier and it
reports a failure the broker never sees. Two numbers for the same deadline
drifted apart, so it is one.

## `spec.provision`

| Field | Required | Effect |
|---|---|---|
| `apiVersion` | yes | Default group/version for documents that do not name one. |
| `kind` | yes | Default kind, likewise. |
| `template` | yes | A Go template rendering a complete manifest — or several, separated by `\n---`. |

**`apiVersion` and `kind` are more than a default.** The readiness check, the
provisioned-service lookup and the owner reference of the projected secret
**always** look up under this kind and the name `safeName`. In a multi-document
template this must therefore describe the primary object, not just any of them.

### What is available in the template

Both spellings are available side by side:

| lowercase | Go | Content |
|---|---|---|
| `.instanceID` | `.InstanceID` | the raw OSB `instance_id` |
| `.safeName` | `.SafeName` | a DNS-safe object name derived from it |
| `.plan` | `.Plan` | the plan's `params`, overlaid with the permitted user parameters |
| `.bindingID` | `.BindingID` | empty during provision |
| `.parameters` | `.Parameters` | the user parameters alone, without the plan defaults |

The only helper function is `upper`. `missingkey=error` applies — a typo in a
field name is an error, not an empty string.

**`{{ .safeName }}` for `metadata.name`, `{{ .instanceID }}` for labels only.**
`SanitizeInstanceName` turns the instance ID into a valid DNS label name:
lowercase, everything outside `[a-z0-9-]` becomes a dash, and **always** the
prefix `osb-`. The prefix is not cosmetic — some operator webhooks reject bare
GUID-style names even when they are formally valid, CloudNativePG 1.24 for
example. Beyond 63 characters the name is truncated and a slice of the SHA-256
of the original ID is appended so the name stays unique.

**`{{ .plan.x }}` is almost always the right choice, not
`{{ .parameters.x }}`.** `.plan` holds the value that applies: the plan's
default when the user said nothing, their value otherwise. The template needs
no branch and no fallback for it.

`.parameters` is the narrower view — only what the user sent. It is the right
one where the template has to tell "not set" apart from "set to the default
value". Whoever uses it must reckon with `missingkey=error`:
`{{ .parameters.x }}` is a render error as soon as the user does not send `x`.

**Updates merge.** A `PATCH` overrides the keys it names and leaves the rest
standing — see [ADR 0007](adr/0007-user-parameters.md).

### Several objects per instance

The template may contain several YAML documents separated by `---`. Per document
the missing `apiVersion`, `kind` and `namespace` are filled in from the
definition and the target namespace respectively. **A document without
`metadata.name` is a hard error** — without a name the object could not be
deleted later.

## `spec.readiness`

| Field | Required | Effect |
|---|---|---|
| `statusJSONPath` | yes | **gjson** path over the entire CR. |
| `expectedValue` | no | Defaults to `"True"`, compared case-insensitively. |
| `timeoutSeconds` | no | Deadline for the operator, measured from the CR's `creationTimestamp`. Absent means **600**; a negative value switches the deadline off. Once it elapses, `last_operation` reports `failed`. |

**It is gjson, not JSONPath.** No leading `$`, array filters written as
`#(type=="Ready")`:

```yaml
statusJSONPath: 'status.conditions.#(type=="Ready").status'
expectedValue: "True"
```

A leading dot is stripped, and a path that cannot be found means **not ready
yet** — never *error*. That is intentional, because an operator only creates a
condition once it knows about it.

So that a typo does not end as an eternal `in progress` anyway, the evaluation
tells two cases apart and writes the reason into the `description` of
`last_operation`:

| State of the CR | `description` |
|---|---|
| no `status` | *the operator has not written a status yet* |
| `status` present, path finds nothing | *the path … finds nothing in the status* — **together with the condition names that are actually there** |
| path finds something else | *… is `"False"`, expected `"True"`* |

The state stays `in progress` in all three cases; an operator is allowed to take
its time. The difference becomes visible in `cf service <name>`.

**The path is determined on the live object**, not from the operator's
documentation — see [how-to/add-a-service.md](how-to/add-a-service.md).

### The readiness path must fit the operator's CRD

For every shipped definition there is a `status` excerpt from its operator's
CRD under `internal/definition/testdata/crds/`, and `readiness_crd_test.go`
computes the path against it. A new definition without that excerpt does not
pass the test.

**Why that was needed:** five definitions carried the same copied path on
`type=="Ready"`, and three of them could never match — provable from the
schema, without starting an operator. MinIO's `Tenant` carries no `conditions`
at all (it has `currentState`), Redpanda's `Cluster` enumerates exactly one
condition type (`ClusterConfigured`), and the opstree operator's `Redis` has a
`status` with **no** properties at all — the API server prunes everything the
operator tries to write there.

What the test does and does not do: a schema says what is **possible**, not
what the operator **does**. It rules out the provably impossible. Computed
against a real CR are only `cnpg-postgresql` and `rabbitmq-cluster`.

The excerpt is produced from the operator's CRD — from the cluster:

```bash
kubectl get crd <plural>.<group> -o json | \
  python3 -c 'import json,sys,yaml; d=json.load(sys.stdin); yaml.safe_dump({
    "group": d["spec"]["group"], "kind": d["spec"]["names"]["kind"],
    "versions": [{"name": v["name"], "status": v["schema"]["openAPIV3Schema"]["properties"].get("status")}
                 for v in d["spec"]["versions"]]}, sys.stdout, sort_keys=False)'
```

## `spec.bind`

| Field | Required | Effect |
|---|---|---|
| `credentialsFromSecret` | yes, unless `provisionedService` | Go template for the secret name; only `instanceID` and `safeName` are available. Also serves as the fallback. |
| `credentialKeys` | no | Selection of keys to pass through. Empty = all. **Ignored as soon as `mapping` is set.** |
| `provisionedService` | no | Read the secret name from `.status.binding.name` of the CR (CNCF Provisioned Service). |
| `type` | no | Well-known service type, `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`. Required with `projectSecret`. |
| `provider` | no | The implementation behind the type. |
| `mapping` | no | Shapes the result. **Replaces, does not extend.** |
| `projectSecret` | no | Additionally write the credentials as a spec-conformant secret into the target namespace. |
| `extraLabels` | no | Extra labels, **only** on the projected secret. |
| `fromStatus` | no | **Values from the status of other objects.** Requires `mapping`. See below. |

**Without `mapping` everything ends up in the binding.** Every key of the
operator's secret is passed through — configuration files included. With the
RabbitMQ operator those were `default_user.conf` and `connection_string`, which
have no business in a binding. With `mapping` the result consists **exactly** of
the named keys plus `type` and `provider`. An adapter that additionally passes
through all original keys makes the result unpredictable and defeats the purpose.

`type` and `provider` are set **after** the mapping. A mapping entry named
`type` therefore cannot silently override the value from the definition.

### `fromStatus` — when the address is not in the secret

The operator writes into its secret what is valid **inside** the cluster.
CloudNativePG puts the bare service name there, `osb-…-rw`. A Cloud Foundry
application runs isolated from that and never reaches the address — the whole
lifecycle is green and the binding is worthless anyway.

The address that counts is then in **another object's status**: the external
address of a `LoadBalancer` Service that the same template creates as a second
document.

```yaml
bind:
  credentialsFromSecret: "{{ .safeName }}-app"
  fromStatus:
    - name: externalHost
      apiVersion: v1
      kind: Service
      objectName: "{{ .safeName }}-external"   # empty = the provisioned CR
      jsonPath: 'status.loadBalancer.ingress.0.ip'
  mapping:
    - name: username
      from: username
    - name: host
      value: "{{ .fromStatus.externalHost }}"
    - name: uri
      value: "postgres://{{ .credentials.username }}:{{ .credentials.password }}@{{ .fromStatus.externalHost }}:5432/app"
```

**The broker reads the address, it does not produce it.** Creating a
`LoadBalancer` Service is the template's business, and whether it gets an
address is the operator's — the broker only answers a question somebody just
asked ([ADR 0011](adr/0011-broker-operator-boundary.md)).

**`jsonPath` applies to the whole object**, not only to `status` — the same
gjson notation as [`readiness.statusJSONPath`](#specreadiness), so nobody has to
learn two dialects. `spec.ports.0.nodePort` is just as readable as a status
field.

**`fromStatus` without `mapping` is a start-up error.** The values read would
have no way into the credentials, and a definition that looks like it does
something is worse than one that is missing.

**A missing value and a wrong path look alike** — both times gjson finds
nothing. The broker tells them apart by the deepest path that does exist:

| Situation | Message | What to do |
|---|---|---|
| Object missing | names kind, name and namespace | check the template |
| Structure there, leaf missing | "… is not assigned yet — bind again later" | **wait** |
| Even the second segment missing | names the path and what the status really holds | check the definition |

The difference is not a nicety: one means wait, the other means change the
definition. A LoadBalancer address is not assigned the moment the Service comes
into being.

**What is read needs a right.** If the kind is not in `rbac.operatorCRDs`, it is
not the provision that fails but the **bind** — and only when a customer calls
it. A guard in the chart holds the two against each other.

### Mapping entries

| Field | Effect |
|---|---|
| `name` | Key in the result, unique. |
| `from` | Key in the operator's secret. **If it is missing at bind time that is a hard error** — deliberately not a silent omission. |
| `value` | Go template over `.credentials.<key>`, for instance to compose a URI. |

Exactly one of `from` and `value` is required. `value` templates are parsed when
the definition is **loaded**, not when a binding is created — a broken template
aborts the start, not a customer request.

## Example 1: the simple shape

`definitions/cnpg-postgresql.yaml`, trimmed to the essentials:

```yaml
apiVersion: broker.osb.io/v1alpha1
kind: ServiceDefinition
metadata:
  name: cnpg-postgresql
spec:
  offering:
    id: f48a9e21-cnpg-0000-0000-000000000001   # stable forever
    name: cnpg-postgresql
    bindable: true
    tags: [postgresql, database, cnpg]
    plans:
      - id: plan-small-0000-0000-000000000001
        name: small
        params: { storageSize: 1Gi, instances: 1 }
      - id: plan-large-0000-0000-000000000002
        name: large
        params: { storageSize: 10Gi, instances: 3 }

  provision:
    apiVersion: postgresql.cnpg.io/v1
    kind: Cluster
    template: |
      apiVersion: postgresql.cnpg.io/v1
      kind: Cluster
      metadata:
        name: {{ .safeName }}                  # osb-<guid>, not the bare GUID
        labels:
          app.kubernetes.io/managed-by: osb-broker-go
          osb.io/instance-id: {{ .instanceID }}  # the raw ID is fine in a label
      spec:
        instances: {{ .plan.instances }}
        storage:
          size: {{ .plan.storageSize }}

  readiness:
    statusJSONPath: 'status.conditions.#(type=="Ready").status'
    expectedValue: "True"

  bind:
    credentialsFromSecret: "{{ .safeName }}-app"   # CNPG's naming convention
```

Effect: `cf create-service cnpg-postgresql large mydb` creates a `Cluster` named
`osb-<instance-guid>` in the space namespace, three instances, 10Gi.
`cf create-service-key` passes through **all** keys of the secret
`osb-<guid>-app` — with CNPG those are `username`, `password`, `host`, `port`,
`dbname`, `uri`, `jdbc-uri`, `pgpass` and `user`.

## Example 2: every feature

`definitions/rabbitmq-cluster.yaml` differs only in the `bind` block, and that
block shows every feature the schema knows:

```yaml
  bind:
    provisionedService: true                              # 1
    credentialsFromSecret: "{{ .safeName }}-default-user" # 2
    type: rabbitmq                                        # 3
    provider: rabbitmq-cluster-operator
    projectSecret: true                                   # 4
    mapping:                                              # 5
      - { name: username, from: username }
      - { name: password, from: password }
      - { name: host,     from: host }
      - { name: port,     from: port }
      - name: uri
        value: "amqp://{{ .credentials.username }}:{{ .credentials.password }}@{{ .credentials.host }}:{{ .credentials.port }}/"
```

1. **The operator says itself where the credentials are** — the broker reads
   `.status.binding.name` from the CR instead of reconstructing a naming scheme.
   The path is deliberately not configurable: were it configurable, it would be a
   convention again and not a standard.
2. **Fallback** for operator versions that do not yet populate the field.
3. **Service type and provider** end up in the credentials and in the secret
   type.
4. **Additionally a spec-conformant secret** in the target namespace, for
   consumers outside Cloud Foundry. Requires `rbac.projectedBindingSecrets: true`
   in the Helm chart, otherwise the write fails.
5. **The target shape**, exactly: `username`, `password`, `host`, `port`, `uri` —
   plus `type` and `provider`. Nothing else.

The projected secret is called `osb-<binding-guid>-binding`, carries the type
`servicebinding.io/rabbitmq` and is owned by the provisioned CR. If the instance
is deleted, Kubernetes cleans it up as well, even without a prior unbind.

## Fields that do nothing

So that nobody loses time over them:

| Field | What you expect | What happens |
|---|---|---|
| `metadata.annotations` | control over behaviour | the Go type only knows `name` |

### What an offering owes the developer

The catalogue is the only thing a developer sees of the service before typing
`cf create-service`. What is not there they have to look up in someone else's
repository — or provision and find out.

`cnpg-pgvector` is the example to read this off. Its display block names four
things you would otherwise only learn by looking:

| What the developer needs to know | Where it lives |
|---|---|
| **Which versions.** PostgreSQL 18, pgvector 0.8.1 | `offering.metadata.longDescription` |
| **What is already done.** `CREATE EXTENSION` has run; `vector` and `<->` are ready in `app` | same place |
| **What it is not.** Not a second system — same credentials, same client library | same place |
| **When not to take it.** No vectors needed means `cnpg-postgresql`; this offering is tied to PostgreSQL 18 | same place |
| **What the plan costs and gives.** Instances, storage, ceiling, delete behaviour | `plan.metadata.bullets` |

**The rule behind it:** an offering describes the *promise*, not the plumbing.
"Run by CloudNativePG" belongs in, because it says who handles failover and
maintenance. The name of the extension image does not — it changes, and the
developer can do nothing with it.

**And what the plan enforces it must also state.** `parameterLimits` reaches the
catalogue as a plan schema; put the ceiling in the `bullets` too and a human
sees it before running into it.

## An instance's state against its plan

The broker reads the definitions **at start-up**. A changed definition therefore
touches only new instances on its own: raising plan `small` from 1Gi to 2Gi
raises it for the next instance and leaves the running ones alone.

**The path for the running ones is [`maintenanceInfo`](#maintenanceinfo--offering-a-new-state-instead-of-imposing-it)**, not a timer inside
the broker. Whoever changes the plan raises its state; the platform marks
instances on an older state `upgrade available`, and their owner triggers the
upgrade. The broker then re-renders inside a request — with `last_operation`,
with an error somebody sees, and on a decision somebody made.

**The broker has no timer.** Nothing changes an existing instance without a
request. A loop that periodically drags the inventory along would be a
controller's work, not a broker's — what happens when nobody asks belongs on the
operator's side ([ADR 0011](adr/0011-broker-operator-boundary.md)). And it would be invisible to an instance's owner: it would
change while they neither saw nor wanted it.

### A plan change is not an upgrade

Switching to another plan is something else than a new state of the same plan.
It is only safe in
one direction — CloudNativePG grows storage and cannot shrink it — and that
direction belongs on the plan, not in a loop: a per-plan `planUpdateable` says
which plan may be left. As long as no shipped definition promises it, the broker
refuses the change with `422`.

## When a definition is rolled out

The broker reads the definitions directory **at start-up**. A changed definition
only takes effect after the pod restarts — the Helm chart deliberately carries no
checksum annotation on the pod template. On the development platform the restart
comes with `make broker-deploy`; see
[how-to/add-a-service.md](how-to/add-a-service.md).

Two tests have a say at build time: `internal/definition/catalog_test.go`
requires every file under `definitions/` to parse, and `schema_sync_test.go`
requires every Go field to appear in the JSON schema. A new field without a
schema entry fails the suite.
