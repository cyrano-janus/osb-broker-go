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

`cmd/osb-gate` prüft den Broker gegen OSB 2.17 und ist das Gate, an dem sich
Änderungen an der HTTP-Schicht messen lassen. **Der Name sagt die Rolle, nicht
den Umfang:** die Prüfungen selbst kennen keine Service-ID und keinen
Katalogeintrag dieses Repos und gelten für jeden OSB-Broker. Zum *Gate* macht
es allein, wo es läuft — in der CI dieses Repos, blockierend, bei jedem Push.

```bash
go build -o /tmp/osb-gate ./cmd/osb-gate/
/tmp/osb-gate --url http://localhost:8080 --user dev --pass dev
```

Über HTTPS mit Client-Zertifikat:

```bash
/tmp/osb-gate \
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
/tmp/osb-gate --url http://localhost:8080 --user dev --pass dev \
  --service-id f48a9e21-cnpg-0000-0000-000000000001 \
  --plan-id plan-small-0000-0000-000000000001
```

`--plan-id` darf entfallen, dann nimmt sie den ersten Plan des Service. Eine
`--service-id`, die nicht im Katalog steht, ist ein Fehlschlag und kein stiller
Rückfall — sonst prüfte die CI klaglos etwas anderes als gemeint. Welcher
Service gewählt wurde, steht als erste `PASS`-Zeile im Bericht.

### Den Update-Pfad wirklich prüfen

`cf update-service -c '{...}'` schickt ein `PATCH`, das **nur Parameter** trägt
und kein `plan_id` — im OSB 2.17 ist das Feld dort optional. Die Prüfung
`update-parameters` deckt genau diese Form ab: die Anfrage darf nicht am
fehlenden `plan_id` scheitern, und was der Broker annimmt, muss
`GET /v2/service_instances` auch berichten.

Ohne Vorgabe sondiert sie mit einem erfundenen Schlüssel. Ein Broker mit
`allowedParameters` lehnt den zu Recht mit `400` ab — dann sagt die Prüfung
nichts und wird übersprungen. Einen erlaubten Schlüssel nennt man so:

```bash
/tmp/osb-gate --url http://localhost:8080 --user dev --pass dev \
  --service-id f48a9e21-cnpg-0000-0000-000000000001 \
  --plan-id plan-small-0000-0000-000000000001 \
  --update-parameter storageSize=2Gi
```

**Diese Prüfung existiert, weil die Entwicklungsplattform sie nicht ersetzen
kann.** Korifi reicht ein `cf update-service -c` gar nicht an den Broker weiter:
die CLI meldet „Update of service instance complete", und beim Broker kommt kein
`PATCH` an. Über die CLI geprüft ist der Pfad also ungeprüft — auf einem
Zielsystem fällt ein Bruch dann zuerst einem Kunden auf. In der
Entwicklungsplattform gibt es dafür `make conformance`, das genau diesen Lauf
gegen den ausgerollten Broker fährt.

## Zwei Checker, zwei Rollen

Neben dem eingebauten gibt es ein eigenstaendiges Werkzeug,
`github.com/cyrano-janus/osb-checker`. Die Rollen sind festgelegt, damit sie
nicht auseinanderdriften:

| Werkzeug | Was es ist | Prüft | Läuft |
|---|---|---|---|
| `cmd/osb-gate` | das Gate dieses Repos | **diesen** Broker bei jedem Push | L2 und L2b, blockierend |
| `osb-checker` | die unabhängige Zweitmeinung, eigenes Repo, öffentlich, MIT | **jeden** OSB-Broker | L2, blockierend |

Der Unterschied ist die **Rolle**, nicht die Fähigkeit. Beide prüfen dieselbe
Spezifikation; `osb-gate` ist an diesen Build gebunden und entscheidet, ob er
durchgeht, `osb-checker` ist ein Werkzeug für sich und gegen fremde Broker
einsetzbar.

**Widersprechen sich beide, gewinnt die Spezifikation, nicht das Werkzeug.** Ein
Widerspruch ist ein Befund fuer [known-issues.md](../known-issues.md), kein
Grund, eine der beiden Suiten anzupassen.

Beide Laeufe blockieren. Eine Gegenprobe, die nur berichtet, wird ueberlesen;
ein Werkzeug, dessen Urteil folgenlos bleibt, ist von einem kaputten nicht zu
unterscheiden.

**Beide belegen, dass ihre Prüfungen anschlagen können.** Ein Gate, dessen
Prüfungen wirkungslos sind, ist von einem grünen nicht zu unterscheiden — es
meldet dieselbe Farbe und sagt damit nichts. `osb-gate` führt den Beleg in
`checks/mockbroker_test.go`: ein konformer httptest-Broker muss null
Fehlschläge ergeben, jede der 31 Mutationen — genau eine verletzte Regel — muss
genau die zuständige Prüfung auslösen, und gegen einen geschlossenen Server darf
**nichts** bestehen. Die letzte Zusage ist die wichtigste: eine Negativprüfung,
die einen Transportfehler als „der Broker hat abgelehnt" liest, meldet einen
unerreichbaren Broker als konform.

```bash
go test ./cmd/osb-gate/checks/ -run TestMock -v
```

Stand: **31 Mutationen**, 37 Prüfzusagen gegen einen konformen Broker.

Zwei davon sind Gegenproben statt Verletzungen: eine nicht angemeldete
Abrufbarkeit und ein ehrlich verneinter Planwechsel dürfen **nicht**
fehlschlagen. Ohne sie wäre eine Prüfung eine Regel, die die Spezifikation
nicht kennt.

Wer eine Prüfung hinzufügt, fügt ihre Mutation dazu. Sonst ist die Prüfung
selbst ungeprüft.

Das Checker-Repo ist öffentlich; die CI klont es ohne Anmeldung und ohne
gepinnten Stand — die Gegenprobe soll den jeweils aktuellen Checker fahren. Der
Preis dafuer ist bekannt: ein Commit im Checker-Repo kann die CI dieses Repos
rot machen, ohne dass sich hier etwas geaendert hat. Genau dann soll jemand
hinsehen.

Lokal:

```bash
git clone https://github.com/cyrano-janus/osb-checker
cd osb-checker && cp config.yaml configs/config.yaml
# configs/ ist gitignoriert: dort stehen echte Zugangsdaten
go build -o osb-checker . && ./osb-checker -f configs/config.yaml
```

## Ein neues Feld an einer Definition

Der Weg ist kurz, aber es gibt fünf Stellen, und vier davon mahnt ein Test an,
wenn man sie vergisst:

1. **Der Go-Typ** in `internal/definition/definition.go` — `Offering` oder
   `Plan`.
2. **Das JSON-Schema** `schemas/service-definition.schema.json` — damit prüft
   ein Anwender seine Definition offline, bevor er sie ausrollt.
3. **Die eingebettete Kopie** `internal/handlers/docs/service-definition.schema.json`,
   die der Broker unter `/schemas/…` ausliefert. Ein `cp` genügt.
4. **Der Katalog** `internal/definition/engine.go` — aber nur, wenn das Feld
   nach außen gehört. Siehe unten.
5. **Diese Doku** — `service-definitions.md`, in beiden Sprachbäumen.

`TestSchema_PlanDecktDenGoTyp`, `TestSchema_OfferingDecktDenGoTyp` und ihre
Gegenstücke prüfen Schritt 1 und 2 **in beide Richtungen**: ein Go-Feld ohne
Schema-Eintrag fällt auf, und ein Schema-Schlüssel ohne Go-Feld auch — das wäre
eine Zusage an Anwender, die der Broker nicht einlöst.
`TestDocsSync_ServiceDefinitionSchemaMatchesEmbeddedCopy` prüft Schritt 3.

**Schritt 4 ist der, den man übersieht.** `Offering` und `Plan` beschreiben,
was in der YAML-Datei stehen darf; `CatalogEntry` und `CatalogPlan` beschreiben,
was über die Leitung geht. Das sind zwei Typen, und ein Feld, das nur im ersten
steht, wird gelesen und verschwindet. `free` war genau das: im Go-Typ, im
Schema, in der Doku — und nie im Katalog.

Kein Test kann diesen Schritt allgemein einfordern, weil nicht jedes
Definitionsfeld nach außen gehört: `readiness.statusJSONPath` etwa geht
niemanden außerhalb etwas an. **Die Frage lautet deshalb: soll eine Plattform
das wissen?** Wenn ja, gehört daneben ein Test in
`internal/handlers/catalog_promises_test.go`, der die Zusage gegen das
Verhalten hält — und, wo es sich über HTTP prüfen lässt, eine Prüfung in
`cmd/osb-gate`.

**Der Waagebalken dahinter:** eine Zusage, die der Broker nicht hält, scheitert
erst beim Anwender auf einem System, das hier niemand nachstellt. Eine
Fähigkeit, die er hat und nicht anmeldet, benutzt niemand. Beide Fehler sind
still, und beide fallen nur auf, wenn jemand sie gegeneinander hält.

`Plan` steckt nicht unter `definitions/` im Schema, sondern inline in
`offering.properties.plans.items` — er braucht deshalb seinen eigenen Wächter
und hat ihn. Für `Offering` gilt dasselbe.

## Das Chart bleibt mit dem Repo zusammen

`values-kind.yaml` bettet die Definitionen als Zeichenketten ein. Zwei Kopien
desselben Inhalts driften auseinander, sobald jemand nur eine anfasst — deshalb
ist die Datei **erzeugt**, nicht handgepflegt:

```bash
go test ./internal/chart/          # prüft
```

`internal/chart/sync_test.go` hält vier Zusagen: die eingebetteten Kopien sind
zeichengleich mit `definitions/`, kein Schlüssel kommt doppelt vor (der
YAML-Parser verschluckt das sonst), `rbac.operatorCRDs` deckt **genau** die
CRD-Gruppen ab, die die Definitionen anfassen — eine fehlende ist `403` beim
Provision, eine überflüssige ein clusterweites Recht zu viel —, und jeder Wert
unter `config` kommt als Umgebungsvariable im Pod an.

Dazu zwei Render-Prüfungen: jede mitgelieferte Wertedatei muss rendern, und die
Vorgaben müssen mit Ansage scheitern. Letzteres ist Absicht — der Aussteller des
Broker-Zertifikats ist standortabhängig, und ein `{{ required }}` ist die Art,
wie Helm „das musst du entscheiden" ausdrückt. Ein stilles Rendern mit leerem
Aussteller wäre schlimmer: das Deployment ginge durch und der Broker bekäme nie
ein Zertifikat.

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
