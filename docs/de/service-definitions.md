# ServiceDefinitions

> [English](../en/service-definitions.md) · Führende Fassung: deutsch

Eine ServiceDefinition ist eine YAML-Datei, die einen Kubernetes-Operator über
die OSB-API verfügbar macht. Sie ist der einzige Erweiterungspunkt des Brokers:
ein neuer Service bedeutet eine neue Datei, keinen neuen Code.

**Maschinenlesbare Quelle ist `schemas/service-definition.schema.json`.** Dieses
Dokument erklärt die Felder, es listet sie nicht ab. Wenn Schema und Text
auseinandergehen, gilt das Schema — `internal/definition/schema_sync_test.go`
hält es am Go-Typ fest, diesen Text hält niemand fest außer dem Leser.

## Was ein Operator mitbringen muss

Nicht jeder Operator lässt sich anbinden. Es braucht drei Dinge, und alle drei
müssen zusammen vorhanden sein:

1. **Eine CRD für Service-Instanzen.** Etwas, das der Broker anlegen kann und
   das für den Operator eine Instanz bedeutet.
2. **Credentials als Kubernetes-Secret.** Der Operator muss die Zugangsdaten in
   ein Secret schreiben, dessen Namen der Broker ermitteln kann — entweder über
   `.status.binding.name` am CR oder über ein vorhersagbares Namensschema.
3. **Ein Statusfeld für Readiness.** Etwas im `status`, das von „wird angelegt"
   auf „fertig" umspringt.

**Wo das Versprechen „nur YAML" bricht.** Fehlt eines der drei, hilft die beste
Definition nicht. Ausgeliefert werden drei, davon zwei durchgängig erprobt:

| Definition | Zustand |
|---|---|
| `cnpg-postgresql` | Ende zu Ende verifiziert, die Referenz für die einfache Form |
| `rabbitmq-cluster` | Ende zu Ende verifiziert, die Referenz für die vollständige Form |
| `seaweedfs-s3` | Readiness nur gegen das CRD-Schema geprüft |

Vier weitere liegen unter `definitions/unsupported/` und werden nicht geladen.
Zwei davon scheitern an der Lizenz (Redis, Redpanda untersagen die
Bereitstellung als managed Service), eines am aufgegebenen Projekt (MinIO), und
eines daran, dass sein Operator kein Credential-Secret erzeugt (Valkey).

Das ist keine Schwäche der Definitionen, sondern der Operatoren und ihrer
Lizenzen — und der Grund, warum die Frage nach dem Dreier-Muster **vor** dem
Schreiben einer Definition kommt.

## Aufbau

```yaml
apiVersion: broker.osb.io/v1alpha1     # Pflicht, exakt diese Konstante
kind: ServiceDefinition                # Pflicht, exakt diese Konstante
metadata:
  name: <name>                         # Pflicht, [a-z0-9-]+, interner Bezeichner
spec:
  offering:   # Pflicht — der Katalogeintrag
  provision:  # Pflicht — die anzulegenden Objekte
  readiness:  # Pflicht — woran last_operation „fertig" erkennt
  bind:       # Pflicht — woher die Credentials kommen
```

Alle vier `spec`-Blöcke sind Pflicht. `Parse` validiert beim Laden; eine
fehlerhafte Datei bricht den **Start des Brokers** ab, nicht erst den ersten
Request. Das ist Absicht: ein Broker, der mit halbem Katalog hochkommt, ist
schlimmer als einer, der gar nicht startet.

## `spec.offering`

| Feld | Pflicht | Wirkung |
|---|---|---|
| `id` | ja | Die OSB-`service_id`. **Muss für immer stabil bleiben** — Cloud Foundry speichert sie, und `definitionFor` schlägt darüber nach. Eine Änderung macht bestehende Instanzen unauffindbar. |
| `name` | ja | Der Name im Marketplace: `cf create-service <name> …`. |
| `description` | nein | Katalogtext. |
| `bindable` | nein | Vorgabe **true**. |
| `tags` | nein | Katalog-Tags. |
| `plans` | ja | Mindestens einer, IDs eindeutig. |

### Pläne

| Feld | Pflicht | Wirkung |
|---|---|---|
| `id` | ja | Die OSB-`plan_id`, eindeutig innerhalb des Angebots. |
| `name` | ja | `cf create-service <svc> <name> …`. |
| `description` | nein | Katalogtext. |
| `params` | nein | **Die Stellschrauben.** Landen im Template als `{{ .plan.<key> }}`. |
| `allowedParameters` | nein | Welche dieser Stellschrauben der Benutzer selbst setzen darf. Siehe unten. |
| `parameterLimits` | nein | **Kontingente.** Grenzen für die Werte, die er setzen darf. Siehe unten. |
| `retainOnDeprovision` | nein | Lässt die Ressourcen des Operators beim Löschen stehen. Siehe unten. |
| `free` | nein | Wird gelesen und **nicht verwendet** — der Katalog setzt für jeden Plan hart `free: true`. |

**Der Typ eines `params`-Wertes zählt.** YAML liest `1` als Zahl und `1Gi` als
Zeichenkette. Ein Template wie `{{ if eq .plan.replicas 1 }}` vergleicht
typsicher und schlägt fehl, wenn der Wert als `1.0` notiert wurde.

**`allowedParameters` ist die Freigabeliste, und sie gilt auf beiden Wegen.**
Ein Schlüssel aus der Liste überschreibt im Template den gleichnamigen
`params`-Wert; jeder Schlüssel, der nicht darin steht, ist `400` — beim `PUT`
genauso wie beim `PATCH`. Eine fehlende oder leere Liste heißt: dieser Plan
nimmt keine Benutzerparameter.

```yaml
- id: plan-small-0000-0000-000000000001
  name: small
  params:
    storageSize: 1Gi      # Vorgabe
    instances: 1          # nicht überschreibbar
  allowedParameters: [storageSize]
```

`cf create-service pg small db -c '{"storageSize":"5Gi"}'` rendert damit
`5Gi`, `cf create-service pg small db` rendert `1Gi`, und
`-c '{"instances":3}'` ist `400`.

**`parameterLimits` macht aus „darf setzen" ein Kontingent.** Ohne Grenzen
beschreibt ein Plan seine Größen, erzwingt sie aber nicht: der Plan `small` mit
1Gi ließe sich mit `10Ti` provisionieren, und der Betreiber sähe es an der
Rechnung.

```yaml
allowedParameters: [storageSize, instances, tier]
parameterLimits:
  storageSize: {max: 5Gi}            # Mengenangabe
  instances:   {min: "1", max: "3"}  # Zahl
  tier:        {oneOf: [bronze, silber]}
```

Verglichen wird über `resource.Quantity` — dieselbe Schreibweise deckt eine
bloße Zahl und eine Kubernetes-Mengenangabe ab. Zahlen kommen aus JSON als
`float64`, aus YAML als `int`; beide werden auf dieselbe Form gebracht, sonst
griffe die Grenze je nach Herkunft des Wertes anders. Ein Wert, der sich nicht
vergleichen lässt, wird **abgelehnt** und nicht durchgewinkt — sonst umginge
`"viel"` jede Grenze.

`oneOf` schließt max/min aus; beides zusammen ist ein Ladefehler. Zwei weitere
Fehler fallen ebenfalls beim Laden auf statt beim ersten Provision: eine Grenze
auf einem Schlüssel, der nicht in `allowedParameters` steht (sie könnte nie
greifen und täuschte Schutz vor), und eine Grenze, die sich nicht lesen lässt.

**Die Grenzen stehen auch im Katalog.** OSB 2.17 sieht je Plan einen
`schemas`-Block vor; eine Plattform kann damit ablehnen, bevor der Broker
gefragt wird, und eine Oberfläche daraus ein Formular bauen. Der Broker leitet
ihn aus `allowedParameters` und `parameterLimits` ab — zwei Quellen für
dieselbe Aussage liefen auseinander:

| Definition | im Plan-Schema |
|---|---|
| `allowedParameters` | `properties` plus `additionalProperties: false` |
| `oneOf` | `enum` |
| `max`/`min` als bloße Zahl | `maximum`/`minimum` |
| `max`/`min` als Mengenangabe | `description` — `10Gi` ist in JSON Schema keine Zahl |

**`retainOnDeprovision` schützt Daten vor einem Tastendruck.**
`cf delete-service` löscht die Backing-Ressource sonst sofort. Für einen
Entwicklungsplan ist das richtig; für einen Produktionsplan ist es die
unwiderrufliche Löschung einer Datenbank — und OSB kennt für das Deprovision
keinen Weg, eine Bestätigung zu übermitteln. Der Request trägt weder Körper
noch Parameter, mit dem ein Benutzer „ja wirklich" sagen könnte. **Deshalb
entscheidet der Plan.**

```yaml
- id: plan-large-0000-0000-000000000002
  name: large
  description: "HA, 3 Instanzen. Deleting the service keeps the data."
  retainOnDeprovision: true
```

Der Broker gibt die Instanz trotzdem auf: der Datensatz verschwindet, das
Deprovision ist `200`, ein zweites `410`. OSB kennt kein „teilweise gelöscht",
und die Plattform muss abschließen können. Nur die Daten bleiben — und die
Ressourcen tragen `osb.io/retained-instance` und `osb.io/retained-at`, damit
ein Betreiber sie findet:

```bash
kubectl get clusters.postgresql.cnpg.io -A -l osb.io/retained-instance
```

**Ohne Datensatz wird gelöscht.** Kennt der Broker den Plan einer Instanz nicht
mehr, hält er nicht zurück: das wäre eine Annahme über einen Plan, den er nicht
kennt, und jede verlorene Buchführung würde stillschweigend Ressourcen
anhäufen. Und: schreibe die Absicht in die `description` — das ist der Text,
den ein Benutzer in `cf marketplace` sieht.

## `spec.provision`

| Feld | Pflicht | Wirkung |
|---|---|---|
| `apiVersion` | ja | Vorgabe-Gruppe/Version für Dokumente, die selbst keine nennen. |
| `kind` | ja | Vorgabe-Art, dito. |
| `template` | ja | Go-Template, das ein vollständiges Manifest rendert — oder mehrere, getrennt durch `\n---`. |

**`apiVersion` und `kind` sind mehr als eine Vorgabe.** Readiness-Prüfung,
Provisioned-Service-Suche und der Eigentümerverweis des projizierten Secrets
schlagen **immer** unter dieser Art und dem Namen `safeName` nach. In einem
Multi-Doc-Template muss das also das Hauptobjekt beschreiben, nicht irgendeines.

### Was im Template zur Verfügung steht

Beide Schreibweisen stehen nebeneinander zur Verfügung:

| klein | Go | Inhalt |
|---|---|---|
| `.instanceID` | `.InstanceID` | die rohe OSB-`instance_id` |
| `.safeName` | `.SafeName` | daraus abgeleiteter, DNS-tauglicher Objektname |
| `.plan` | `.Plan` | die `params` des Plans, überlagert von den erlaubten Benutzerparametern |
| `.bindingID` | `.BindingID` | beim Provision leer |
| `.parameters` | `.Parameters` | nur die Benutzerparameter, ohne die Plan-Vorgaben |

Einzige Hilfsfunktion: `upper`. Es gilt `missingkey=error` — ein Tippfehler im
Feldnamen ist ein Fehler, keine leere Zeichenkette.

**`{{ .safeName }}` für `metadata.name`, `{{ .instanceID }}` nur für Labels.**
`SanitizeInstanceName` macht aus der Instanz-ID einen gültigen DNS-Label-Namen:
Kleinbuchstaben, alles außerhalb `[a-z0-9-]` wird zum Bindestrich, und
**immer** das Präfix `osb-`. Das Präfix ist nicht Kosmetik — manche
Operator-Webhooks lehnen nackte GUID-Namen ab, auch wenn sie formal gültig
sind, CloudNativePG 1.24 zum Beispiel. Über 63 Zeichen wird gekürzt und ein
Stück SHA-256 der Original-ID angehängt, damit der Name eindeutig bleibt.

**`{{ .plan.x }}` ist in aller Regel die richtige Wahl, nicht
`{{ .parameters.x }}`.** Unter `.plan` steht der Wert, der gilt: die Vorgabe des
Plans, sofern der Benutzer nichts gesagt hat, sonst sein Wert. Das Template
braucht dafür keine Fallunterscheidung und keinen Vorgabewert.

`.parameters` ist die engere Sicht — nur das, was der Benutzer geschickt hat.
Sie ist dort richtig, wo das Template zwischen „nicht gesetzt" und „auf den
Vorgabewert gesetzt" unterscheiden muss. Wer sie benutzt, muss mit
`missingkey=error` rechnen: `{{ .parameters.x }}` ist ein Renderfehler, sobald
der Benutzer `x` nicht schickt.

**Beim Update gilt Verschmelzen.** Ein `PATCH` überschreibt die genannten
Schlüssel und lässt die übrigen stehen — siehe
[ADR 0007](adr/0007-user-parameters.md).

### Mehrere Objekte je Instanz

Das Template darf mehrere YAML-Dokumente enthalten, getrennt durch `---`. Je
Dokument werden fehlende `apiVersion`, `kind` und `namespace` aus der Definition
beziehungsweise dem Ziel-Namespace ergänzt. **Ein Dokument ohne
`metadata.name` ist ein harter Fehler** — ohne Namen ließe sich das Objekt
später nicht wieder löschen.

## `spec.readiness`

| Feld | Pflicht | Wirkung |
|---|---|---|
| `statusJSONPath` | ja | **gjson**-Pfad über das gesamte CR. |
| `expectedValue` | nein | Vorgabe `"True"`, Vergleich ohne Rücksicht auf Groß-/Kleinschreibung. |
| `timeoutSeconds` | nein | Frist für den Operator, gemessen ab `creationTimestamp` des CR. Ohne Angabe **600**; ein negativer Wert schaltet die Frist ab. Nach Ablauf meldet `last_operation` `failed`. |

**Es ist gjson, nicht JSONPath.** Kein führendes `$`, Array-Filter in der Form
`#(type=="Ready")`:

```yaml
statusJSONPath: 'status.conditions.#(type=="Ready").status'
expectedValue: "True"
```

Ein führender Punkt wird abgeschnitten, ein nicht auffindbarer Pfad bedeutet
**noch nicht bereit** — nie *Fehler*. Das ist gewollt, weil ein Operator die
Condition erst anlegt, wenn er sie kennt.

Damit ein Tippfehler trotzdem nicht als ewiges `in progress` endet,
unterscheidet die Auswertung zwei Fälle und schreibt den Grund in die
`description` von `last_operation`:

| Lage im CR | `description` |
|---|---|
| kein `status` | *der Operator hat noch keinen Status geschrieben* |
| `status` da, Pfad findet nichts | *der Pfad … findet im Status nichts* — **mitsamt der Condition-Namen, die wirklich da sind** |
| Pfad findet etwas Anderes | *… steht auf `"False"`, erwartet `"True"`* |

Der Zustand bleibt in allen drei Fällen `in progress`; ein Operator darf sich
Zeit lassen. Sichtbar wird der Unterschied in `cf service <name>`.

**Den Pfad ermittelt man am lebenden Objekt**, nicht aus der Dokumentation des
Operators — siehe
[how-to/add-a-service.md](how-to/add-a-service.md).

### Der Readiness-Pfad muss zum CRD des Operators passen

Zu jeder mitgelieferten Definition liegt unter
`internal/definition/testdata/crds/` der `status`-Ausschnitt aus dem CRD ihres
Operators, und `readiness_crd_test.go` rechnet den Pfad dagegen. Eine neue
Definition ohne diesen Ausschnitt lässt der Test nicht durch.

**Warum das nötig war:** fünf Definitionen trugen denselben kopierten Pfad auf
`type=="Ready"`, und drei davon konnten nie zutreffen — beweisbar am Schema,
ohne einen Operator zu starten. MinIOs `Tenant` führt gar keine `conditions`
(sondern `currentState`), Redpandas `Cluster` enumeriert genau einen
Condition-Typ (`ClusterConfigured`), und der `Redis` der opstree-Operators hat
einen `status` ganz **ohne** Eigenschaften — der API-Server schneidet dort
alles weg, was der Operator hineinschreiben will.

Was der Test leistet und was nicht: ein Schema sagt, was **möglich** ist, nicht
was der Operator **tut**. Er schließt das nachweislich Unmögliche aus. Gegen
einen echten CR gerechnet sind nur `cnpg-postgresql` und `rabbitmq-cluster`.

Den Ausschnitt erzeugt man aus dem CRD des Operators — aus dem Cluster:

```bash
kubectl get crd <plural>.<gruppe> -o json | \
  python3 -c 'import json,sys,yaml; d=json.load(sys.stdin); yaml.safe_dump({
    "group": d["spec"]["group"], "kind": d["spec"]["names"]["kind"],
    "versions": [{"name": v["name"], "status": v["schema"]["openAPIV3Schema"]["properties"].get("status")}
                 for v in d["spec"]["versions"]]}, sys.stdout, sort_keys=False)'
```

## `spec.bind`

| Feld | Pflicht | Wirkung |
|---|---|---|
| `credentialsFromSecret` | ja, außer bei `provisionedService` | Go-Template für den Secret-Namen; verfügbar sind nur `instanceID` und `safeName`. Dient zusätzlich als Rückfallebene. |
| `credentialKeys` | nein | Auswahl der durchzureichenden Schlüssel. Leer = alle. **Wird ignoriert, sobald `mapping` gesetzt ist.** |
| `provisionedService` | nein | Secret-Namen aus `.status.binding.name` des CR lesen (CNCF Provisioned Service). |
| `type` | nein | Well-known-Diensttyp, `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`. Pflicht bei `projectSecret`. |
| `provider` | nein | Implementierung hinter dem Typ. |
| `mapping` | nein | Formt das Ergebnis. **Ersetzt, erweitert nicht.** |
| `projectSecret` | nein | Credentials zusätzlich als spec-konformes Secret in den Ziel-Namespace schreiben. |
| `extraLabels` | nein | Zusätzliche Labels, **nur** auf dem projizierten Secret. |

**Ohne `mapping` landet alles im Binding.** Jeder Schlüssel des
Operator-Secrets wird durchgereicht — auch Konfigurationsdateien. Beim
RabbitMQ-Operator waren das `default_user.conf` und `connection_string`, die in
einem Binding nichts verloren haben. Mit `mapping` besteht das Ergebnis **genau**
aus den genannten Schlüsseln plus `type` und `provider`. Ein Adapter, der
zusätzlich noch alle Originalschlüssel durchreicht, macht das Ergebnis
unvorhersehbar und den Zweck zunichte.

`type` und `provider` werden **nach** dem Mapping gesetzt. Ein Mapping-Eintrag
namens `type` kann den Wert aus der Definition also nicht stillschweigend
überschreiben.

### Mapping-Einträge

| Feld | Wirkung |
|---|---|
| `name` | Schlüssel im Ergebnis, eindeutig. |
| `from` | Schlüssel im Operator-Secret. **Fehlt er zur Bindezeit, ist das ein harter Fehler** — bewusst kein stilles Weglassen. |
| `value` | Go-Template über `.credentials.<key>`, etwa zum Zusammensetzen einer URI. |

Genau eines von `from` und `value` ist erforderlich. `value`-Templates werden
schon beim **Laden** der Definition übersetzt, nicht erst beim Binden — ein
kaputtes Template bricht den Start ab, nicht den Kundenrequest.

## Beispiel 1: die einfache Form

`definitions/cnpg-postgresql.yaml`, gekürzt auf das Wesentliche:

```yaml
apiVersion: broker.osb.io/v1alpha1
kind: ServiceDefinition
metadata:
  name: cnpg-postgresql
spec:
  offering:
    id: f48a9e21-cnpg-0000-0000-000000000001   # für immer stabil
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
        name: {{ .safeName }}                  # osb-<guid>, nicht die nackte GUID
        labels:
          app.kubernetes.io/managed-by: osb-broker-go
          osb.io/instance-id: {{ .instanceID }}  # im Label ist die rohe ID in Ordnung
      spec:
        instances: {{ .plan.instances }}
        storage:
          size: {{ .plan.storageSize }}

  readiness:
    statusJSONPath: 'status.conditions.#(type=="Ready").status'
    expectedValue: "True"

  bind:
    credentialsFromSecret: "{{ .safeName }}-app"   # Namenskonvention von CNPG
```

Wirkung: `cf create-service cnpg-postgresql large mydb` legt einen `Cluster`
namens `osb-<instanz-guid>` im Space-Namespace an, drei Instanzen, 10Gi.
`cf create-service-key` reicht **alle** Schlüssel des Secrets `osb-<guid>-app`
durch — bei CNPG sind das `username`, `password`, `host`, `port`, `dbname`,
`uri`, `jdbc-uri`, `pgpass` und `user`.

## Beispiel 2: alle Merkmale

`definitions/rabbitmq-cluster.yaml` unterscheidet sich nur im `bind`-Block, und
der zeigt jedes Merkmal, das das Schema kennt:

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

1. **Der Operator sagt selbst, wo die Credentials liegen** — der Broker liest
   `.status.binding.name` am CR, statt ein Namensschema nachzubauen. Der Pfad ist
   absichtlich nicht konfigurierbar: wäre er es, wäre es wieder eine Konvention
   und kein Standard.
2. **Rückfallebene** für Operator-Versionen, die das Feld noch nicht füllen.
3. **Diensttyp und Anbieter** landen in den Credentials und im Secret-Typ.
4. **Zusätzlich ein spec-konformes Secret** im Ziel-Namespace, für Konsumenten
   außerhalb von Cloud Foundry. Braucht `rbac.projectedBindingSecrets: true` im
   Helm-Chart, sonst scheitert das Schreiben.
5. **Die Zielform**, exakt: `username`, `password`, `host`, `port`, `uri` — dazu
   `type` und `provider`. Nichts sonst.

Das projizierte Secret heißt `osb-<binding-guid>-binding`, trägt den Typ
`servicebinding.io/rabbitmq` und gehört dem provisionierten CR. Wird die Instanz
gelöscht, räumt Kubernetes es mit ab, auch ohne vorheriges Unbind.

## Felder, die nichts tun

Damit niemand Zeit damit verliert:

| Feld | Was man erwartet | Was passiert |
|---|---|---|
| `plan.free` | freier oder kostenpflichtiger Plan | Katalog setzt hart `free: true` |
| `metadata.annotations` | Steuerung des Verhaltens | der Go-Typ kennt nur `name` |

## Wenn eine Definition ausgerollt wird

Der Broker liest das Definitionsverzeichnis **beim Start**. Eine geänderte
Definition wirkt erst nach einem Neustart des Pods — das Helm-Chart trägt
bewusst keine Prüfsummen-Annotation auf dem Pod-Template. Wer die
Entwicklungsplattform nutzt, bekommt den Neustart über `make broker-deploy`
mitgeliefert; siehe
[how-to/add-a-service.md](how-to/add-a-service.md).

Zwei Tests reden beim Bauen mit: `internal/definition/catalog_test.go` verlangt,
dass jede Datei in `definitions/` parst, und `schema_sync_test.go`, dass jedes
Go-Feld im JSON-Schema steht. Ein neues Feld ohne Schema-Eintrag lässt die Suite
scheitern.
