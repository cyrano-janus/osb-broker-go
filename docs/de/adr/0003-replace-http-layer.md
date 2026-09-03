# ADR 0003: HTTP-Schicht ersetzen, Engine behalten

> [English](../../en/adr/0003-replace-http-layer.md) · Führende Fassung: deutsch

**Status:** **vorgeschlagen** — nicht entschieden · **Betrifft:** `internal/handlers`, `internal/broker/broker.go`, `internal/store`

> Dieses Dokument hält eine Empfehlung fest, keine getroffene Entscheidung. Es
> steht hier, damit die Begründung nachlesbar ist, wenn die Entscheidung ansteht.

## Kontext

Aus einer systematischen Lesung des Codes und aus zwei Lifecycle-Durchläufen
gegen echte Operatoren sind mehrere Befunde entstanden, die auf eine gemeinsame
Ursache zurückgehen: **es gibt zwei vollständige Broker-Implementierungen im
selben Prozess.**

Jeder Handler verzweigt einzeln über `resolveDefinition`
(`internal/handlers/definition_instances.go:17`). Liefert es eine Definition,
läuft der Request durch die Engine; liefert es `nil` — auch im Fehlerfall —,
fällt er stumm auf `internal/broker/broker.go` zurück, einen zweiten Broker mit
eigenen Instanz- und Binding-Maps und einem Fake-Katalog aus `internal/store`.

Daraus folgen unmittelbar:

- Der Demo-Katalog erscheint in jedem Produktivkatalog.
- Die eigene Konformitätssuite prüft den Legacy-Pfad, weil sie den ersten
  Service aus dem Katalog nimmt — deshalb ist die fehlende Binding-Persistenz
  nie aufgefallen.
- `GET instance` und `GET binding` laufen **immer** über den Legacy-Pfad, auch
  für Definitions-Services.
- Async ist nie angebunden: `accepts_incomplete` wird an der falschen Stelle
  gelesen, und der `last_operation`-Apparat läuft leer.
- Der HTTP-Status wird aus Fehlertexten geraten.

Die Einzelheiten stehen in [known-issues.md](../known-issues.md), die
Auswirkung je Zielplattform in [target-platforms.md](../target-platforms.md).
**Vier dieser Punkte sind Blocker für produktives Cloud Foundry und TAS**, und
alle vier liegen in derselben Schicht.

## Was sich seit der Formulierung geändert hat

Die ursprüngliche Empfehlung lautete „HTTP- **und** State-Schicht ersetzen".
Die State-Hälfte ist in Phase 5 umgesetzt: der ConfigMap-Store ist weg, an
seiner Stelle stehen eigene Ressourcenarten mit `RetryOnConflict` — siehe
[ADR 0001](0001-kubernetes-as-state-store.md). Was offen bleibt, ist die
HTTP-Schicht und der Legacy-Broker dahinter.

## Vorschlag

**Die HTTP-Schicht ersetzen, die Engine behalten.** Konkret:

- **Ein Pfad statt zwei.** Der Legacy-Broker und der Demo-Katalog entfallen
  ersatzlos. Ein Service, der keiner Definition entspricht, ist ein Fehler und
  keine stille Rückfallebene.
- **Echtes Async** über einen persistierten Operations-Datensatz:
  `accepts_incomplete` aus der Query lesen, `202 Accepted` mit `operation`
  antworten, `last_operation` gegen den echten Readiness-Zustand beantworten.
- **Bindings persistieren**, damit `GET binding` und die Idempotenz stimmen.
- **Typisierte Fehler** statt `strings.Contains` auf Fehlertexten.

**Kein Neuschreiben des Repos.** Die Engine ist der teure Teil, und sie trägt.

## Warum sich das rechnet

Das eigentliche Argument ist nicht die Eleganz, sondern die Kosten der
Alternative. Die offenen Befunde einzeln zu beheben, ist **innerhalb** der
Doppelpfad-Struktur bereits ein Durchstich durch sechs Dateien — man zahlte fast
den Preis des Umbaus und behielte die Struktur, die das Problem erzeugt.

Die Gegenprobe zur Engine-Hälfte ist gelaufen: der RabbitMQ-Durchlauf brachte
einen Operator mit anderer CRD-Gruppe, anderen Condition-Typen und anderem
Credential-Layout — und brauchte **keine einzige** Änderung an
`internal/definition`. Das war das offene Argument gegen eine Entscheidung auf
Basis eines einzigen Services. Es ist ausgeräumt.

## Umfang

Stand dieses Dokuments, 6.560 Zeilen Produktivcode:

| Teil | Zeilen | Urteil |
|---|---|---|
| `internal/definition` | 1.493 | behalten |
| `internal/broker/crdstate.go` und Umfeld | ~700 | behalten, Phase 5 |
| `internal/config`, `server`, `auth` | 1.011 | behalten, Phase 4.5 |
| `internal/apis/v1alpha1` | 309 | behalten |
| `cmd/osb-checker` | 658 | behalten |
| Logging, Metriken, Docs-Endpunkte | ~290 | behalten |
| Doppelpfad in den Handlern | ~580 | ersetzen |
| `internal/broker/broker.go` | 414 | ersetzen |
| `internal/store` | 131 | entfällt |

Rund 1.100 Zeilen zu ersetzen, gut 4.400 bleiben.

## Konsequenzen, wenn zugestimmt wird

- Die Konformitätssuite prüft danach die Engine, weil es nur noch sie gibt.
  Zu erwarten ist, dass dabei weitere Abweichungen sichtbar werden, die heute
  hinter dem Demo-Service verborgen sind.
- `internal/auth`, `internal/server` und `internal/config` sind bewusst
  framework-unabhängig geschrieben und überleben den Wechsel unverändert. Das
  war beim Bau von Phase 4.5 schon so gedacht.
- Der Umbau ist ein Bruch nach innen, nicht nach außen: die OSB-API bleibt, was
  sie ist — siehe [ADR 0006](0006-platform-independence.md).

## Konsequenzen, wenn abgelehnt wird

Die vier Blocker bleiben, und der Broker bleibt auf der Entwicklungsplattform
brauchbar und auf einer Zielplattform nicht einsetzbar. Das ist eine legitime
Entscheidung, solange sie bewusst getroffen wird.
