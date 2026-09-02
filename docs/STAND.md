# Arbeitsstand — 02.09.2026

Übergabe für die nächste Sitzung. Beide Repos haben einen sauberen Working
Tree; `osb-broker-go` ist mit GitHub synchron (`main` = `fc52f9b`).

## In einem Satz

Der Broker terminiert TLS selbst, hält seinen Zustand in eigenen CRDs,
liefert Bindings nach CNCF Service Binding Specification und legt
Backing-Ressourcen im Namespace des Cloud-Foundry-Space an — alles gegen
einen laufenden Korifi-Cluster nachgewiesen.

## Was in dieser Sitzung entstanden ist

30 Commits, ausgehend von `afec30e` (Phase 4.6, Stand 27.08.).

| Runde | Inhalt |
|---|---|
| **Phase 4.5** | TLS-Terminierung im Broker mit Hot-Reload bei Rotation, mTLS mit CN/SAN-Allowlist, echter `http.Server` mit Timeouts und Graceful Shutdown, eigenes CI-Gate (L2b) über HTTPS. **OAuth2 bewusst nicht gebaut** — der Broker soll ohne IdP betreibbar bleiben. |
| **Phase 5** | State Store von einer ConfigMap auf `OSBServiceInstance`/`OSBServiceBinding` umgestellt (harter Schnitt), Credentials in Secrets statt im Klartext, Migrationswerkzeug `cmd/osb-state-migrate`. |
| **Phase 6** | `provisionedService` liest den Secret-Namen aus `.status.binding.name`, `mapping` gibt die Zielform vor, `projectSecret` schreibt ein spec-konformes Secret für Kubernetes-Workloads. |
| **#3/#16/#7** | Instanzen landen im Space-Namespace statt gesammelt in `default` — Mandantentrennung bei den Backing-Ressourcen. |

Testzahl: **248**. Ohne eine einzige neue Go-Dependency — alles über
`crypto/tls`, `crypto/x509` und die vorhandene `text/template`-Engine.

## Zustand der Umgebung

Der kind-Cluster `korifi` läuft und ist vollständig auf dem neuen Stand:

- Broker über **HTTPS** registriert, Korifi validiert das Zertifikat
  (`trustInsecureServiceBrokers: false`) gegen die Plattform-CA aus
  `platform/15-pki/`
- App-Routen ebenfalls aus dieser CA — `curl` ohne `-k` funktioniert
- State in CRDs, `kubectl get osbi -n osb-broker`
- `make verify` im Plattform-Repo: **49 Checks, alle grün**

Der Cluster `prometheus` ist unangetastet geblieben, wie in der
Arbeitsanweisung gefordert.

## Befundstand

Von 28 Befunden: **12 gelöst, 1 umgangen, 15 offen.**

Gelöst in dieser Sitzung: #1, #2, #3, #7, #14, #16, #19, #23, #25, #26
(dazu #8 und #24 aus früheren Sitzungen).

## Empfehlung für morgen: #4 und #15

**Der Broker bricht einen Vertrag, den er selbst dokumentiert.** Provision
antwortet synchron mit `201`, obwohl der Async-Apparat samt `last_operation`
existiert. Ursache ist #15: `accepts_incomplete` wird an der falschen Stelle
gelesen. Bei CNPG dauert das Provisioning Minuten — der Broker behauptet
trotzdem sofort „fertig".

Das ist jetzt der teuerste offene Punkt, und #22 (die RabbitMQ-Definition
prüft eine Condition, die es nicht gibt) wird durch den Fix erst sichtbar,
gehört also in dieselbe Runde.

Danach in dieser Reihenfolge sinnvoll:

1. **#13/#20** — zwei vollständige Broker-Implementierungen nebeneinander,
   und die Konformitätssuite prüft ausgerechnet den Legacy-Pfad. Das ist der
   Grund, warum #28 nie auffiel.
2. **#28** — Definition-Bindings werden nicht persistiert, `GET binding`
   liefert 404. Hängt strukturell an #13.
3. **#5/#17** — nicht auflösbarer `host` in den Credentials, Benutzerparameter
   erreichen das Template nicht.

## Offene Entscheidungen

- **OAuth2** bleibt gestrichen. Die `Authenticator`-Schnittstelle in
  `internal/auth` ist der Einstiegspunkt, falls es doch kommt.
- **`korifi-platform` hat kein Remote.** Die Arbeit dort liegt nur lokal. Vor
  einem ersten Push gehört geprüft, dass `broker/values.local.yaml` mit dem
  echten Broker-Passwort wirklich ausgeschlossen bleibt.
- **Java-Referenzbroker** weiterhin bei null.

## Wieder einsteigen

```bash
cd "~/Claude DEV/korifi-platform"
make status                 # Ist-Zustand, read-only
make verify                 # 49 Checks
make dev-broker             # schneller Loop: test, build, load, rollout

cd "~/Claude DEV/osb-broker-go"
go test ./... -count=1
```

Läuft der Cluster nicht mehr: `make start` (nicht `make up` — das setzt neu
auf).
