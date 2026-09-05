# OSB Broker — generischer Service Broker für Kubernetes-Operatoren

[![Go](https://img.shields.io/badge/go-1.26-blue.svg)](https://golang.org)
[![OSB API](https://img.shields.io/badge/OSB%20API-2.17-green.svg)](https://github.com/openservicebrokerapi/servicebroker/blob/v2.17/spec.md)
[![Lizenz](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

> [English](README.md) · Führende Fassung: deutsch

Ein gehärteter Prozess macht Kubernetes-Operatoren über die
Open-Service-Broker-API verfügbar. Ein neuer Service ist eine YAML-Datei, kein
neuer Broker: kein Code je Service, keine externe Datenbank.

**Angeboten werden drei Dienste** — PostgreSQL, RabbitMQ und SeaweedFS —, davon
zwei Ende zu Ende gegen einen laufenden Operator belegt. Der Katalog wächst
entlang eines benannten Bedarfs, nicht entlang des Möglichen: ein Dienst kommt
hinzu, wenn ihn eine konkrete Last verlangt **und** sein Operator das
Dreier-Muster erfüllt. Warum das so ist, steht in
[ADR 0008](docs/de/adr/0008-depth-over-breadth.md).

Die Arbeit geht deshalb in die Tiefe: ein Plan **erzwingt** seine Größen
(`parameterLimits`, auch als OSB-Plan-Schema im Katalog), ein Produktionsplan
verliert seine Daten nicht auf einen Tastendruck (`retainOnDeprovision`), und
der Bestand ist nach Angebot und Plan aufgeschlüsselt.

**Eine geänderte Definition erreicht auch bestehende Instanzen.** Der Broker
liest Definitionen beim Start; `RECONCILE_INTERVAL` gleicht die laufenden
periodisch dagegen ab. Er löscht nie und legt nie an — was er nicht auflösen
kann, meldet er, statt es aufzuräumen.

**Der Katalog sagt, was der Broker kann — und nur das.** Er ist das Einzige,
was ein Marktplatz vom Broker sieht, bevor er ihn benutzt: jedes Angebot und
jeder Plan trägt seinen Anzeigeblock, jeder Plan sagt ausdrücklich, ob er
kostenlos ist, und jede Zusage wird gegen das Verhalten gehalten. Ein
Planwechsel, den keine Definition zusagt, ist `422` statt einer stillen
Änderung. Was noch fehlt — Sicherung, Point-in-Time-Recovery, Upgrades
bestehender Instanzen — steht ungeschönt in
[target-platforms.md](docs/de/target-platforms.md).

## Wofür das gebaut wird

| Rolle | System |
|---|---|
| **Zielplattform** | produktives Cloud Foundry |
| **Zielplattform** | Tanzu TAS |
| **Zielplattform** | externe Marketplaces mit OSB-Anbindung |
| **Entwicklungsplattform** | Korifi auf kind — Testgerät, kein Zielsystem |

Geprüft ist der Broker gegen Korifi v0.18.0 auf kind, also gegen die
Entwicklungsplattform; gegen produktives Cloud Foundry, TAS oder einen externen
Marketplace gibt es keinen Durchlauf. Mehrere bekannte Abweichungen von OSB 2.17
bleiben auf der Entwicklungsplattform folgenlos und sind auf einem Zielsystem
Blocker. Die Nachweise im Einzelnen und was daraus folgt:
[docs/de/target-platforms.md](docs/de/target-platforms.md).

## Der Ansatz

Eine Engine, N ServiceDefinitions: eine Codebasis, Konfiguration statt Code.

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
| `seaweedfs-s3.yaml` | SeaweedFS | Readiness nur gegen das CRD-Schema geprüft |

**Vier weitere liegen unter `definitions/unsupported/` und werden nicht
geladen** — Redis und Redpanda, weil ihre Lizenz die Bereitstellung als managed
Service untersagt, MinIO, weil es AGPLv3 und seit Ende 2025 unmaintained ist,
Valkey, weil sein Operator kein Credentials-Secret anlegt. Die Begründung je
Fall steht in
[definitions/unsupported/README.md](definitions/unsupported/README.md); was
einen Operator brauchbar macht, in
[service-definitions.md](docs/de/service-definitions.md).

## Lizenz

MIT — siehe [LICENSE](LICENSE).
