# ADR 0003: HTTP-Schicht ersetzen, Engine behalten

> [English](../../en/adr/0003-replace-http-layer.md) · Führende Fassung: deutsch

**Status:** **angenommen** · **Betrifft:** `internal/handlers`, `internal/broker/broker.go`, `internal/store`

## Kontext

Mehrere der offenen Befunde gingen auf eine gemeinsame Ursache zurück: **es gab
zwei vollständige Broker-Implementierungen im selben Prozess.**

Jeder Handler verzweigte einzeln über `resolveDefinition`. Lieferte es eine
Definition, lief der Request durch die Engine; lieferte es `nil` — auch im
Fehlerfall —, fiel er stumm auf `internal/broker/broker.go` zurück, einen
zweiten Broker mit eigenen Instanz- und Binding-Maps und einem Demo-Katalog aus
`internal/store`.

Daraus folgte unmittelbar:

- Der Demo-Katalog erschien in jedem Produktivkatalog, ohne Schalter dagegen.
- Die eigene Konformitätssuite prüfte den Fallback-Pfad, weil sie den ersten
  Service aus dem Katalog nahm — deshalb war die fehlende Binding-Persistenz
  nie aufgefallen.
- `GET instance` und `GET binding` liefen **immer** über den Fallback-Pfad, auch
  für Definitions-Services.
- `last_operation` für Bindings war eine Konstante.
- Der HTTP-Status wurde aus Fehlertexten geraten: jeder Fehler mit „not found"
  wurde auf einem `DELETE` zu `410 Gone`, ein unbekannter Plan also genauso wie
  eine bereits gelöschte Instanz.

## Abgrenzung

Der Zustandsspeicher ist von dieser Entscheidung **nicht** betroffen. Er liegt
in eigenen Ressourcenarten mit `RetryOnConflict` und trägt
([ADR 0001](0001-kubernetes-as-state-store.md)). Ersetzt wurde allein die
HTTP-Schicht und der Fallback-Broker dahinter.

## Entscheidung

**Die HTTP-Schicht ist ersetzt, die Engine bleibt.** Konkret:

- **Ein Pfad statt zwei.** Der Fallback-Broker und der Demo-Katalog sind
  ersatzlos entfallen; `internal/store` gibt es nicht mehr. Ein Service, der
  keiner Definition entspricht, ist ein Fehler — `400` mit
  `ErrServiceUnknown` — und keine stille Rückfallebene.
- **Der Katalog ist genau das, was die Engine kennt.** Ohne geladene
  Definitionen antwortet `GET /v2/catalog` mit einer leeren Liste, nicht mit
  erfundenen Services.
- **Typisierte Fehler statt `strings.Contains`.** `internal/definition/errors.go`
  führt `ErrServiceUnknown`, `ErrPlanUnknown`, `ErrResourceGone` und
  `ErrParameterNotAllowed` unter der Oberkategorie `ErrNotFound`. Die
  HTTP-Schicht bildet Werte ab, keine Formulierungen.
- **`last_operation` für Bindings ist eine echte Abfrage.** Für eine bekannte
  Binding `succeeded`, für eine unbekannte `410 Gone`.

**Kein Neuschreiben des Repos.** Die Engine ist der teure Teil, und sie trägt.

## Warum sich das gerechnet hat

Das Argument war nicht die Eleganz, sondern die Kosten der Alternative. Die
offenen Befunde einzeln zu beheben, war **innerhalb** der Doppelpfad-Struktur
bereits ein Durchstich durch sechs Dateien — man zahlte fast den Preis des
Umbaus und behielte die Struktur, die das Problem erzeugt.

Dass die Engine den Umbau trägt, ist belegt: zwei Operatoren mit
unterschiedlichen CRD-Gruppen, Condition-Typen und Credential-Layouts laufen
über sie, ohne dass `internal/definition` etwas je Service wüsste. Der Umbau
hat sie nicht angefasst.

## Umfang

| Teil | Urteil |
|---|---|
| `internal/definition` | unverändert |
| `internal/broker/crdstate.go` und Umfeld | unverändert |
| `internal/config`, `server`, `auth` | unverändert |
| `internal/apis/v1alpha1` | unverändert |
| `cmd/osb-checker` | unverändert |
| Doppelpfad in den Handlern | ersetzt |
| `internal/broker/broker.go` | auf den Zustandszugang reduziert |
| `internal/store` | gelöscht |

`internal/broker/broker.go` schrumpfte von 437 auf 100 Zeilen, die
Handler-Schicht von rund 580 Zeilen Verzweigung auf einen Pfad.

## Konsequenzen

- Die Konformitätssuite prüft die Engine, weil es nur noch sie gibt. Der
  standalone-Checker braucht keine `skip_services`-Liste mehr.
- `internal/auth`, `internal/server` und `internal/config` haben den Wechsel
  unverändert überlebt. Genau dafür sind sie so geschnitten.
- Der Umbau ist ein Bruch nach innen, nicht nach außen: die OSB-API bleibt, was
  sie ist — siehe [ADR 0006](0006-platform-independence.md).
- Wer einen Service anbieten will, schreibt eine ServiceDefinition. Es gibt
  keinen zweiten Weg mehr, und das ist der Punkt.
