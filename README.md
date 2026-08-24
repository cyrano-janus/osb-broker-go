# 🚀 OSB Broker — Generischer Service Broker für Kubernetes-Operatoren

[![Go Version](https://img.shields.io/badge/go-1.22-blue.svg)](https://golang.org)
[![OSB API](https://img.shields.io/badge/OSB%20API-2.17-green.svg)](https://github.com/openservicebrokerapi/servicebroker/blob/v2.17/spec.md)
[![Tests](https://img.shields.io/badge/tests-84%20total-brightgreen.svg)]()
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

- Persistenz: ConfigMap-StateStore (kein SQL), Restart-E2E bewiesen
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
cf create-service-broker go-reference-broker broker-user broker-secret \
  "http://osb-broker-go.osb.svc.cluster.local"   # http:// ist Pflicht!

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
| `STORE_BACKEND` | *(leer = memory)* | `k8s` aktiviert den ConfigMap-StateStore |
| `POD_NAMESPACE` | — | Pflicht bei `STORE_BACKEND=k8s`; Namespace der State-ConfigMap |
| `DEFINITIONS_DIR` | — | Verzeichnis mit ServiceDefinition-YAMLs (`/definitions` im Deployment) |
| `BROKER_AUTH_USER` | — | Basic-Auth-User (mit `BROKER_AUTH_PASSWORD` setzen) |
| `BROKER_AUTH_PASSWORD` | — | Basic-Auth-Passwort |

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

## 🗺️ Nächste Schritte (Roadmap v2.1)

- **Phase 3**: Definition-Catalog (Redis, MinIO als YAML), JSON-Schema-
  Validierung pro Plan, Credential-Rotation, optional Instance Sharing
- **Phase 4**: Helm Chart, CI mit osb-checker als Conformance-Gate,
  Prometheus-Metriken, OpenAPI-Doku, optional mTLS/OAuth2

Gesamt-Roadmap inkl. Java-Status:
[development-open-service-broker/roadmap.md](https://github.com/cyrano-janus/development-open-service-broker/blob/main/roadmap.md)

---

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
