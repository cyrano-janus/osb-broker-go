# Adding a new service

> [Deutsch](../../de/how-to/add-a-service.md) · Leading version: German

End to end, using the RabbitMQ cluster operator as the example. The steps are
the same for every operator; only the values change.

What the fields mean is in
[service-definitions.md](../service-definitions.md) — what is here is the order
in which to proceed.

## Step 0: is the operator suitable at all?

Before a single line of YAML, three questions. All three have to be answered
with yes:

1. **Is there a CRD for instances?**
   ```bash
   kubectl get crd | grep rabbitmq
   # rabbitmqclusters.rabbitmq.com
   ```
2. **Does the operator create a credential secret?** That can only be answered
   on a running object, see step 2.
3. **Is there a status field for readiness?** Likewise.

**Four of the seven shipped definitions fail question 2.** Skipping this step
means writing a definition that provisions and then cannot bind.

## Step 1: install the operator

On the development platform an operator is a directory under `services/` with a
`service.env`:

```bash
# ../korifi-platform/services/rabbitmq/service.env
NAME=RabbitMQ Cluster Operator
VERSION=${RABBITMQ_OPERATOR_VERSION}
URL=https://github.com/rabbitmq/cluster-operator/releases/download/${RABBITMQ_OPERATOR_VERSION}/cluster-operator.yml
WAIT_NS=rabbitmq-system
WAIT_DEPLOY=rabbitmq-cluster-operator
```

The version lives in `versions.env`, not here: the directory describes *how* the
operator is installed, `versions.env` *which* version.

```bash
cd ../korifi-platform && make services
```

## Step 2: look at the living object

The most important step, and the one most likely to be skipped. Create an
instance by hand and **look at what the operator really does** — not at what its
documentation claims.

```bash
kubectl apply -f - <<'EOF'
apiVersion: rabbitmq.com/v1beta1
kind: RabbitmqCluster
metadata: { name: probe, namespace: default }
spec: { replicas: 1 }
EOF
```

**Which conditions exist?**

```bash
kubectl get rabbitmqcluster probe -o jsonpath='{.status.conditions[*].type}'
# AllReplicasReady NoWarnings ReconcileSuccess ClusterAvailable
```

This is where the most common integration mistake shows itself: the obvious
condition `Ready` does **not** exist. Entering it anyway produces no error but an
instance that reports `in progress` forever — a gjson path that cannot be found
means "not ready yet", never "misconfigured".

**Does the operator say where the credentials are?**

```bash
kubectl get rabbitmqcluster probe -o jsonpath='{.status.binding.name}'
# probe-default-user
```

If the field is populated, `provisionedService: true` is the right choice — the
operator says so itself and the broker does not have to guess a naming scheme.
If it is empty, `credentialsFromSecret` with a name template is needed.

**Which keys does the secret have?**

```bash
kubectl get secret probe-default-user -o jsonpath='{.data}' | jq 'keys'
# ["default_user.conf","host","password","port","provider","type","username"]
```

`default_user.conf` is a configuration file. It has no business in a binding —
that is the reason for `mapping`.

Clean up afterwards:

```bash
kubectl delete rabbitmqcluster probe
```

## Step 3: write the definition

A new file under `definitions/`. The file name is free; `metadata.name` should
match it.

```yaml
apiVersion: broker.osb.io/v1alpha1
kind: ServiceDefinition
metadata:
  name: rabbitmq-cluster
spec:
  offering:
    id: d9e6f7a8-rabb-0000-0000-000000000001   # roll it once, then never change it
    name: rabbitmq-cluster
    description: "RabbitMQ clusters"
    bindable: true
    tags: [rabbitmq, messaging, amqp]
    plans:
      - id: plan-dev-rabbit-0000-000000000001
        name: dev
        params: { replicas: 1, storageSize: 1Gi }
      - id: plan-prod-rabbit-0000-000000000002
        name: prod
        params: { replicas: 3, storageSize: 10Gi }

  provision:
    apiVersion: rabbitmq.com/v1beta1
    kind: RabbitmqCluster
    template: |
      apiVersion: rabbitmq.com/v1beta1
      kind: RabbitmqCluster
      metadata:
        name: {{ .safeName }}
        labels:
          app.kubernetes.io/managed-by: osb-broker-go
          osb.io/instance-id: {{ .instanceID }}
      spec:
        replicas: {{ .plan.replicas }}
        persistence:
          storage: {{ .plan.storageSize }}

  readiness:
    statusJSONPath: 'status.conditions.#(type=="AllReplicasReady").status'
    expectedValue: "True"

  bind:
    provisionedService: true
    credentialsFromSecret: "{{ .safeName }}-default-user"
    type: rabbitmq
    provider: rabbitmq-cluster-operator
    mapping:
      - { name: username, from: username }
      - { name: password, from: password }
      - { name: host,     from: host }
      - { name: port,     from: port }
      - name: uri
        value: "amqp://{{ .credentials.username }}:{{ .credentials.password }}@{{ .credentials.host }}:{{ .credentials.port }}/"
```

Three things that easily go wrong here:

- **`{{ .safeName }}` for `metadata.name`.** The raw instance ID belongs in
  labels only.
- **The condition from step 2**, not the obvious one.
- **`offering.id` and the plan IDs are forever.** Cloud Foundry stores them;
  changing them makes existing instances unfindable.

## Step 4: check without a cluster

```bash
go test ./internal/definition/ -run TestCatalog -count=1
```

`catalog_test.go` requires every file under `definitions/` to parse. A typo in
the schema shows up here, not later in the cluster.

## Step 5: add the rights

The broker may only touch CRDs it has rights for. The list is deliberately
hand-maintained — rights on CRDs that are not installed obscure what the broker
is really allowed to touch.

```yaml
# deploy/helm/osb-broker-go/values.yaml, or the platform's value file
rbac:
  operatorCRDs:
    - apiGroups: ["rabbitmq.com"]
      resources: ["rabbitmqclusters"]
      verbs: ["create", "get", "list", "update", "delete"]
```

If the definition uses `projectSecret: true`, add:

```yaml
  projectedBindingSecrets: true
```

Forgotten rights show up as a 403 on provision, with a message naming the
missing resource.

## Step 6: roll it out

On the development platform you select which definitions are rolled out at all —
only those whose operator is installed:

```bash
# ../korifi-platform/versions.env
BROKER_DEFINITIONS="cnpg-postgresql rabbitmq-cluster"
```

```bash
cd ../korifi-platform && make broker
```

**`make broker-deploy` restarts the pod after rolling out.** That is mandatory,
not caution: the broker reads the definitions at start-up, and the chart carries
no checksum annotation on the pod template. Without a restart the broker keeps
serving the old catalogue — and the symptom looks like a registration problem.

## Step 7: verify

First without Cloud Foundry:

```bash
make broker-catalog | jq '.services[].name'
```

If the new service appears, the definition is right and the broker has loaded
it. If it does not appear, the pod was not restarted.

Then through Cloud Foundry:

```bash
cf marketplace
cf create-service rabbitmq-cluster dev my-queue
cf service my-queue                       # waits for readiness
kubectl get rabbitmqcluster -A            # the CR in the space namespace
cf create-service-key my-queue k1
cf service-key my-queue k1                # exactly the mapped keys
```

Finally the teardown, which matters just as much:

```bash
cf delete-service-key my-queue k1 -f
cf delete-service my-queue -f
kubectl get rabbitmqcluster -A            # must be empty
```

**If a CR is left behind, the bookkeeping is broken.** On deprovision the broker
deletes what it recorded on provision — an object that the template creates but
does not give a name to never appears in that list.

## Step 8: register the definition where it belongs

- `definitions/<name>.yaml` — the source of truth.
- The platform's `versions.env`, `BROKER_DEFINITIONS` — what gets rolled out.
- The platform's `values.korifi.yaml`, `rbac.operatorCRDs` — the rights.
- `services/<operator>/service.env` — how the operator is installed.

**Not** in the broker repository's `values-kind.yaml`: that file duplicates the
definitions as embedded YAML strings and has already drifted. See
[known-issues.md](../known-issues.md).
