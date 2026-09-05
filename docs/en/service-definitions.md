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
best definition does not help. Of the seven shipped definitions only two are
proven end to end:

| Definition | State |
|---|---|
| `cnpg-postgresql` | verified end to end, the reference for the simple shape |
| `rabbitmq-cluster` | verified end to end, the reference for the complete shape |
| `redis-standalone` | the secret is created by the operator's administrator, not by the operator |
| `minio-objectstorage` | marked DEPRECATED |
| `valkey-cluster` | the operator creates no credential secret |
| `redpanda-cluster` | likewise |
| `seaweedfs-s3` | successor to MinIO, not measured through |

Four of the seven cannot bind at all. That is not a weakness of the definitions
but of the operators — and the reason the question about the three-part pattern
comes **before** writing a definition.

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
| `plans` | yes | At least one, IDs unique. |

### Plans

| Field | Required | Effect |
|---|---|---|
| `id` | yes | The OSB `plan_id`, unique within the offering. |
| `name` | yes | `cf create-service <svc> <name> …`. |
| `description` | no | Catalogue text. |
| `params` | no | **The sizing knobs.** They arrive in the template as `{{ .plan.<key> }}`. |
| `allowedParameters` | no | Which of those knobs the user may set themselves. See below. |
| `free` | no | Read and **not used** — the catalogue hardcodes `free: true` for every plan. |

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

**Without `mapping` everything ends up in the binding.** Every key of the
operator's secret is passed through — configuration files included. With the
RabbitMQ operator those were `default_user.conf` and `connection_string`, which
have no business in a binding. With `mapping` the result consists **exactly** of
the named keys plus `type` and `provider`. An adapter that additionally passes
through all original keys makes the result unpredictable and defeats the purpose.

`type` and `provider` are set **after** the mapping. A mapping entry named
`type` therefore cannot silently override the value from the definition.

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
| `plan.free` | free or paid plan | the catalogue hardcodes `free: true` |
| `metadata.annotations` | control over behaviour | the Go type only knows `name` |

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
