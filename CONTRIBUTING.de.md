# Mitarbeiten

> [English](CONTRIBUTING.md) · Führende Fassung: deutsch

## Der kürzeste Weg zu einem grünen Stand

```bash
go vet ./...
go test ./... -count=1
go build ./...
```

Genau diese drei Schritte fährt das CI-Gate `L1`, dort zusätzlich mit `-race`.
**Ein Cluster wird dafür nicht gebraucht** — alles, was mit Kubernetes spricht,
läuft in den Tests gegen den Fake-Client von controller-runtime.

Wie man gegen echte Operatoren prüft, steht in
[docs/de/how-to/local-development.md](docs/de/how-to/local-development.md).

## Die Wächtertests

Fünf Tests scheitern bei Änderungen, die man nicht als Teständerung erkennt.
Wer sie kennt, spart sich das Rätselraten:

| Test | Verlangt |
|---|---|
| `internal/definition/schema_sync_test.go` | jedes JSON-Tag der Definitionstypen steht in `schemas/service-definition.schema.json` |
| `internal/definition/catalog_test.go` | jede Datei in `definitions/` parst |
| `internal/handlers/docs_sync_test.go` | `docs/openapi.yaml` und das Schema sind byte-identisch mit den eingebetteten Kopien |
| `internal/apis/v1alpha1/crd_schema_test.go` | jedes Go-Feld der State-Typen steht im CRD-Manifest |
| `internal/docs/sync_test.go` | `docs/de` und `docs/en` sind strukturgleich, keine toten Verweise, und kein Dokument erzählt seinen eigenen Werdegang |

Jeder von ihnen sichert eine Kopplung, die sich sonst unbemerkt löst — und die
niemand beim Lesen nachprüft.

**Wer `docs/openapi.yaml` oder das Schema ändert, muss die Kopie mitziehen** —
kopieren, nicht eine der beiden Fassungen bearbeiten:

```bash
cp docs/openapi.yaml internal/handlers/docs/openapi.yaml
cp schemas/service-definition.schema.json internal/handlers/docs/service-definition.schema.json
```

## Sprache

- **Kommentare, Commit-Nachrichten und neue Testnamen: deutsch.**
- **Bezeichner, Feldnamen, Fehlermeldungen der OSB-Ebene: englisch.**
- Der Bestand ist gemischt. Er wird **nicht** rückwirkend vereinheitlicht — ein
  solcher Commit macht jede `git blame` unbrauchbar und bringt keinen Nutzen.
- Die Dokumentation liegt zweisprachig vor. **Deutsch ist die führende
  Fassung**: zuerst dort schreiben, dann die englische nachziehen. Der
  Strukturwächter meldet, wenn eine Seite fehlt.

## Begründungen gehören in die Datei

Die wichtigste Konvention dieses Repos. Jede nicht offensichtliche Zeile trägt
ein *Warum* als Kommentar — nicht in der Commit-Nachricht, wo es beim Lesen des
Codes niemand findet.

Wie das aussieht, zeigen `internal/broker/crdstate.go`, `internal/config/config.go`
und `internal/server/certreload.go`. Beispiel aus dem Bestand:

```go
// Bewusst nicht konfigurierbar: waere er es, waere es wieder eine Konvention
// und kein Standard.
var bindingSecretPath = []string{"status", "binding", "name"}
```

Eine Entscheidung, die über eine einzelne Zeile hinausgeht, gehört als
Architekturentscheidung nach `docs/de/adr/` — mit Kontext, Entscheidung,
Konsequenzen und Status.

## Tests

Das Verhältnis von Produktiv- zu Testcode liegt bei ungefähr eins zu eins, und
das soll so bleiben. Es gibt kein Mocking-Framework und keine Build-Tags; was
gebraucht wird, sind `testify`, `httptest` und der Fake-Client.

Zwei Muster aus dem Bestand, die sich bewährt haben:

- **Vertragssuiten** statt doppelter Tests. Beide Zustandsspeicher bestehen
  dieselbe Suite (`internal/broker/statestore_contract_test.go`).
- **Konfiguration über eine Lookup-Funktion** statt über die Prozessumgebung
  (`config.LoadFrom`). Tests verändern damit nie globalen Zustand.

## Eine neue ServiceDefinition

Der Weg steht in
[docs/de/how-to/add-a-service.md](docs/de/how-to/add-a-service.md).
Zwei Punkte, die dort besonders leicht schiefgehen:

- **Den Readiness-Pfad am lebenden Objekt ermitteln**, nicht aus der
  Operator-Dokumentation. Ein nicht auffindbarer gjson-Pfad bedeutet „noch nicht
  bereit", nie „falsch konfiguriert" — der Fehler ist unsichtbar.
- **`offering.id` und die Plan-IDs sind für immer.** Cloud Foundry speichert
  sie; eine Änderung macht bestehende Instanzen unauffindbar.

## Ein neues Feld im Schema

1. Feld am Go-Typ in `internal/definition/definition.go` ergänzen, mit JSON-Tag.
2. `schemas/service-definition.schema.json` nachziehen, sonst scheitert
   `schema_sync_test.go`.
3. Die eingebettete Kopie kopieren, sonst scheitert `docs_sync_test.go`.
4. Feld in [docs/de/service-definitions.md](docs/de/service-definitions.md) und
   in der englischen Fassung beschreiben.
5. Validierung ergänzen, wenn es Werte gibt, die keinen Sinn ergeben. Ein Fehler
   beim Laden ist besser als einer beim Kundenrequest.

## Commits

Deutsch, im Bestandsstil: eine knappe Betreffzeile, dann ein Absatz, der
**warum** sagt, nicht was.

```
docs: Mandantentrennung über Space-Namespaces dokumentieren (#3/#16/#7)

Ein eigener Abschnitt zu Namespaces: worauf abgebildet wird, dass beide von
der Spezifikation erlaubten Quellen für die Space-GUID ausgewertet werden,
warum der Namespace am Datensatz gespeichert werden muss statt ihn aus dem
Request abzuleiten, und was das für die RBAC bedeutet.
```

Zwischenstände werden committet, nicht angesammelt.

## Was vor einem größeren Umbau zu lesen ist

- [docs/de/architecture.md](docs/de/architecture.md) — wo die Grenze zwischen
  tragfähig und zu ersetzen verläuft.
- [docs/de/known-issues.md](docs/de/known-issues.md) — was bekannt offen ist,
  damit nichts doppelt gefunden wird.
- [docs/de/adr/0003-replace-http-layer.md](docs/de/adr/0003-replace-http-layer.md)
  — der vorgeschlagene, noch nicht entschiedene Umbau.
- [docs/de/target-platforms.md](docs/de/target-platforms.md) — woran „fertig"
  gemessen wird.
