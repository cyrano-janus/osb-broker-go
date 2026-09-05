# Architektur

> [English](../en/architecture.md) · Führende Fassung: deutsch

Der Broker ist ein einzelner Prozess, der die OSB-API 2.17 spricht und daraus
Kubernetes-Objekte macht. Was pro Service zu tun ist, steht nicht im Code,
sondern in einer YAML-Datei — siehe
[service-definitions.md](service-definitions.md).

## Der Weg eines Requests

```
  Cloud Foundry / TAS / OSB-Client
            │  HTTPS, Basic Auth oder mTLS
            ▼
  ┌─────────────────────────────────────────────────────────┐
  │ internal/server    http.Server, TLS, Zertifikats-Reload  │
  │ internal/auth      Authenticator-Kette (basic, mtls)     │
  ├─────────────────────────────────────────────────────────┤
  │ internal/handlers  gin-Router, OSB-Endpunkte             │
  │                                                          │
  │        definitionFor(service_id)                         │
  │             ├── trifft zu ──▶ Engine                     │
  │             └── trifft nicht zu ──▶ 400 BadRequest       │
  ├──────────────────────┬──────────────────────────────────┤
  │ internal/definition  │ internal/broker                   │
  │ DIE ENGINE           │ Zustandsspeicher                  │
  │ YAML ─▶ CR           │ wer existiert, wer gebunden ist   │
  └──────────┬───────────┴───────────┬──────────────────────┘
             │                       │
             ▼                       ▼
   Operator-CRs                OSBServiceInstance
   (Cluster, RabbitmqCluster)  OSBServiceBinding
   + deren Credential-Secrets  + Credentials-Secret
             │                       │
             └───────────┬───────────┘
                         ▼
                    Kubernetes
```

**Es gibt einen Pfad, und das ist das Erste, was man verstehen muss.** Jeder
Handler löst über `definitionFor` die ServiceDefinition zur `service_id` auf
(`internal/handlers/definition_instances.go`):

```go
func (h *Handlers) definitionFor(serviceID string) (*definition.ServiceDefinition, error) {
	if serviceID == "" {
		return nil, fmt.Errorf("%w: service_id is required", definition.ErrServiceUnknown)
	}
	if h.engine == nil || h.engine.Engine == nil {
		return nil, fmt.Errorf("%w: no service definitions are loaded", definition.ErrServiceUnknown)
	}
	return h.engine.Engine.DefinitionByServiceID(serviceID)
}
```

Kennt die Engine den Service nicht, ist das `ErrServiceUnknown` und damit
`400` — es gibt keine Rückfallebene, in die ein Request fallen könnte, und der
Katalog ist genau das, was die Engine kennt. Warum das so entschieden wurde und
was vorher an der Stelle stand, steht in
[ADR 0003](adr/0003-replace-http-layer.md).

## Pakete und Größen

**6.419 Zeilen Produktivcode, 6.281 Zeilen Tests** — das Verhältnis ist
ungefähr eins zu eins.

| Paket | Zeilen | Aufgabe |
|---|---|---|
| `internal/definition` | 1.586 | **die Engine**: ServiceDefinition laden, Template rendern, CRs anwenden, Readiness auswerten, Credentials formen |
| `internal/broker` | 1.026 | Zustandsspeicher: Zugang (`broker.go`, 100) und der CRD-State-Store (`crdstate.go`, 486) |
| `internal/handlers` | 1.265 | gin-Router, OSB-Endpunkte, Auth-Middleware, Logging, Metriken |
| `internal/config` | 411 | Umgebungsvariablen zu einer validierten Struktur, Fail-Fast |
| `internal/apis/v1alpha1` | 309 | Go-Typen der State-CRDs |
| `internal/server` | 301 | `http.Server`, TLS, Zertifikats-Hot-Reload, Signalbehandlung |
| `internal/auth` | 299 | Authenticator-Kette, unabhängig von gin |
| `internal/migrate` | 208 | übernimmt Zustand aus einer ConfigMap im Format `state.json` |
| `main.go` | 139 | Verdrahtung, sonst nichts |
| `cmd/osb-checker` | 797 | Konformitätssuite, CI-Gate |
| `cmd/osb-state-migrate` | 66 | Werkzeug um `internal/migrate` |

## Die Schichten im Einzelnen

### `internal/server` — der Listener

Ein `http.Server` mit gesetzten Timeouts, Graceful Shutdown auf SIGTERM und
einem `CertReloader`, der Zertifikat, Schlüssel und Client-CA turnusmäßig neu
liest.

**Warum Polling und kein inotify:** Kubernetes blendet ein Secret über einen
atomar getauschten `..data`-Symlink ein. Ein inotify-Watch auf dem Blattpfad
verstummt nach dem ersten solchen Tausch. Der Reloader vergleicht stattdessen
SHA-256-Summen; scheitert ein Reload, bleibt das vorherige Material gültig und
es wird nur geloggt. Begründung in
[ADR 0004](adr/0004-tls-and-mtls-no-oauth2.md).

### `internal/auth` — die Kette

`Authenticator` mit einem dreiwertigen Fehlervertrag: `nil` (erfolgreich),
`ErrNoCredentials` (die Kette macht weiter), `ErrInvalidCredentials` (die Kette
merkt es sich und macht trotzdem weiter). Erst am Ende entscheidet sie.

Die Kette spricht `net/http`, nicht gin — sie ist bewusst
Framework-unabhängig, damit ein Austausch der HTTP-Schicht sie nicht mitreißt.
Basic-Auth vergleicht SHA-256-Digests mit `subtle.ConstantTimeCompare`. Die
Fehlerantwort ist für alle Fehlerarten identisch: welche Methode gescheitert
ist, würde einem nicht authentifizierten Aufrufer verraten, welche Methoden
überhaupt aktiv sind.

### `internal/handlers` — die HTTP-Schicht

Die Reihenfolge der Middleware ist tragend und im Code kommentiert:

```
gin.New()
  → Recovery
  → strukturiertes Logging          (eine JSON-Zeile je Request)
  → GET /healthz                    ← vor der Auth registriert, also frei
  → GET /openapi.yaml, /schemas/…   ← ebenfalls frei
  → GET /metrics + Metrics-Middleware  ← Middleware VOR der Auth, damit 401 gezählt wird
  → Auth-Middleware                 ← ab hier authentifiziert
  → API-Versions-Middleware
  → die /v2-Routen
```

Welche Endpunkte es gibt und wo sie von OSB 2.17 abweichen, steht in
[reference/osb-api.md](reference/osb-api.md).

**Fehlerabbildung** (`errors.go`): der HTTP-Status wird aus dem *Text* des
Fehlers geraten — `strings.Contains` auf „has existing bindings", „not found"
und Ähnliches. Zentral und damit an einer Stelle, aber falsch: jeder
DELETE-Fehler mit „not found" im Text wird `410`, auch „service not found".

**Correlation-ID:** jeder Request bekommt eine, entweder aus dem eingehenden
`X-Correlation-ID`-Header oder neu erzeugt, und gibt sie im Antwort-Header
zurück. Sie steht in jeder Logzeile — der Einstieg für
[how-to/debugging.md](how-to/debugging.md).

### `internal/definition` — die Engine

Der Teil, der trägt. Hier passiert die eigentliche Arbeit, und hier steckt der
Grund, warum ein neuer Service ohne Code auskommt.

| Datei | Aufgabe |
|---|---|
| `definition.go` | Typen, `Parse`, `Validate`, Parameter-Allowlist |
| `render.go` | `SanitizeInstanceName`, Template-Daten, `RenderProvision`, `SplitManifests` |
| `operator.go` | CR anwenden, löschen, lesen; Secrets; Multi-Doc-Dekodierung |
| `engine.go` | Orchestrierung, `InstanceRegistry`, `Catalog()` |
| `readiness.go` | Readiness über gjson, Credential-Extraktion |
| `servicebinding.go` | CNCF Service Binding Specification |
| `update.go` | Plan-Wechsel und No-Op-Erkennung |
| `load.go` | Verzeichnis einlesen, sortiert |

**Provision, Schritt für Schritt:**

1. `DefinitionByServiceID(service_id)`, dann `PlanByID(plan_id)`.
2. `RenderProvision` füllt `{{ .safeName }}`, `{{ .instanceID }}` und
   `{{ .plan.* }}` in das Template.
3. `ApplyManifestRefs` trennt das Ergebnis an `\n---`, dekodiert jedes Dokument,
   ergänzt fehlendes `apiVersion`, `kind` und `namespace` aus der Definition und
   legt an oder aktualisiert. Ein Dokument ohne `metadata.name` ist ein harter
   Fehler.
4. Die angelegten Objekte werden als `ObjectRef` (Gruppe, Version, Art,
   Namespace, Name) am Datensatz vermerkt — das ist die Buchführung, aus der
   Deprovision später weiß, was es zu löschen gibt.

**Bricht das Provision ab, wird zurückgerollt.** Was schon angelegt ist, wird
gelöscht — auch dann, wenn erst das Schreiben des Datensatzes scheitert. Beides
hinterließe sonst Objekte, auf die kein Datensatz zeigt: der Operator betriebe
eine Instanz, die es für den Broker nie gab, und kein Deprovision fände sie je
wieder. Der Grund des Abbruchs gewinnt in der Meldung; scheitert auch das
Aufräumen, wird das angehängt statt ihn zu ersetzen.

**Deprovision** arbeitet diese Buchführung in drei Stufen ab: erst die
`AppliedRefs` (Multi-Doc, jede mit eigener Art), dann `AppliedObjects` (nur
Namen, Art aus der Definition), zuletzt der Rückfall auf ein einzelnes CR unter
`safeName`. Die Stufen fangen Datensätze ab, die nicht alle drei Felder
tragen.

**Update** rendert mit der neuen wirksamen Konfiguration neu — Plan und
verschmolzene Benutzerparameter — und vergleicht dann, bevor es schreibt. Der Grund steht im Code: auch ein Schreibvorgang, der nichts
ändert, erhöht die `resourceVersion` und weckt die Reconcile-Schleife des
Operators. Der Vergleich normalisiert beide Seiten über JSON, weil dieselbe Zahl
je nach Weg als `int64` oder `float64` ankommt.

Der Datensatz wird auch dann nachgeführt, wenn das Manifest gleich bleibt: ein
Planwechsel ohne Wirkung auf das Manifest und ein Parameter, den das Template
nicht liest, ändern den Zustand der Instanz, nicht aber ihre Objekte.

**Readiness** ist **gjson**, nicht JSONPath. Der Pfad läuft über das ganze CR,
nicht nur über `status`, ein führender Punkt wird abgeschnitten, und ein nicht
vorhandener Pfad heißt *noch nicht bereit* — nie *Fehler*.

Aus *noch nicht bereit* wird *gescheitert*, wenn die Frist abgelaufen ist:
`timeoutSeconds` wird ab `creationTimestamp` des CR gemessen, ohne Angabe gilt
der Schemawert 600, ein negativer Wert schaltet die Frist ab. Der Zeitstempel
kommt vom API-Server und überlebt einen Neustart des Brokers — eine Uhr im
Prozess täte das nicht. Die Meldung nennt weiter den Grund aus der
Readiness-Prüfung: das Zeitlimit sagt, *dass* es hängt, der Grund sagt,
*woran*.

### `internal/broker` — zwei Dinge in einem Paket

Das Paket hat eine Aufgabe: die Buchführung. Welche Instanzen und Bindings der
Broker kennt — nicht, wie die Dienste hergestellt werden.

- **`crdstate.go` (486 Zeilen) ist der Zustandsspeicher.** Je
  Datensatz ein `OSBServiceInstance` beziehungsweise `OSBServiceBinding`,
  Schreibvorgänge mit `RetryOnConflict`, Credentials in einem eigenen Secret mit
  `OwnerReference`. Begründung in
  [ADR 0001](adr/0001-kubernetes-as-state-store.md).
- **`broker.go` (100 Zeilen) ist der Zugang dazu** — lesen, schreiben,
  vergessen, und die zwei OSB-Antworten `GET instance` und `GET binding`.

Objektnamen leitet der Store aus der OSB-ID ab, solange diese ein gültiges
DNS-1123-Label bis 63 Zeichen ist — bei Cloud Foundry immer der Fall, weil dort
UUIDs kommen. Sonst `osb-` plus gekürzter SHA-256. Die echte ID steht immer in
`spec.id` und wird bei jedem Lesen gegengeprüft, damit eine Hash-Kollision nicht
den falschen Datensatz liefert.

## Wo die Grenze verläuft

Die Entscheidung lautete: **Engine behalten, HTTP-Schicht ersetzen.** Die
Grenze läuft zwischen den Schichten, nicht quer durch sie:

| Teil | Zeilen | Urteil |
|---|---|---|
| `internal/definition` | 1.586 | trägt |
| `internal/broker/crdstate.go` und Umfeld | ~700 | trägt |
| `internal/config`, `server`, `auth` | 1.011 | trägt |
| `internal/apis/v1alpha1` | 309 | trägt |
| `cmd/osb-checker` | 797 | trägt |
| Logging, Metriken, Docs-Endpunkte | ~290 | trägt, entkoppelte Querschnitte |

Dass die Engine trägt, ist belegt: zwei Operatoren mit unterschiedlichen
CRD-Gruppen, Condition-Typen und Credential-Layouts laufen über sie, ohne dass
`internal/definition` etwas je Service wüsste. Ersetzt wurde die Schicht
darüber: ein Pfad statt zwei, ein Katalog aus Definitionen statt aus Demo-Daten
und typisierte Fehler statt `strings.Contains`. Die Entscheidung dazu ist
[ADR 0003](adr/0003-replace-http-layer.md).
