# Einen neuen Service anbinden

> [English](../../en/how-to/add-a-service.md) · Führende Fassung: deutsch

Ende zu Ende, am Beispiel des RabbitMQ-Cluster-Operators. Die Schritte sind für
jeden Operator dieselben; nur die Werte ändern sich.

Die Feldbedeutungen stehen in
[service-definitions.md](../service-definitions.md) — hier steht die
Reihenfolge, in der man vorgeht.

## Schritt 0: Taugt der Operator überhaupt?

Bevor eine Zeile YAML entsteht, drei Fragen. Alle drei müssen mit ja beantwortet
sein:

1. **Gibt es eine CRD für Instanzen?**
   ```bash
   kubectl get crd | grep rabbitmq
   # rabbitmqclusters.rabbitmq.com
   ```
2. **Legt der Operator ein Credential-Secret an?** Das lässt sich nur am
   laufenden Objekt beantworten, siehe Schritt 2.
3. **Gibt es ein Statusfeld für Readiness?** Ebenso.

**Vier der sieben mitgelieferten Definitionen scheitern an Frage 2.** Wer diesen
Schritt überspringt, schreibt eine Definition, die provisioniert und dann nicht
binden kann.

## Schritt 1: Operator installieren

Auf der Entwicklungsplattform ist ein Operator ein Verzeichnis unter `services/`
mit einer `service.env`:

```bash
# ../korifi-platform/services/rabbitmq/service.env
NAME=RabbitMQ Cluster Operator
VERSION=${RABBITMQ_OPERATOR_VERSION}
URL=https://github.com/rabbitmq/cluster-operator/releases/download/${RABBITMQ_OPERATOR_VERSION}/cluster-operator.yml
WAIT_NS=rabbitmq-system
WAIT_DEPLOY=rabbitmq-cluster-operator
```

Die Version steht in `versions.env`, nicht hier: das Verzeichnis beschreibt
*wie* installiert wird, `versions.env` *welche* Version.

```bash
cd ../korifi-platform && make services
```

## Schritt 2: Am lebenden Objekt nachsehen

Der wichtigste Schritt, und der, den man am ehesten überspringt. Lege von Hand
eine Instanz an und **sieh nach, was der Operator wirklich tut** — nicht, was
seine Dokumentation behauptet.

```bash
kubectl apply -f - <<'EOF'
apiVersion: rabbitmq.com/v1beta1
kind: RabbitmqCluster
metadata: { name: probe, namespace: default }
spec: { replicas: 1 }
EOF
```

**Welche Conditions gibt es?**

```bash
kubectl get rabbitmqcluster probe -o jsonpath='{.status.conditions[*].type}'
# AllReplicasReady NoWarnings ReconcileSuccess ClusterAvailable
```

Hier zeigt sich der häufigste Fehler beim Anbinden: die naheliegende Condition
`Ready` gibt es **nicht**. Wer sie trotzdem einträgt, bekommt keinen Fehler,
sondern eine Instanz, die ewig `in progress` meldet — ein nicht auffindbarer
gjson-Pfad heißt „noch nicht bereit", nie „falsch konfiguriert".

**Sagt der Operator, wo die Credentials liegen?**

```bash
kubectl get rabbitmqcluster probe -o jsonpath='{.status.binding.name}'
# probe-default-user
```

Ist das Feld gefüllt, taugt `provisionedService: true` — der Operator sagt es
selbst, und der Broker muss kein Namensschema raten. Ist es leer, braucht es
`credentialsFromSecret` mit einem Namenstemplate.

**Welche Schlüssel hat das Secret?**

```bash
kubectl get secret probe-default-user -o jsonpath='{.data}' | jq 'keys'
# ["default_user.conf","host","password","port","provider","type","username"]
```

`default_user.conf` ist eine Konfigurationsdatei. Sie hat in einem Binding
nichts verloren — das ist der Grund für `mapping`.

Danach aufräumen:

```bash
kubectl delete rabbitmqcluster probe
```

## Schritt 3: Die Definition schreiben

Eine neue Datei unter `definitions/`. Der Dateiname ist frei, `metadata.name`
sollte zu ihm passen.

```yaml
apiVersion: broker.osb.io/v1alpha1
kind: ServiceDefinition
metadata:
  name: rabbitmq-cluster
spec:
  offering:
    id: d9e6f7a8-rabb-0000-0000-000000000001   # einmal wuerfeln, dann nie mehr aendern
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

Drei Dinge, die hier leicht schiefgehen:

- **`{{ .safeName }}` für `metadata.name`.** Die rohe Instanz-ID gehört nur in
  Labels.
- **Die Condition aus Schritt 2**, nicht die naheliegende.
- **`offering.id` und die Plan-IDs sind für immer.** Cloud Foundry speichert
  sie; eine Änderung macht bestehende Instanzen unauffindbar.

## Schritt 4: Ohne Cluster gegenprüfen

```bash
go test ./internal/definition/ -run TestCatalog -count=1
```

`catalog_test.go` verlangt, dass jede Datei unter `definitions/` parst. Ein
Tippfehler im Schema fällt hier auf, nicht erst im Cluster.

## Schritt 5: Rechte ergänzen

Der Broker darf nur an CRDs, für die er Rechte hat. Die Liste ist bewusst
handgepflegt — Rechte auf nicht installierte CRDs verschleiern, was der Broker
wirklich anfassen darf.

```yaml
# deploy/helm/osb-broker-go/values.yaml, bzw. die Wertedatei der Plattform
rbac:
  operatorCRDs:
    - apiGroups: ["rabbitmq.com"]
      resources: ["rabbitmqclusters"]
      verbs: ["create", "get", "list", "update", "delete"]
```

Braucht die Definition `projectSecret: true`, zusätzlich:

```yaml
  projectedBindingSecrets: true
```

Vergessene Rechte äußern sich als 403 beim Provision, mit einer Meldung, die die
fehlende Ressource benennt.

## Schritt 6: Ausrollen

Auf der Entwicklungsplattform wird ausgewählt, welche Definitionen überhaupt
ausgerollt werden — nur solche, deren Operator installiert ist:

```bash
# ../korifi-platform/versions.env
BROKER_DEFINITIONS="cnpg-postgresql rabbitmq-cluster"
```

```bash
cd ../korifi-platform && make broker
```

**`make broker-deploy` startet den Pod nach dem Ausrollen neu.** Das ist Pflicht,
nicht Vorsicht: der Broker liest die Definitionen beim Start, und das Chart trägt
keine Prüfsummen-Annotation auf dem Pod-Template. Ohne Neustart serviert der
Broker weiter den alten Katalog — und der Fehler sieht aus wie ein
Registrierungsproblem.

## Schritt 7: Prüfen

Erst ohne Cloud Foundry:

```bash
make broker-catalog | jq '.services[].name'
```

Taucht der neue Service auf, stimmt die Definition und der Broker hat sie
geladen. Taucht er nicht auf, wurde der Pod nicht neu gestartet.

Dann über Cloud Foundry:

```bash
cf marketplace
cf create-service rabbitmq-cluster dev meine-queue
cf service meine-queue                    # wartet auf Readiness
kubectl get rabbitmqcluster -A            # das CR im Space-Namespace
cf create-service-key meine-queue k1
cf service-key meine-queue k1             # genau die gemappten Schluessel
```

Zum Schluss der Rückbau, der genauso wichtig ist:

```bash
cf delete-service-key meine-queue k1 -f
cf delete-service meine-queue -f
kubectl get rabbitmqcluster -A            # muss leer sein
```

**Bleibt ein CR zurück, ist die Buchführung kaputt.** Der Broker löscht beim
Deprovision, was er beim Provision vermerkt hat — ein Objekt, das das Template
anlegt, aber nicht mit einem Namen versieht, kommt in dieser Liste nicht vor.

## Schritt 8: Die Definition eintragen, wo sie hingehört

- `definitions/<name>.yaml` — die Quelle der Wahrheit.
- `versions.env` der Plattform, `BROKER_DEFINITIONS` — was ausgerollt wird.
- `values.korifi.yaml` der Plattform, `rbac.operatorCRDs` — die Rechte.
- `services/<operator>/service.env` — wie der Operator installiert wird.

**Nicht** in `values-kind.yaml` des Broker-Repos: die Datei dupliziert die
Definitionen als eingebettete YAML-Strings und ist bereits abgedriftet. Siehe
[known-issues.md](../known-issues.md).
