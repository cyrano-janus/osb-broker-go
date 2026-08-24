# 🚀 OSB Broker & Checker - Complete Suite

[![Go Version](https://img.shields.io/badge/go-1.22-blue.svg)](https://golang.org)
[![OSB API](https://img.shields.io/badge/OSB%20API-2.17-green.svg)](https://github.com/openservicebrokerapi/servicebroker/blob/v2.17/spec.md)
[![Tests](https://img.shields.io/badge/tests-53%20total-brightgreen.svg)]()
[![License](https://img.shields.io/badge/license-MIT-blue.svg)]()

> **Open Service Broker API 2.17 Referenz-Implementierung in Go —
> production-hardened (Phase 1) und live gegen Cloud Foundry Korifi verifiziert**

---

## 🎯 Was dieser Broker heute kann

### OSB 2.17 — vollständiger Lebenszyklus

| Endpunkt | Verhalten |
|----------|-----------|
| `GET /v2/catalog` | Katalog mit Services + Plans |
| `PUT /v2/service_instances/{id}` | Provision (idempotent; Konflikt bei abweichendem service/plan → 409) |
| `PATCH /v2/service_instances/{id}` | Update (Plan-Wechsel, Parameter) |
| `GET /v2/service_instances/{id}` | Instanz abrufen |
| `DELETE /v2/service_instances/{id}` | Deprovision — **410 Gone** wenn nicht vorhanden, **409** solange Bindings offen sind |
| `PUT .../service_bindings/{id}` | Bind (idempotent, liefert bestehende Credentials zurück) |
| `DELETE .../service_bindings/{id}` | Unbind |
| `GET .../last_operation` | Operations-Status (`succeeded`) |
| `GET /healthz` | Liveness/Readiness — **ohne Auth** (K8s-Probe-tauglich) |

### Phase 1 „Production Basics" (abgeschlossen, 24.08.2026)

#### 1.1 Persistenz ohne externe Datenbank

Instances und Bindings werden über ein `StateStore`-Interface verwaltet.
Zwei Implementierungen:

- **`memoryStateStore`** — Default für lokale Runs und Tests
- **`K8sStateStore`** — persistiert als JSON in der ConfigMap
  `osb-broker-state`; Kubernetes repliziert und hält sie auf Disk, dadurch
  überleben Instances/Bindings Pod-Restarts und Rescheduling **ohne SQL,
  Redis oder einen anderen externen Store**

Aktivierung: `STORE_BACKEND=k8s` + `POD_NAMESPACE=<namespace>`.

Verifiziert im Cluster: Service-Instanz per `cf create-service` angelegt,
Broker-Pod gelöscht, neuer Pod gestartet — Instanz und Binding danach
vollständig abrufbar, Service-Key lieferte die Credentials.

Alle Broker-Pfade laufen über den Store: Provision-Idempotenz,
Binding-Schutz bei Deprovision und Get-/Unbind-Lookups verhalten sich nach
einem Restart identisch wie vorher (abgedeckt durch
`restart_consistency_test.go`).

#### 1.2 Basic Auth

- Alle OSB-Endpoints verlangen HTTP Basic Auth, sobald
  `BROKER_AUTH_USER` / `BROKER_AUTH_PASSWORD` gesetzt sind (in Kubernetes
  aus einem Secret injiziert)
- Fehlende/falsche Credentials → `401` mit `WWW-Authenticate:
  Basic realm="osb-broker"` (RFC 7617)
- Vergleich in konstanter Zeit (`crypto/subtle`) gegen Timing-Angriffe
- `/healthz` ist bewusst ausgenommen — Proben tragen keine Credentials
- Beide Variablen leer → Auth deaktiviert (Rückwärtskompatibilität), mit
  lauter Warnung im Log

#### 1.3 Structured Logging mit Correlation IDs

- Eine JSON-Zeile pro Request statt gin-Textlog:

```json
{"timestamp":"2026-08-24T14:46:36.893143256Z","level":"info","message":"request","correlation_id":"9c3872b8-601d-1dc1-43bd-152a7f85ec2c","method":"PUT","path":"/v2/service_instances/9ace35ba-...","status":200,"duration_ms":12,"client_ip":"172.18.0.3"}
```

- `X-Correlation-ID`: eingehender Header wird übernommen, sonst wird eine
  UUID generiert; immer im Response-Header echo-t
- `X-OSB-Originating-Identity` wird je Request erfasst und geloggt — Audit-
  fähig (wer hat was angelegt)
- Panic-Schutz via `gin.Recovery`

#### 1.4 Zentrales OSB-Fehler-Mapping

Alle Fehler laufen durch eine Schicht (`respondOSBError`), die spec-korrekte
Statuscodes garantiert:

| Fall | Code |
|------|------|
| Unbekannte service_id / plan_id | 400 Bad Request |
| Instanz existiert mit anderem service/plan | 409 Conflict |
| Deprovision trotz offener Bindings | 409 Conflict |
| DELETE auf nicht vorhandener Instanz / Binding | **410 Gone** (OSB-Spec) |
| Bindings auf unbekannter Instanz | 400 Bad Request |
| Alles andere | 500 Internal Server Error |

Jede Fehler-Response trägt die Correlation-ID des Requests.

### Deployment-Artefakte

- **Dockerfile**: Multi-Stage (golang → distroless, nonroot)
- **Manifeste** (`deploy/k8s/broker.yaml`): Namespace, ServiceAccount, RBAC
  (least-privilege Role nur auf die State-ConfigMap via `resourceNames`),
  Deployment mit Liveness/Readiness-Probes auf `/healthz`, ClusterIP-Service
- **RBAC**: Der Broker darf ausschließlich seine eigene State-ConfigMap
  lesen/erstellen/updaten — nichts anderes

### Verifikationsstand

- `go vet ./...` sauber, komplette Test-Suite grün (53 Tests, `-count=1`)
- Live gegen **Cloud Foundry Korifi** auf kind geprüft: Registrierung als
  Service-Broker, Marketplace-Sichtbarkeit, `cf create-service`,
  Service-Keys, Pod-Restart-Persistenz

---

## 🚀 Quickstart

### Lokal starten (in-memory State)

```bash
go build -o broker .
PORT=8080 ./broker
```

### In Kubernetes (persistenter State)

```bash
# Image bauen und in den Cluster laden
docker build -t osb-broker-go:v3 -f Dockerfile .
kind load docker-image osb-broker-go:v3 --name <cluster>

# Auth-Secret anlegen
kubectl create secret generic osb-broker-auth -n osb \
  --from-literal=username=broker-user \
  --from-literal=password=broker-secret

# Deployen (Namespace, SA, RBAC, Deployment, Service)
kubectl apply -f deploy/k8s/broker.yaml
kubectl rollout status deployment/osb-broker-go -n osb
```

### Bei Cloud Foundry registrieren

```bash
cf create-service-broker go-reference-broker broker-user broker-secret \
  "http://osb-broker-go.osb.svc.cluster.local"

# Wichtig: http:// explizit angeben — ohne Schema ruft Korifi HTTPS auf
# Port 443 an und der Catalog-Fetch timet out.

cf enable-service-access example-service -b go-reference-broker
cf marketplace
```

### Lifecycle-Test

```bash
cf create-service example-service free my-db
cf services                                  # create succeeded
cf create-service-key my-db test-key
cf service-key my-db test-key                # Credentials im VCAP-Format
cf delete-service-key -f my-db test-key
cf delete-service -f my-db
```

---

## ⚙️ Konfiguration (Umgebungsvariablen)

| Variable | Default | Bedeutung |
|----------|---------|-----------|
| `PORT` | `8080` | HTTP-Listenport |
| `STORE_BACKEND` | *(leer = memory)* | `k8s` aktiviert den ConfigMap-StateStore |
| `POD_NAMESPACE` | — | Pflicht bei `STORE_BACKEND=k8s`; Namespace der State-ConfigMap |
| `BROKER_AUTH_USER` | — | Basic-Auth-User (mit `BROKER_AUTH_PASSWORD` setzen) |
| `BROKER_AUTH_PASSWORD` | — | Basic-Auth-Passwort |

---

## 🗺️ Nächste Schritte (Roadmap v2.1)

Die Gesamt-Roadmap lebt im
[`development-open-service-broker`](https://github.com/cyrano-janus/development-open-service-broker)
Workspace. Für diesen Broker folgt als nächstes:

### Phase 2 — Generic Engine (Q1 2027, nächster Schritt)

Der Kern der Generik: Statt fest verdrahtetem `example-service` liest der
Broker deklarative **ServiceDefinitions** (YAML), die beliebige
Kubernetes-Operatoren beschreiben:

1. **ServiceDefinition-API (2.1)** — CRD/ConfigMap-Format, Validierung beim
   Laden, Hot-Reload. Definiert Offering, Plans mit Parametern und das
   Mapping auf eine Custom Resource (apiVersion/kind/template).
2. **Template-Renderer (2.2)** — Go-Templates mit `.instanceID`,
   `.plan.*`, `.parameters` rendern das CR-Manifest; striktes Escaping.
3. **CR-Lifecycle (2.3)** — Provision = CR apply, Deprovision = CR delete,
   Update = CR patch (echte Plan-Wechsel).
4. **Readiness-Polling (2.4)** — JSONPath auf den CR-Status speist
   `last_operation` (`in progress` / `succeeded` / `failed`, inkl. Timeout).
5. **Binding-Extraktion (2.5)** — Credentials kommen aus dem vom Operator
   erzeugten Secret (z.B. `<instance>-app` bei CloudNativePG); Re-Bind liest
   frisch (Rotation-fähig).
6. **RBAC-Erweiterung (2.6)** — least-privilege Role auf Space-Namespaces
   ausweiten (CR create/update/delete nur für die gemappten Kinds).

Erster End-to-End: eine CNPG-Definition, sodass
`cf create-service cnpg-postgresql large mydb` einen echten
PostgreSQL-Cluster provisioniert und Apps die echten Zugangsdaten via
`VCAP_SERVICES` erhalten.

### Danach (Ausblick)

- **Phase 3**: Definition-Catalog (CNPG, Redis, MinIO als YAML), JSON-Schema-
  Validierung pro Plan, Credential-Rotation, optional Instance Sharing
- **Phase 4**: Helm Chart, CI mit osb-checker als Conformance-Gate,
  Prometheus-Metriken, OpenAPI-Doku, optional mTLS/OAuth2

Details und Status aller Phasen: [Roadmap v2.1 im Workspace-Repo](https://github.com/cyrano-janus/development-open-service-broker/blob/main/roadmap.md).

---

## 🧪 Entwicklung & Tests

```bash
go vet ./...          # statische Analyse
go test ./... -count=1  # komplette Suite ohne Cache

# Einzelbereiche
go test ./internal/broker/ -run TestK8sStateStore -v     # Persistenz (Fake-APIserver)
go test ./internal/broker/ -run TestRestart -v           # Restart-Konsistenz
go test ./internal/handlers/ -run TestBasicAuth -v       # Auth-Middleware
go test ./internal/handlers/ -run TestErrorMapping -v    # OSB-Fehlercodes
```

Test-Philosophie: TDD — jeder neue Behavior-Path bekommt zuerst einen
scheiternden Test. Die Restart-Konsistenz-Tests sind der Vertrag für jeden
künftigen StateStore (die K8s-Implementierung muss dieselben Tests bestehen).

---

## 🤝 Contributing

Feature aussuchen → Issue eröffnen → implementieren → PR. Jeder PR sollte:
einen fokussierten Zweck haben, Tests enthalten und `go vet`/`go test`
grün halten.

---

## 📄 Lizenz

MIT — siehe [LICENSE](LICENSE).

---

<div align="center">

**Built by a Cloud Foundry enthusiast, for the CF community.** 💙

</div>
