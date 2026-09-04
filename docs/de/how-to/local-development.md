# Lokal entwickeln

> [English](../../en/how-to/local-development.md) · Führende Fassung: deutsch

## Das Wichtigste zuerst

**Die Testsuite braucht keinen Cluster.** Alles, was mit Kubernetes spricht,
läuft in den Tests gegen den Fake-Client von controller-runtime. Ein `git clone`
und ein Go-Toolchain reichen, um die vollständige Suite zu fahren.

```bash
git clone https://github.com/cyrano-janus/osb-broker-go
cd osb-broker-go
go test ./... -count=1
```

Ein Cluster wird erst gebraucht, wenn gegen echte Operatoren geprüft werden soll.

## Voraussetzungen

| Werkzeug | Wofür |
|---|---|
| Go (siehe `go.mod`) | bauen und testen |
| Docker | Image bauen |
| kind, kubectl, helm | nur für den Durchlauf gegen echte Operatoren |
| `cf` CLI | nur für den Durchlauf über Cloud Foundry |

## Die Schleife

```bash
go vet ./...                 # zuerst, faengt mehr als man denkt
go test ./... -count=1       # -count=1 umgeht den Test-Cache
go build ./...
```

Genau diese drei Schritte fährt auch das CI-Gate `L1`, dort zusätzlich mit
`-race`. Wer sie lokal grün hat, hat die erste Stufe hinter sich.

Einzelne Bereiche:

```bash
go test ./internal/definition/ -run TestBind -v
go test ./internal/handlers/ -count=1
go test ./internal/docs/            # der Waechter ueber diese Dokumentation
```

## Die Wächter, die mitreden

Diese Tests scheitern bei Änderungen, die man nicht als Teständerung erkennt.
Wer sie kennt, spart sich das Rätselraten:

| Test | Verlangt |
|---|---|
| `internal/definition/schema_sync_test.go` | jedes JSON-Tag der Definitionstypen steht in `schemas/service-definition.schema.json` |
| `internal/definition/catalog_test.go` | jede Datei in `definitions/` parst, und drei benannte Definitionen sind vorhanden |
| `internal/handlers/docs_sync_test.go` | `docs/openapi.yaml` und das Schema sind byte-identisch mit den eingebetteten Kopien unter `internal/handlers/docs/` |
| `internal/apis/v1alpha1/crd_schema_test.go` | jedes Go-Feld der State-Typen steht im CRD-Manifest |
| `internal/docs/sync_test.go` | `docs/de` und `docs/en` sind strukturgleich, keine toten Verweise, und kein Dokument erzählt seinen eigenen Werdegang |

**Wer `docs/openapi.yaml` ändert, muss die Kopie mitziehen** — kopieren, nicht
eine der beiden bearbeiten:

```bash
cp docs/openapi.yaml internal/handlers/docs/openapi.yaml
cp schemas/service-definition.schema.json internal/handlers/docs/service-definition.schema.json
```

## Ohne Cluster starten

```bash
go build -o broker .
DEFINITIONS_DIR=./definitions \
BROKER_AUTH_USER=dev BROKER_AUTH_PASSWORD=dev \
./broker
```

Der Zustand liegt dann im Speicher und ist beim nächsten Start weg — für einen
Blick auf den Katalog reicht das:

```bash
curl -s -u dev:dev -H 'X-Broker-API-Version: 2.17' \
  http://localhost:8080/v2/catalog | jq '.services[].name'
```

Provision wird ohne Cluster fehlschlagen, sobald die Engine ein CR anlegen will.
Das ist erwartetes Verhalten, kein Fehler.

## Gegen echte Operatoren

Dafür gibt es die Entwicklungsplattform im Nachbarrepo `korifi-platform`. Sie
baut einen kind-Cluster mit Korifi, den Operatoren und dem Broker deklarativ auf.
Beschrieben ist sie dort; hier nur, was für den Broker relevant ist:

```bash
cd ../korifi-platform
make up                 # Cluster, Abhaengigkeiten, Korifi, Buildpacks
make services           # die Backing-Service-Operatoren
make broker             # Image bauen, in kind laden, per Helm ausrollen
make register           # bei Korifi registrieren
```

Der Dev-Loop danach:

```bash
make dev-broker         # go test, Image bauen, in kind laden, Rollout
make broker-catalog     # OSB-Katalog direkt abfragen, ohne Cloud Foundry
```

**`make broker-image` fährt `go test ./...` als Vorbedingung.** Ein Image mit
roter Suite entsteht gar nicht erst.

Zwei Dinge, die dabei überraschen:

- Das Image wird **in den kind-Node geladen**, nicht in eine Registry gepusht.
  Deshalb `pullPolicy: IfNotPresent` — sonst sucht Kubernetes im Netz.
- Der Broker liest die Definitionen **beim Start**. Ein Definitionswechsel wirkt
  erst nach einem Neustart des Pods; `make broker-deploy` erzwingt ihn.

**Denk daran, dass Korifi nur die Entwicklungsplattform ist.** Was dort
durchgeht, ist noch kein Nachweis für ein Zielsystem — siehe
[target-platforms.md](../target-platforms.md).

## Die Konformitätssuite

`cmd/osb-checker` prüft den Broker gegen OSB 2.17 und ist das Gate, an dem sich
Änderungen an der HTTP-Schicht messen lassen.

```bash
go build -o /tmp/osb-checker ./cmd/osb-checker/
/tmp/osb-checker --url http://localhost:8080 --user dev --pass dev
```

Über HTTPS mit Client-Zertifikat:

```bash
/tmp/osb-checker \
  --url https://localhost:8443 \
  --user dev --pass dev \
  --ca-cert ca.crt --client-cert client.crt --client-key client.key
```

Der Rückgabewert ist die Zahl der Fehlschläge. Die Ausgabe ist für GitHub
Actions annotiert.

**Welchen Service die Suite prüft, entscheidet über den Aussagewert.** Ohne
Vorgabe nimmt sie den ersten Service, der **kein** Demo-Angebot ist — also eine
ServiceDefinition, und damit die Engine. Gezielt wählen geht so:

```bash
/tmp/osb-checker --url http://localhost:8080 --user dev --pass dev \
  --service-id f48a9e21-cnpg-0000-0000-000000000001 \
  --plan-id plan-small-0000-0000-000000000001
```

`--plan-id` darf entfallen, dann nimmt sie den ersten Plan des Service. Eine
`--service-id`, die nicht im Katalog steht, ist ein Fehlschlag und kein stiller
Rückfall — sonst prüfte die CI klaglos etwas anderes als gemeint. Welcher
Service gewählt wurde, steht als erste `PASS`-Zeile im Bericht.

## Zwei Checker, zwei Rollen

Neben dem eingebauten gibt es ein eigenstaendiges Werkzeug,
`github.com/cyrano-janus/osb-checker`. Die Rollen sind festgelegt, damit sie
nicht auseinanderdriften:

| Werkzeug | Rolle | Laeuft |
|---|---|---|
| `cmd/osb-checker` | schnelles Gate, blockierend | L2 und L2b, jeder Push |
| standalone `osb-checker` | Voll-Audit, unabhaengige Gegenprobe | L2, berichtend |

**Widersprechen sich beide, gewinnt die Spezifikation, nicht das Werkzeug.** Ein
Widerspruch ist ein Befund fuer [known-issues.md](../known-issues.md), kein
Grund, eine der beiden Suiten anzupassen.

Der standalone-Lauf braucht in der CI ein Token, weil das Repo privat ist:
Repository-Secret `OSB_CHECKER_TOKEN`. Fehlt es, entfaellt der Schritt und der
Gate bleibt vollstaendig — nur ohne Gegenprobe.

Lokal:

```bash
git clone git@github.com:cyrano-janus/osb-checker
cd osb-checker && cp config.yaml configs/config.yaml
# configs/ ist gitignoriert: dort stehen echte Zugangsdaten
go build -o osb-checker . && ./osb-checker -f configs/config.yaml
```

## Konventionen

- **Sprache:** Kommentare, Commit-Nachrichten und neue Testnamen auf Deutsch.
  Bezeichner und Feldnamen bleiben englisch.
- **Begründungen gehören in die Datei, nicht in die Commit-Nachricht.** Jede
  nicht offensichtliche Zeile trägt ein *Warum* als Kommentar. Der Bestand macht
  das durchgängig — beim Lesen von `crdstate.go` oder `config.go` sieht man es.
- **Tests zuerst**, wo es geht. Das Verhältnis von Produktiv- zu Testcode liegt
  bei ungefähr eins zu eins, und das soll so bleiben.
- **Neue Felder brauchen einen Schema-Eintrag**, sonst scheitert
  `schema_sync_test.go`.
