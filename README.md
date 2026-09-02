# 🚀 OSB Broker — Generischer Service Broker für Kubernetes-Operatoren

[![Go Version](https://img.shields.io/badge/go-1.22-blue.svg)](https://golang.org)
[![OSB API](https://img.shields.io/badge/OSB%20API-2.17-green.svg)](https://github.com/openservicebrokerapi/servicebroker/blob/v2.17/spec.md)
[![Tests](https://img.shields.io/badge/tests-248%20total-brightgreen.svg)]()
[![License](https://img.shields.io/badge/license-MIT-blue.svg)]()

> **Open Service Broker 2.17 in Go — Phase 1 + 2 der Roadmap abgeschlossen,
> live gegen Cloud Foundry Korifi verifiziert**

---

## 🎯 Was dieser Broker kann

Ein einziger, gehärteter Broker-Prozess macht beliebige Kubernetes-Operatoren
über die Open Service Broker API nutzbar: Ein Service wird durch eine
**ServiceDefinition (YAML)** beschrieben — Offering, Plans mit Parametern,
das zu erstellende Custom Resource Manifest, das Readiness-Kriterium und das
Credentials-Secret. Kein Broker-Code pro Service, kein externer
Datenbank-Store, keine Abhängigkeit von einem fremden Deployment.

### Verifikationsstand (24.08.2026)

| Nachweis | Ergebnis |
|----------|----------|
| OSB-2.17-Lebenszyklus über HTTP (Fake-Clients) | ✅ Integrationstest deckt catalog → provision → last_operation → bind → unbind → deprovision ab |
| Live gegen Korifi auf kind | ✅ Registrierung, Marketplace, `cf create-service` |
| **Generic Engine E2E** | ✅ `cf create-service cnpg-postgresql large my-real-pg` erzeugte einen echten CloudNativePG-Cluster (3 Instanzen, 10Gi), `psql` im Pod antwortet „E2E OK", echte Credentials aus Operator-Secret `<id>-app` |
| Pod-Restart-Persistenz | ✅ Instances/Bindings in ConfigMap `osb-broker-state`, überleben Kill & Rescheduling |

### Service-Binding-Spec-Nachweis (02.09.2026)

Gegen den echten RabbitMQ-cluster-operator, dessen CRD den
Provisioned-Service-Duck-Type ausdrücklich dokumentiert:

| Nachweis | Ergebnis |
|----------|----------|
| Secret-Name aus `status.binding.name` | ✅ Operator meldete `osb-<id>-default-user`, der Broker nutzte ihn ohne Namenstemplate |
| Zielform per `mapping` | ✅ Binding enthält `host, password, port, provider, type, uri, username` — `default_user.conf` und `connection_string` sind draußen (FINDINGS #23) |
| Spec-konformes Secret (6.4) | ✅ Type `servicebinding.io/rabbitmq`, Labels für Instanz und Binding, OwnerReference auf den `RabbitmqCluster` |
| Aufräumen beim Unbind | ✅ `cf delete-service-key` entfernt das projizierte Secret |

### State-Store-Nachweis (02.09.2026)

| Nachweis | Ergebnis |
|----------|----------|
| Conformance gegen den CRD-Store | ✅ 24/24 im kind-Cluster gegen echtes RBAC, über das Helm-Chart deployt |
| Sichtbarkeit | ✅ `kubectl get osbi` / `osbb` zeigt Instanz und Binding mit Service, Plan und Ready |
| Credentials getrennt | ✅ Nicht im Binding-CR, sondern in einem Secret mit OwnerReference darauf |
| Neustart-Persistenz | ✅ Instanz, Binding und Credentials überleben das Löschen des Pods |
| Kontext vollständig | ✅ `platform`, `spaceGuid`, `organizationGuid` werden abgebildet |

### TLS/mTLS-Nachweis (02.09.2026)

| Nachweis | Ergebnis |
|----------|----------|
| Conformance über HTTPS mit Client-Zertifikat | ✅ 24/24 Checks, im kind-Cluster über das Helm-Chart deployt — und 24/24 auch mit dem Client-Zertifikat allein, ohne Basic Auth |
| Zertifikatsrotation ohne Neustart | ✅ TLS-Secret im laufenden Pod getauscht: ausgelieferte Seriennummer wechselt, `restartCount` bleibt 0 |
| **Korifi-Registrierung über `https://`** | ✅ `CFServiceBroker` ready bei `trustInsecureServiceBrokers=false` — Korifi hat das Zertifikat gegen die Plattform-CA geprüft |
| Voller Lifecycle über HTTPS | ✅ `cf create-service cnpg-postgresql small` → gesunder CNPG-Cluster, `cf create-service-key` liefert echte Credentials, `cf delete-service` räumt ab |
| mTLS-Autorisierung | ✅ Ein von derselben CA signiertes, aber nicht auf der Allowlist stehendes Client-Zertifikat bekommt 401 |
| Probes bleiben offen | ✅ `/healthz` ohne Client-Zertifikat erreichbar (`VerifyClientCertIfGiven`) |

Die Korifi-seitige Einrichtung (Plattform-CA, Trust-Store der Korifi-Pods)
steht in [docs/tls-korifi.md](docs/tls-korifi.md).

### Funktionsumfang im Detail

**OSB 2.17 Endpoints**

| Endpunkt | Verhalten |
|----------|-----------|
| `GET /v2/catalog` | Statische Services + alle ServiceDefinitions |
| `PUT /v2/service_instances/{id}` | Definition-basiert: Template rendern → CR anlegen; legacy: Fake-Instanz |
| `PATCH /v2/service_instances/{id}` | Plan/Parameter-Update |
| `DELETE /v2/service_instances/{id}` | CR löschen; 410 Gone wenn nicht vorhanden, 409 bei offenen Bindings |
| `PUT .../service_bindings/{id}` | Credentials aus Operator-Secret |
| `DELETE .../service_bindings/{id}` | Unbind |
| `GET .../last_operation` | CR-Readiness via gjson-Pfad → `in progress`/`succeeded` |
| `GET /healthz` | Liveness/Readiness ohne Auth |

**Production Basics (Phase 1)**

- Persistenz: CRD-StateStore (kein SQL), Restart-E2E bewiesen
- Basic Auth: konstantzeitvergleichen, Secret-Injection, `/healthz` ausgenommen
- JSON-Logging mit UUID-Correlation-ID (`X-Correlation-ID`) und Audit-
  Identity (`X-OSB-Originating-Identity`)
- Zentrales Fehler-Mapping: 400/409/410 spec-korrekt je Fall

---

## 🧩 ServiceDefinition — ein Service, eine YAML

```yaml
apiVersion: broker.osb.io/v1alpha1
kind: ServiceDefinition
metadata:
  name: cnpg-postgresql
spec:
  offering:
    id: f48a9e21-cnpg-0000-0000-000000000001
    name: cnpg-postgresql
    description: "CloudNativePG PostgreSQL clusters"
    tags: [postgresql, database]
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
        name: {{ .instanceID }}
      spec:
        instances: {{ .plan.instances }}
        storage:
          size: {{ .plan.storageSize }}
  readiness:
    statusJSONPath: 'status.conditions.#(type=="Ready").status'
    expectedValue: "True"
    timeoutSeconds: 600
  bind:
    credentialsFromSecret: "{{ .instanceID }}-app"
```

Templates unterstützen beide Schreibweisen: `.InstanceID`/`.Plan` (Go-Stil)
und `.instanceID`/`.plan` (YAML-Stil, wie oben). Unbekannte Keys schlagen
laut fehl (`missingkey=error`). Beispiel liegt unter
[`definitions/cnpg-postgresql.yaml`](definitions/cnpg-postgresql.yaml).

## 🚀 Quickstart

### Lokal starten (in-memory State)

```bash
go build -o broker .
PORT=8080 ./broker
```

### In Kubernetes (persistenter State + Definitionen)

```bash
docker build -t osb-broker-go:v4 -f Dockerfile .
kind load docker-image osb-broker-go:v4 --name <cluster>

kubectl create secret generic osb-broker-auth -n osb \
  --from-literal=username=broker-user \
  --from-literal=password=broker-secret

kubectl create configmap osb-definitions -n osb \
  --from-file=definitions/cnpg-postgresql.yaml

kubectl apply -f deploy/k8s/broker.yaml
kubectl apply -f deploy/k8s/operator-rbac.yaml   # CNPG-CR-Rechte
kubectl rollout status deployment/osb-broker-go -n osb
```

### Bei Cloud Foundry registrieren

```bash
# Über TLS (Deployment-Default). Voraussetzung: Korifi vertraut der CA des
# Broker-Zertifikats - siehe docs/tls-korifi.md.
cf create-service-broker go-reference-broker broker-user broker-secret \
  "https://osb-broker-go.osb.svc.cluster.local"

# Bestehende http://-Registrierung umziehen:
# cf update-service-broker go-reference-broker broker-user broker-secret \
#   "https://osb-broker-go.osb.svc.cluster.local"

cf enable-service-access cnpg-postgresql -b go-reference-broker
cf marketplace
```

### Lifecycle-Test (echter PostgreSQL-Cluster)

```bash
cf create-service cnpg-postgresql large my-db     # erzeugt CNPG-Cluster!
cf services                                        # create succeeded
cf create-service-key my-db test-key
cf service-key my-db test-key                      # echte DB-Credentials
cf delete-service-key -f my-db test-key
cf delete-service -f my-db                         # löscht den Cluster
```

---

## ⚙️ Konfiguration

| Variable | Default | Bedeutung |
|----------|---------|-----------|
| `PORT` | `8080` | HTTP-Listenport |
| `STORE_BACKEND` | `memory` | `crd` aktiviert den persistenten Store; ein unbekannter Wert ist ein Startfehler |
| `POD_NAMESPACE` | — | Pflicht bei `STORE_BACKEND=crd`; Namespace der State-Objekte |
| `DEFINITIONS_DIR` | — | Verzeichnis mit ServiceDefinition-YAMLs (`/definitions` im Deployment) |
| `BROKER_AUTH_USER` | — | Basic-Auth-User (mit `BROKER_AUTH_PASSWORD` setzen) |
| `BROKER_AUTH_PASSWORD` | — | Basic-Auth-Passwort |
| `METRICS_ENABLED` | *(an)* | Nur der exakte Wert `0` schaltet `/metrics` ab |

### TLS und Authentifizierung (Phase 4.5)

| Variable | Default | Bedeutung |
|----------|---------|-----------|
| `TLS_ENABLED` | `false` | HTTPS-Listener; im Helm-Chart per Default **an** |
| `TLS_CERT_FILE` / `TLS_KEY_FILE` | — | Pflicht bei `TLS_ENABLED=true` |
| `TLS_MIN_VERSION` | `1.2` | `1.2` oder `1.3` |
| `TLS_RELOAD_INTERVAL` | `30s` | Poll-Intervall für Zertifikatsrotation; `0` schaltet ab |
| `AUTH_REALM` | `osb-broker` | Realm im `WWW-Authenticate`-Header |
| `AUTH_METHODS` | *(abgeleitet)* | CSV `basic,mtls`; leer = alles aktiv, was konfiguriert ist |
| `MTLS_ENABLED` | `false` | Client-Zertifikats-Authentifizierung (braucht `TLS_ENABLED`) |
| `MTLS_CLIENT_CA_FILE` | — | PEM-Bundle, gegen das Client-Zertifikate geprüft werden |
| `MTLS_REQUIRE` | `false` | `true` erzwingt Client-Zertifikate auf TLS-Ebene — siehe Warnung unten |
| `MTLS_ALLOWED_CNS` | — | CSV-Allowlist auf den Common Name |
| `MTLS_ALLOWED_DNS_NAMES` | — | CSV-Allowlist auf DNS-SANs |
| `MTLS_ALLOWED_URIS` | — | CSV-Allowlist auf URI-SANs (z. B. `spiffe://osb/checker`) |

### Server-Timeouts

| Variable | Default |
|----------|---------|
| `SERVER_READ_HEADER_TIMEOUT` | `10s` |
| `SERVER_READ_TIMEOUT` | `30s` |
| `SERVER_WRITE_TIMEOUT` | `60s` |
| `SERVER_IDLE_TIMEOUT` | `120s` |
| `SERVER_SHUTDOWN_TIMEOUT` | `15s` |

---

## 🗂️ Namespaces und Mandantentrennung

Die Backing-Ressourcen einer Instanz liegen im **Namespace des Cloud-Foundry-Space**,
nicht in einem gemeinsamen Sammelnamespace. Korifi legt seine Space-Namespaces
unter der Space-GUID an; genau darauf bildet der Broker ab.

```bash
kubectl get cluster.postgresql.cnpg.io -n <space-guid>
kubectl get osbi -n osb-broker -o custom-columns=NAME:.metadata.name,NS:.spec.namespace
```

Ohne Space-GUID — Plattformen ohne Space-Begriff — bleibt es bei `default`.

**Woher die Space-GUID kommt.** Die OSB-Spezifikation kennt zwei Wege: das
verschachtelte `context`-Objekt und die Top-Level-Felder `space_guid` /
`organization_guid` aus OSB ≤ 2.12. Letztere gelten als veraltet, werden von
Cloud Foundry aber weiter gesendet — Korifi sogar ausschließlich. Der Broker
wertet beide aus, `context` hat Vorrang.

**Warum der Namespace gespeichert wird.** Aus einem Deprovision-,
`last_operation`-, Update- oder Bind-Request ist er grundsätzlich nicht
herleitbar: keiner davon trägt `context` oder `space_guid`. Er steht deshalb im
Instanz-Datensatz (`spec.namespace`). Für Datensätze, die vor dieser Änderung
entstanden sind, greift als zweite Stufe der Namespace der angelegten Objekte,
danach `default`.

**RBAC.** Der Broker braucht damit Rechte auf die Operator-CRDs in allen
Space-Namespaces — das deckt die ClusterRole des Charts (`rbac.operatorCRDs`)
bereits ab. Feiner geschnitten ginge es erst, wenn die Menge der Spaces vorab
bekannt wäre.

---

## 🔗 Service Binding Specification

Eine Definition beschreibt nicht mehr nur *wo* das Credentials-Secret liegt,
sondern auch *welche Form* das Binding haben soll — nach der
[CNCF Service Binding Specification](https://servicebinding.io/).

```yaml
bind:
  # Der Operator nennt sein Secret selbst in .status.binding.name.
  provisionedService: true
  # Rückfallebene für Operatoren, die das Feld nicht füllen.
  credentialsFromSecret: "{{ .safeName }}-default-user"

  type: rabbitmq
  provider: rabbitmq-cluster-operator

  # Gibt die Zielform vor. Ohne mapping reicht der Broker alle Secret-Keys
  # durch — auch Konfigurationsdateien, die in einem Binding nichts zu
  # suchen haben.
  mapping:
    - name: username
      from: username
    - name: uri
      value: "amqp://{{ .credentials.username }}:{{ .credentials.password }}@{{ .credentials.host }}:{{ .credentials.port }}/"

  # Optional: dasselbe Binding zusätzlich als spec-konformes Secret.
  projectSecret: true
```

**Warum `provisionedService` der Kern ist.** Bisher musste jede Definition das
Namensschema ihres Operators nachbauen — eine Konvention, die bei jedem neuen
Operator neu zu erraten ist und an der Valkey, Redpanda und NATS gescheitert
sind. Ein Provisioned Service sagt stattdessen selbst, wo seine Credentials
liegen. Der Pfad `.status.binding.name` ist bewusst nicht konfigurierbar:
wäre er es, wäre er wieder eine Konvention und kein Standard.

**Rückwärtskompatibilität.** Eine Definition ohne die neuen Felder verhält
sich exakt wie vorher — dafür gibt es eigene Tests, Punkt für Punkt.

**`mapping` ersetzt, es ergänzt nicht.** Ist es gesetzt, besteht das Ergebnis
genau aus den genannten Keys plus `type`/`provider`. Ein Adapter, der daneben
noch alle Originalschlüssel durchreicht, macht die definierte Zielform
zunichte. Ein fehlender Quellschlüssel bricht hart ab, statt still ein halb
gefülltes Binding zu liefern, das erst in der App auffällt.

**`projectSecret` braucht zusätzliche Rechte.** Das Secret entsteht im
Ziel-Namespace der Instanz, nicht im Broker-Namespace — im Helm-Chart ist das
`rbac.projectedBindingSecrets: true`. Fehlt die Berechtigung, schlägt der Bind
fehl mit einer Meldung, die genau sie benennt.

---

## 💾 State Store

Instanzen und Bindings liegen als je ein Custom Resource
(`OSBServiceInstance`, `OSBServiceBinding`) im Namespace des Brokers.

```bash
kubectl apply -f deploy/crds/          # einmalig, cluster-weit
kubectl get osbi -n osb                # Instanzen
kubectl get osbb -n osb                # Bindings
```

**Warum nicht mehr eine ConfigMap.** Bis Phase 4 lag der gesamte Zustand als
ein JSON-Dokument unter einem Key in der ConfigMap `osb-broker-state`. Gemessen
an realen Datensätzen (543 Bytes je Instanz, 1.496 Bytes je Binding mit
PostgreSQL-Credentials) war damit das 1-MiB-Limit bei rund **514 Instanzen**
erreicht — und bis dahin schrieb *jeder* Provision-, Bind- oder
Deprovision-Aufruf den gesamten Zustand neu, bei 1000 Instanzen also 1,9 MiB
pro Aufruf. Zwei gleichzeitige Schreiber überschrieben sich dabei still, weil
die `resourceVersion` für den Update aus einem zweiten Get stammte und nicht
aus dem, auf dem die Änderung beruhte.

Ein Objekt je Datensatz behebt alle drei Punkte: kein Gesamtlimit, ein
Schreibzugriff kostet einen Datensatz, und Konflikte betreffen nur den
Datensatz, an dem wirklich zwei Schreiber arbeiten (`RetryOnConflict` statt
stillem Überschreiben) — womit auch mehr als eine Replica möglich wird.

**Credentials** stehen nicht im Binding-CR, sondern in einem eigenen Secret,
auf das `spec.credentialsSecret` verweist. In der ConfigMap lagen sie im
Klartext neben allem anderen.

**Objektnamen** sind die OSB-ID, wenn sie ein gültiger Kubernetes-Name ist
(der Normalfall — Cloud Foundry schickt UUIDs). Sonst entscheidet ein Hash;
die ursprüngliche ID steht immer in `spec.id`, und Lesezugriffe vergleichen
sie.

### Umstieg von der ConfigMap

Der Schnitt ist hart — der Broker liest die alte ConfigMap nicht mehr. Wer
bestehende Instanzen hat, muss sie übertragen, sonst sind sie für den Broker
verloren und die angelegten Datenbanken bleiben als Waisen stehen:

```bash
go build -o osb-state-migrate ./cmd/osb-state-migrate/
./osb-state-migrate --namespace osb --dry-run   # erst zählen
./osb-state-migrate --namespace osb             # dann übertragen
```

Der Lauf ist idempotent und lässt die alte ConfigMap stehen — sie ist die
Rückfallebene, bis die Migration geprüft ist. `STORE_BACKEND=k8s` wird
weiterhin als `crd` gelesen, damit bestehende Deployments beim Upgrade nicht
still im Speicher landen; der Broker warnt dann beim Start.

---

## 🔐 TLS und mTLS

Der Broker liefert produktive Datenbank-Credentials aus. Seit Phase 4.5
terminiert er TLS selbst; das Helm-Chart deployt per Default über HTTPS.

**Die Methoden sind gleichrangig — eine genügt.** Cloud Foundry schickt Basic
Auth, zertifikatsbasierte Clients kommen über mTLS. Ein `401` nennt im
`WWW-Authenticate`-Header die aktiven HTTP-Challenges; mTLS taucht dort nicht
auf, weil es unterhalb von HTTP stattfindet.

Immer unauthentifiziert, damit Probes und Scrapes funktionieren:
`/healthz`, `/metrics`, `/openapi.yaml`, `/schemas/service-definition.schema.json`.

### Lokal ausprobieren

```bash
mkdir -p /tmp/osbcerts && cd /tmp/osbcerts
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -sha256 \
  -keyout ca.key -out ca.crt -subj "/CN=osb-ca"
openssl req -newkey rsa:2048 -nodes -keyout tls.key -out server.csr \
  -subj "/CN=osb-broker-go"
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -days 1 -sha256 -out tls.crt \
  -extfile <(printf "subjectAltName=DNS:localhost,IP:127.0.0.1\nextendedKeyUsage=serverAuth\n")
openssl req -newkey rsa:2048 -nodes -keyout client.key -out client.csr \
  -subj "/CN=osb-checker"
openssl x509 -req -in client.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -days 1 -sha256 -out client.crt \
  -extfile <(printf "extendedKeyUsage=clientAuth\n")

cd -
TLS_ENABLED=true TLS_CERT_FILE=/tmp/osbcerts/tls.crt TLS_KEY_FILE=/tmp/osbcerts/tls.key \
MTLS_ENABLED=true MTLS_CLIENT_CA_FILE=/tmp/osbcerts/ca.crt MTLS_ALLOWED_CNS=osb-checker \
BROKER_AUTH_USER=u BROKER_AUTH_PASSWORD=p PORT=8443 ./broker
```

```bash
curl --cacert /tmp/osbcerts/ca.crt https://localhost:8443/healthz            # 200, ohne alles
curl --cacert /tmp/osbcerts/ca.crt https://localhost:8443/v2/catalog          # 401
curl --cacert /tmp/osbcerts/ca.crt -u u:p \
     -H 'X-Broker-API-Version: 2.17' https://localhost:8443/v2/catalog        # 200 per Basic
curl --cacert /tmp/osbcerts/ca.crt --cert /tmp/osbcerts/client.crt \
     --key /tmp/osbcerts/client.key \
     -H 'X-Broker-API-Version: 2.17' https://localhost:8443/v2/catalog        # 200 per mTLS
```

### Zertifikatsrotation

Zertifikat und Client-CA werden alle `TLS_RELOAD_INTERVAL` neu eingelesen —
**ohne Pod-Neustart**. Deshalb trägt das TLS-Secret im Chart bewusst *keine*
`checksum`-Annotation. Erkannt wird die Rotation über Inhalts-Digests, nicht
über inotify: Kubernetes tauscht Secret-Volumes über einen `..data`-Symlink,
und ein Watch auf dem Blattpfad feuert danach nicht mehr.

Ein fehlgeschlagener Reload behält das letzte gute Material und loggt nur —
eine halb geschriebene Datei darf den Listener nie abreißen.

### Warnung zu `MTLS_REQUIRE=true`

Der strikte Modus (`RequireAndVerifyClientCert`) bricht den **Handshake** ab,
wenn kein Client-Zertifikat vorliegt — vor jeder Middleware. Das trifft das
Kubelet auf `/healthz` und einen Prometheus-Scrape auf `/metrics`, die beide
keins schicken. Das Chart stellt die Probes deshalb automatisch auf
`tcpSocket` um, sobald `auth.mtls.required` gesetzt ist; Prometheus braucht
dann ein eigenes Zertifikat. Default ist deshalb der optionale Modus
(`VerifyClientCertIfGiven`): ein vorgelegtes Zertifikat wird voll verifiziert,
sein Fehlen lässt Basic Auth greifen.

### Allowlist ist Autorisierung, nicht Authentifizierung

Ein Common Name ist vom Antragsteller gewählter Text, den **jede** CA im Pool
signieren kann. `MTLS_CLIENT_CA_FILE` gehört deshalb eng gehalten (eine
interne CA, nie der System-Pool), und SANs sind dem CN vorzuziehen. Sind alle
drei Allowlists leer, wird jedes von dieser CA signierte Zertifikat
akzeptiert — der Broker warnt dann beim Start.

### Korifi

Korifi validiert Broker-Zertifikate per Default und kennt keinen Wert für
eine broker-spezifische CA. Die Einrichtung steht in
[docs/tls-korifi.md](docs/tls-korifi.md).

---

## 🏗️ Architektur

```text
cf create-service cnpg-postgresql large mydb
     │
     ▼
handlers ──► resolveDefinition(service_id)
     │             │
     │       definition.Engine
     │         ├─ PlanByID → RenderProvision (Template + Plan-Params)
     │         ├─ ApplyCR   → CR im Space-Namespace
     │         ├─ EvaluateReadiness (gjson auf CR-Status)
     │         └─ BindCredentials → Secret <id>-app lesen
     ▼                                    │
VCAP_SERVICES                       Operator reconciliert
                                   (PostgreSQL-Pods, Secrets)
```

| Paket | Verantwortung |
|-------|---------------|
| `internal/definition` | Parse/Validate/Render/Engine/OperatorClient/LoadFromDir |
| `internal/broker` | Legacy-Fake-Services + StateStore (memory/K8s-ConfigMap) |
| `internal/handlers` | HTTP-Layer, Dispatch legacy vs. definition, Auth, Logging, Error-Mapping |

Persistenz: Instances/Bindings liegen als JSON in ConfigMap
`osb-broker-state` (Phase 1) — kein SQL/Redis nötig.

---

## 🔌 Unterstützte Kubernetes-Operatoren

Der Broker ist **operator-agnostisch**: Jeder Operator, der die beiden
Anforderungen unten erfüllt, lässt per reiner YAML-Definition in den
Marktplace bringen — ohne Broker-Code, ohne neues Deployment.

### Anforderungen an einen Operator

| # | Anforderung | Warum |
|---|-------------|-------|
| 1 | **CRD für Service-Instanzen** | Der Broker rendert aus dem Template ein Custom Resource Manifest und applied es (`ApplyCR`). GVK (apiVersion/kind) steht in der Definition. |
| 2 | **Credentials als Kubernetes-Secret** | Beim Bind liest der Broker ein Secret und liefert dessen Keys als OSB-Credentials. Der Secret-Name wird per Template gerendert, z. B. `{{ .safeName }}-app`. |
| 3 | **Status-Condition oder Statusfeld** | Für `last_operation` wertet der Broker einen gjson-Pfad ins CR-Statusobjekt aus (z. B. `status.conditions.#(type=="Ready").status` gegen `expectedValue`). Fehlt das Feld → `in progress`. |

Empfehlungen (kein Muss, aber Enterprise-relevant):

| Empfehlung | Grund |
|-----------|-------|
| Status-Conditions nach K8s-Konvention (`type=Ready`) | Einheitliche Readiness-Pfade über alle Services |
| Credentials rotierbar (Operator aktualisiert das Secret) | Rebind liest immer frisch — Rotation ohne Broker-Beteiligung |
| Namespace-scoped CRs | Korifi mappt Spaces auf Namespaces; Cluster-scoped CRs passen nicht zum Mandantenmodell |
| Webhook validiert Namensschema | CNPG lehnt z. B. bare GUIDs ab — deshalb setzt der Broker `safeName` mit `osb-`-Präfix (≤ 63 Zeichen, DNS-1123-Label) |

### Mitgelieferte Beispiel-Definitionen (`definitions/`)

| Service | Operator | API-Version / Kind | Plans | Credentials-Secret |
|---------|----------|--------------------|-------|--------------------|
| **cnpg-postgresql** ✅ E2E-verifiziert | [CloudNativePG](https://cloudnative-pg.io/) | `postgresql.cnpg.io/v1` / `Cluster` | small (1×, 1Gi), large (3×, 10Gi HA) | `<name>-app` (basic-auth: user/pass/host/…) |
| **redis-standalone** | [Opstree Redis Operator](https://ot-container-kit.github.io/redis-operator/) | `redis.redis.opstreelabs.in/v1beta2` / `Redis` | dev (128Mi), prod (1Gi + 2 Replicas) | `<name>-redis` (Konvention — siehe Hinweis in der Definition) |
| **minio-objectstorage** | [MinIO Operator](https://min.io/docs/minio/kubernetes/upstream/) | `minio.min.io/v2` / `Tenant` | small (1 Server, 10Gi), large (4×2 Volumes, 50Gi EC) | `<name>-secret` (accessKey/secretKey) |

### Weitere Operatoren, die nachweislich passen

Diese Operatoren erfüllen alle drei Anforderungen und sind bekannte
Kandidaten für weitere Definitionen (Beitrag = eine YAML):

| Operator | Service | Kind | Typisches Credentials-Secret |
|----------|---------|------|------------------------------|
| Percona (PXC / PostgreSQL / MongoDB) | MySQL, PostgreSQL, MongoDB | `PerconaXtraDBCluster`, `PostgresCluster`, `PerconaServerMongoDB` | `<name>-secrets` etc. |
| CrunchyData PGO | PostgreSQL | `PostgresCluster` | `<name>-pguser-<user>` |
| Strimzi | Apache Kafka | `Kafka`, `KafkaTopic` | `<name>-<user>` (KafkaUser) |
| RabbitMQ Cluster Operator | RabbitMQ | `RabbitmqCluster` | `<name>-default-user` |
| Redis Operator (Spotahome) | Redis Sentinel | `RedisFailover` | via `<name>`-Secrets |
| MongoDB Community Operator | MongoDB ReplicaSet | `MongoDBCommunity` | `<name>-admin-<user>`, SCRAM-Keys |
| InfluxDB Operator | InfluxDB | `InfluxDB` | `<name>-auth` |
| ClickHouse Operator (Altinity) | ClickHouse | `ClickHouseInstallation` | `<name>-password` etc. |
| Solr / Kafka (Strimzi-Erweiterung) u. a. | Suchindex, Event Streaming | je nach Operator | je nach Operator |

> **Wichtig:** Die Liste ist nicht abschließend — sie zeigt das Muster.
> Ob ein konkreter Operator passt, entscheidet allein die Dreier-Prüfung
> oben (CRD ✓, Secret ✓, Statusfeld ✓). Neue Definition = YAML in
> `definitions/` ablegen; der Catalog-Guard-Test prüft automatisch.

---

## 🗺️ Nächste Schritte (Roadmap v2.3)

- ✅ **Phase 6 — CNCF Service Binding Specification**: Der Secret-Name kommt
  aus `.status.binding.name` des Operators statt aus einem nachgebauten
  Namensschema; `mapping` gibt die Zielform vor, `type`/`provider` machen das
  Binding spec-konform, und `projectSecret` legt es zusätzlich als Secret für
  Kubernetes-Workloads ab. Bestehende Definitionen laufen unverändert weiter.
  Ohne neue Abhängigkeit — die Templates nutzen die vorhandene
  `text/template`-Engine statt CEL.
- ✅ **Phase 5 — CRD-State-Store**: Instanzen und Bindings als je ein Custom
  Resource statt eines JSON-Blobs in einer ConfigMap. Behebt das
  1-MiB-Limit (~514 Instanzen), das Neuschreiben des gesamten Zustands bei
  jedem Aufruf und die verlorenen Schreibvorgänge bei gleichzeitigem Zugriff.
  Credentials liegen in Secrets, nicht mehr im Klartext neben dem Zustand.
  Harter Schnitt mit Migrationswerkzeug (`cmd/osb-state-migrate`); beide
  CI-Gates fahren jetzt gegen den persistenten Store statt gegen den Speicher.
- ✅ **Phase 4.5 — TLS/mTLS**: Der Broker terminiert TLS selbst
  (Hot-Reload bei Rotation, ohne Pod-Neustart), authentifiziert wahlweise
  per Basic Auth oder Client-Zertifikat mit CN/SAN-Allowlist, und bringt
  echten `http.Server` mit Timeouts und Graceful Shutdown mit. Eigenes
  CI-Gate (L2b) fährt die 24 Conformance-Checks über HTTPS mit
  Client-Zertifikat. **OAuth2 bewusst nicht gebaut**: der Broker soll ohne
  IdP betreibbar bleiben, ein JWT-Validator ohne konkreten IdP wäre Code
  auf Vorrat. Die `Authenticator`-Schnittstelle ist der Einstiegspunkt,
  falls er später kommt.
- **Phase 4**: Helm Chart, CI mit osb-checker als Conformance-Gate,
  Prometheus-Metriken
  - ✅ OpenAPI-Doku: `GET /openapi.yaml` (volle OSB-API-Spec) und
    `GET /schemas/service-definition.schema.json` — unauthentifiziert,
    zur Compile-Zeit ins Binary eingebettet
- **Java-Nachzug**: Phase 1+2 Portierung (Go-Design stabil)
- ✅ **Engine: Multi-Doc-Templates** (4.6, abgeschlossen): `provision.template` auf mehrere
  durch `---` getrennte Manifeste erweitern. Hintergrund: die Engine legt
  aktuell genau eine Ressource pro Instanz an (`yaml.Unmarshal` in
  `internal/definition/operator.go`). Mehrere Dienste scheitern daran:
    - Kafka via Strimzi braucht `KafkaNodePool` + `Kafka` (2 CRs)
    - RocketMQ braucht `NameService` + `Broker` (mehrere CRDs)
    - Dgraph braucht Zero + Alpha (2 StatefulSets)
  Umbauumfang: Rendering am `\n---` splitten und in Reihenfolge anwenden,
  Delete über alle Objekte (OwnerReferences/Labels), konfigurierbares
  Readiness-/Bind-Ziel je Dokument, Update-PATCH-Ressource definieren,
  Rollback-Semantik bei Teilfehler, Conformance-Gate (L2) erweitern.
  Nutzen: schaltet Strimzi-Kafka, RocketMQ und Dgraph als Service-Angebote
  frei; Alternative bis dahin für Kafka = ältere Strimzi-Version pinnen
  (einzelnes `Kafka`-CR ohne NodePools).
  Dienst-Bewertung vom 25.08.2026 (Prüfung gegen Dreier-Muster
  CRD/Secret/Status): geeignet = Valkey (Definition liegt in
  `definitions/valkey-cluster.yaml`, Deployment ausstehend), RabbitMQ
  (cluster-operator, Default-User-Secret via `status.binding`), Redpanda
  (offizieller Operator); bedingt = NATS (Operator archiviert — Variante
  plain StatefulSet mit `status.readyReplicas` + Konventions-Secret);
  verworfen = NiFi (ZooKeeper-Pflicht), Supabase, Yugabyte (legacy bzw.
  kommerzieller YBA-Stack).
- **S3-Nachfolger für MinIO**: MinIO Community ist EOL (minio-operator
  deprecated). Neuer Standard: **SeaweedFS** via seaweedfs/seaweedfs-operator
  (`seaweed.seaweedfs.com/v1 Seaweed` — ein CR mit master/volume/filer/s3,
  Ready-Condition, S3Credentials-CRDs). Definition: `definitions/seaweedfs-s3.yaml`.
  MinIO-Definition als DEPRECATED markiert (läuft weiter, kein Neuausbau).
  Alternative geprüft und verworfen: Garage (deuxfleurs-org) — nur
  Discovery-CRD ohne Status/Automatik, StatefulSet-Handarbeit nötig;
  offen bleibt die Credentials-Automatisierung beim Bind bei SeaweedFS
  (derzeit Konventions-Secret; mittelfristig Bind via S3Identities-CRD).
- **Observability-Abhängigkeit für die Metriken**: Im kind-Cluster läuft
  aktuell kein Prometheus (nur metrics-server für kubectl top) — der
  Broker-/Operator-Metriken-Punkt aus Phase 4 braucht daher zuerst einen
  Sammelstack. Wachstumspfad:
    1. Jetzt (Entwicklung): kube-prometheus-stack via Helm in
       Namespace `monitoring` (Prometheus + Grafana + Alertmanager,
       optional Loki single-binary). Broker exponiert `/metrics`
       im Prometheus-Format (promhttp) — Request-Latenzen, Provision-Dauer,
       aktive Instanzen, Fehlerquoten; Scrape via ServiceMonitor.
    2. Später (Produktion): zentraler LGTM-Stack (Loki, Grafana, Tempo,
       Mimir als langlebiger Metrik-Speicher) für Multi-Cluster/Flotten.
       Broker-seitig ändert sich nichts: `/metrics` bleibt Prometheus-
       Format; strukturiertes Logging (JSON) und ggf. OpenTelemetry-Hooks
       früh vorbereiten, damit Logs/Traces später ohne Code-Umbau anbandeln.

Aktueller Arbeitsstand und Empfehlung für den nächsten Schritt:
[docs/STAND.md](docs/STAND.md)

Gesamt-Roadmap inkl. Java-Status:
[development-open-service-broker/roadmap.md](https://github.com/cyrano-janus/development-open-service-broker/blob/main/roadmap.md)

---

## 📖 API-Dokumentation

Der Broker dient seine eigene API-Doku aus (unauthentifiziert):

```bash
curl http://localhost:8080/openapi.yaml                              # OpenAPI 3.0.3 Spec
curl http://localhost:8080/schemas/service-definition.schema.json    # JSON-Schema
```

Die Spec in Swagger-UI/Redoc laden oder mit `oapi-codegen`-Clients
konsumieren. Die ServiceDefinition-YAMLs lassen sich offline gegen das
JSON-Schema validieren (`definitions/*` im Repo sind die Referenz).

## 🧪 Entwicklung & Tests

```bash
go vet ./...                    # statische Analyse
go test ./... -count=1          # komplette Suite ohne Cache

# Bereiche
go test ./internal/definition/ -v   # Parse/Render/Engine/OperatorClient
go test ./internal/broker/ -v       # StateStore + Restart-Konsistenz
go test ./internal/handlers/ -v     # Auth, Logging, Error-Mapping,
                                    # Definition-Lifecycle über HTTP
```

Test-Philosophie: TDD — jeder neue Behavior-Path bekommt zuerst einen
scheiternden Test. Restart-Konsistenz und Definition-Lifecycle sind als
Verträge getestet; die K8s-Implementierungen müssen dieselben Tests bestehen.

---

## 📄 Lizenz

MIT — siehe [LICENSE](LICENSE).

---

<div align="center">

**Built by a Cloud Foundry enthusiast, for the CF community.** 💙

</div>
