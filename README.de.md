# OSB Broker — generischer Service Broker für Kubernetes-Operatoren

[![Go](https://img.shields.io/badge/go-1.26-blue.svg)](https://golang.org)
[![OSB API](https://img.shields.io/badge/OSB%20API-2.17-green.svg)](https://github.com/openservicebrokerapi/servicebroker/blob/v2.17/spec.md)
[![Lizenz](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

> [English](README.md) · Führende Fassung: deutsch

Ein gehärteter Prozess macht beliebige Kubernetes-Operatoren über die
Open-Service-Broker-API verfügbar. Ein neuer Service ist eine YAML-Datei, kein
neuer Broker: kein Code je Service, keine externe Datenbank.

## Wofür das gebaut wird

| Rolle | System |
|---|---|
| **Zielplattform** | produktives Cloud Foundry |
| **Zielplattform** | Tanzu TAS |
| **Zielplattform** | externe Marketplaces mit OSB-Anbindung |
| **Entwicklungsplattform** | Korifi auf kind — Testgerät, kein Zielsystem |

Das steht hier oben, weil es die Lesart von allem Folgenden bestimmt: sämtliche
Nachweise in diesem Dokument sind gegen die **Entwicklungsplattform**
aufgenommen. Mehrere bekannte Abweichungen von OSB 2.17 bleiben dort folgenlos
und sind auf einem Zielsystem Blocker. Was das im Einzelnen heißt, steht in
[docs/de/target-platforms.md](docs/de/target-platforms.md).

## Der Ansatz

```
v1:  ein Broker je Service-Typ            →  N Codebasen, N Deployments
v2:  eine Engine + N ServiceDefinitions   →  1 Codebasis, Konfiguration statt Code
```

```
  Cloud Foundry / TAS / OSB-Client
            │  OSB 2.17 über HTTPS
            ▼
  ┌──────────────────────────────────────────┐
  │  osb-broker-go                            │
  │  ServiceDefinition (YAML) ──▶ Operator-CR │
  │  Zustand in eigenen CRDs                  │
  └──────────────┬───────────────────────────┘
                 ▼
   CloudNativePG · RabbitMQ · weitere Operatoren
```

Ein Operator lässt sich anbinden, wenn er drei Dinge mitbringt: eine CRD für
Instanzen, Credentials als Secret und ein Statusfeld für Readiness.

## Quickstart

Ohne Cluster, für einen Blick auf den Katalog:

```bash
go build -o broker .
DEFINITIONS_DIR=./definitions \
BROKER_AUTH_USER=dev BROKER_AUTH_PASSWORD=dev \
./broker &

curl -s -u dev:dev -H 'X-Broker-API-Version: 2.17' \
  http://localhost:8080/v2/catalog | jq '.services[].name'
```

Mit Cluster, gegen echte Operatoren — die Entwicklungsplattform im Nachbarrepo
`korifi-platform` baut alles Nötige deklarativ auf:

```bash
cd ../korifi-platform
make up          # Cluster, Abhaengigkeiten, Korifi, Buildpacks
make services    # Backing-Service-Operatoren
make broker      # Image bauen, in kind laden, per Helm ausrollen
make register    # bei Korifi registrieren

cf marketplace
cf create-service cnpg-postgresql small meine-db
cf create-service-key meine-db k1
```

Die State-CRDs müssen vor dem ersten Start installiert sein:

```bash
kubectl apply -f deploy/crds/
```

Ausführlich in
[docs/de/how-to/local-development.md](docs/de/how-to/local-development.md).

## Verifikationsstand

**Alle folgenden Nachweise wurden gegen Korifi v0.18.0 auf kind aufgenommen**,
also gegen die Entwicklungsplattform. Sie belegen, dass der beschriebene Ablauf
dort wirklich lief — nicht, dass er auf einer Zielplattform läuft. Gegen
produktives Cloud Foundry, TAS oder einen externen Marketplace gibt es bisher
**keinen** Durchlauf.

### Lebenszyklus (24.08.2026)

| Nachweis | Ergebnis |
|---|---|
| OSB-2.17-Lebenszyklus über HTTP | Integrationstest deckt catalog → provision → last_operation → bind → unbind → deprovision ab |
| Live gegen Korifi auf kind | Registrierung, Marketplace, `cf create-service` |
| Generic Engine Ende zu Ende | `cf create-service cnpg-postgresql large my-real-pg` erzeugte einen echten CloudNativePG-Cluster (3 Instanzen, 10Gi); `psql` im Pod antwortete, Credentials aus dem Operator-Secret |
| Neustart-Persistenz | Instanzen und Bindings überleben Kill und Rescheduling |

### Service Binding Specification (02.09.2026)

Gegen den echten RabbitMQ-Cluster-Operator, dessen CRD den
Provisioned-Service-Duck-Type dokumentiert:

| Nachweis | Ergebnis |
|---|---|
| Secret-Name aus `status.binding.name` | Operator meldete `osb-<id>-default-user`, der Broker nutzte ihn ohne Namenstemplate |
| Zielform per `mapping` | Binding enthält genau `host, password, port, provider, type, uri, username` — `default_user.conf` und `connection_string` bleiben draußen |
| Spec-konformes Secret | Typ `servicebinding.io/rabbitmq`, Labels für Instanz und Binding, `OwnerReference` auf den `RabbitmqCluster` |
| Aufräumen beim Unbind | `cf delete-service-key` entfernt das projizierte Secret |

### State Store (02.09.2026)

| Nachweis | Ergebnis |
|---|---|
| Konformität gegen den CRD-Store | 24 von 24 im kind-Cluster gegen echtes RBAC, über das Helm-Chart ausgerollt |
| Sichtbarkeit | `kubectl get osbi` und `osbb` zeigen Instanz und Binding mit Service, Plan und Ready |
| Credentials getrennt | nicht im Binding-CR, sondern in einem Secret mit `OwnerReference` darauf |
| Neustart-Persistenz | Instanz, Binding und Credentials überleben das Löschen des Pods |
| Kontext vollständig | `platform`, `spaceGuid` und `organizationGuid` werden abgebildet |

### TLS und mTLS (02.09.2026)

| Nachweis | Ergebnis |
|---|---|
| Konformität über HTTPS mit Client-Zertifikat | 24 von 24 — und 24 von 24 auch mit dem Client-Zertifikat allein, ohne Basic Auth |
| Zertifikatsrotation ohne Neustart | TLS-Secret im laufenden Pod getauscht: die ausgelieferte Seriennummer wechselt, `restartCount` bleibt 0 |
| Registrierung über `https://` | `CFServiceBroker` wird ready bei `trustInsecureServiceBrokers=false` — die Plattform hat das Zertifikat geprüft |
| Voller Lebenszyklus über HTTPS | `cf create-service` bis `cf delete-service` einschließlich echter Credentials |
| mTLS-Autorisierung | ein von derselben CA signiertes, aber nicht gelistetes Client-Zertifikat bekommt 401 |
| Probes bleiben offen | `/healthz` ohne Client-Zertifikat erreichbar |

## Dokumentation

| Wenn du wissen willst … | lies |
|---|---|
| wofür der Broker gebaut wird | [target-platforms.md](docs/de/target-platforms.md) |
| wie die Schichten zusammenhängen | [architecture.md](docs/de/architecture.md) |
| wie man einen Service beschreibt | [service-definitions.md](docs/de/service-definitions.md) |
| wie man einen neuen Operator anbindet | [how-to/add-a-service.md](docs/de/how-to/add-a-service.md) |
| wie man lokal entwickelt und testet | [how-to/local-development.md](docs/de/how-to/local-development.md) |
| warum etwas nicht funktioniert | [how-to/debugging.md](docs/de/how-to/debugging.md) |
| welche Endpunkte es gibt und wo abgewichen wird | [reference/osb-api.md](docs/de/reference/osb-api.md) |
| welche Schalter es gibt | [reference/configuration.md](docs/de/reference/configuration.md) |
| welche Minen liegen | [known-issues.md](docs/de/known-issues.md) |
| warum etwas so entschieden wurde | [docs/de/adr/](docs/de/adr/0001-kubernetes-as-state-store.md) |
| wie man beiträgt | [CONTRIBUTING.de.md](CONTRIBUTING.de.md) |

Maschinenlesbar und im Binary eingebettet:
[`docs/openapi.yaml`](docs/openapi.yaml) und
[`schemas/service-definition.schema.json`](schemas/service-definition.schema.json),
zur Laufzeit unter `/openapi.yaml` und
`/schemas/service-definition.schema.json` abrufbar, ohne Authentifizierung.

## Mitgelieferte Definitionen

| Datei | Operator | Zustand |
|---|---|---|
| `cnpg-postgresql.yaml` | CloudNativePG | Ende zu Ende verifiziert |
| `rabbitmq-cluster.yaml` | RabbitMQ Cluster Operator | Ende zu Ende verifiziert |
| `redis-standalone.yaml` | Redis Operator | Secret wird nicht vom Operator erzeugt |
| `seaweedfs-s3.yaml` | SeaweedFS | nicht durchgemessen |
| `valkey-cluster.yaml` | Hyperspike Valkey | Operator erzeugt kein Credential-Secret |
| `redpanda-cluster.yaml` | Redpanda | dito |
| `minio-objectstorage.yaml` | MinIO | DEPRECATED |

Vier von sieben können nicht binden — nicht wegen der Definitionen, sondern
weil die Operatoren das Dreier-Muster nicht erfüllen. Siehe
[service-definitions.md](docs/de/service-definitions.md).

## Lizenz

MIT — siehe [LICENSE](LICENSE).
