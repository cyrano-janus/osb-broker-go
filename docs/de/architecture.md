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
  │        resolveDefinition(service_id)                     │
  │             ├── trifft zu ──▶ Engine-Pfad                │
  │             └── trifft nicht zu ──▶ Fallback-Pfad        │
  ├──────────────────────┬──────────────────────────────────┤
  │ internal/definition  │ internal/broker                   │
  │ DIE ENGINE           │ Fallback-Broker + StateStore      │
  │ YAML ─▶ CR           │ internal/store: Demo-Katalog      │
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

**Der Verzweigungspunkt ist das Erste, was man verstehen muss.** Er steht in
`internal/handlers/definition_instances.go:17`:

```go
func (h *Handlers) resolveDefinition(serviceID string) (*definition.ServiceDefinition, error) {
	if h.engine == nil || h.engine.Engine == nil { return nil, nil }
	sd, err := h.engine.Engine.DefinitionByServiceID(serviceID)
	if err != nil { return nil, nil } // not a definition service -> legacy path
	return sd, nil
}
```

Liefert er eine Definition, läuft der Request durch die Engine. Liefert er
`nil` — auch im Fehlerfall —, fällt er **stumm** auf einen zweiten, vollständig
eigenen Broker in `internal/broker/broker.go` zurück. Beide Pfade existieren
nebeneinander, jeder Handler verzweigt einzeln. Der Code nennt den zweiten Pfad
`legacy path`; dieses Dokument nennt ihn nach seiner Funktion Fallback-Pfad.
Was daran hängt, steht in [known-issues.md](known-issues.md) und in
[ADR 0003](adr/0003-replace-http-layer.md).

## Pakete und Größen

**6.560 Zeilen Produktivcode, 6.497 Zeilen Tests** — das Verhältnis ist
ungefähr eins zu eins.

| Paket | Zeilen | Aufgabe |
|---|---|---|
| `internal/definition` | 1.493 | **die Engine**: ServiceDefinition laden, Template rendern, CRs anwenden, Readiness auswerten, Credentials formen |
| `internal/broker` | 1.284 | Fallback-Broker (`broker.go`, 414) **und** der CRD-State-Store (`crdstate.go`, 486) |
| `internal/handlers` | 1.253 | gin-Router, OSB-Endpunkte, Auth-Middleware, Logging, Metriken |
| `internal/config` | 411 | Umgebungsvariablen zu einer validierten Struktur, Fail-Fast |
| `internal/apis/v1alpha1` | 309 | Go-Typen der State-CRDs |
| `internal/server` | 301 | `http.Server`, TLS, Zertifikats-Hot-Reload, Signalbehandlung |
| `internal/auth` | 299 | Authenticator-Kette, unabhängig von gin |
| `internal/migrate` | 208 | übernimmt Zustand aus einer ConfigMap im Format `state.json` |
| `internal/store` | 131 | statischer Demo-Katalog |
| `main.go` | 135 | Verdrahtung, sonst nichts |
| `cmd/osb-checker` | 658 | Konformitätssuite, CI-Gate |
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

**Deprovision** arbeitet diese Buchführung in drei Stufen ab: erst die
`AppliedRefs` (Multi-Doc, jede mit eigener Art), dann `AppliedObjects` (nur
Namen, Art aus der Definition), zuletzt der Rückfall auf ein einzelnes CR unter
`safeName`. Die Stufen fangen Datensätze ab, die nicht alle drei Felder
tragen.

**Update** rendert mit den Parametern des neuen Plans neu und vergleicht dann,
bevor es schreibt. Der Grund steht im Code: auch ein Schreibvorgang, der nichts
ändert, erhöht die `resourceVersion` und weckt die Reconcile-Schleife des
Operators. Der Vergleich normalisiert beide Seiten über JSON, weil dieselbe Zahl
je nach Weg als `int64` oder `float64` ankommt.

**Readiness** ist **gjson**, nicht JSONPath. Der Pfad läuft über das ganze CR,
nicht nur über `status`, ein führender Punkt wird abgeschnitten, und ein nicht
vorhandener Pfad heißt *noch nicht bereit* — nie *Fehler*. Einen Zustand
`failed` gibt es in der Engine nicht, und `timeoutSeconds` wird gelesen, aber
nie durchgesetzt.

### `internal/broker` — zwei Dinge in einem Paket

Das Paket trägt zwei völlig verschiedene Aufgaben, und das ist der Kern der
Verwirrung beim ersten Lesen:

- **`crdstate.go` (486 Zeilen) ist der Zustandsspeicher.** Je
  Datensatz ein `OSBServiceInstance` beziehungsweise `OSBServiceBinding`,
  Schreibvorgänge mit `RetryOnConflict`, Credentials in einem eigenen Secret mit
  `OwnerReference`. Begründung in
  [ADR 0001](adr/0001-kubernetes-as-state-store.md).
- **`broker.go` (414 Zeilen) ist der Fallback-Broker** — eine zweite, vollständige
  OSB-Implementierung mit eigenem Katalog aus `internal/store`. Sie bedient
  alles, was `resolveDefinition` nicht erkennt, und ihre Demo-Services
  `service-1` und `service-2` erscheinen in jedem Katalog.

Objektnamen leitet der Store aus der OSB-ID ab, solange diese ein gültiges
DNS-1123-Label bis 63 Zeichen ist — bei Cloud Foundry immer der Fall, weil dort
UUIDs kommen. Sonst `osb-` plus gekürzter SHA-256. Die echte ID steht immer in
`spec.id` und wird bei jedem Lesen gegengeprüft, damit eine Hash-Kollision nicht
den falschen Datensatz liefert.

## Wo die Grenze verläuft

Der Vorschlag lautet: **Engine behalten, HTTP-Schicht ersetzen.** Die Grenze
läuft zwischen den Schichten, nicht quer durch sie:

| Teil | Zeilen | Urteil |
|---|---|---|
| `internal/definition` | 1.493 | trägt |
| `internal/broker/crdstate.go` und Umfeld | ~700 | trägt |
| `internal/config`, `server`, `auth` | 1.011 | trägt |
| `internal/apis/v1alpha1` | 309 | trägt |
| `cmd/osb-checker` | 658 | trägt |
| Logging, Metriken, Docs-Endpunkte | ~290 | trägt, entkoppelte Querschnitte |
| **Doppelpfad in den Handlern** | ~580 | zu ersetzen |
| **`broker.go` (Fallback-Broker)** | 414 | zu ersetzen |
| **`store.go` (Demo-Katalog)** | 131 | zu ersetzen |

Dass die Engine trägt, ist belegt: zwei Operatoren mit unterschiedlichen
CRD-Gruppen, Condition-Typen und Credential-Layouts laufen über sie, ohne dass
`internal/definition` etwas je Service wüsste. Zu ersetzen wären ein Pfad statt
zwei, echtes Async über einen persistierten Operations-Datensatz und typisierte
Fehler statt `strings.Contains`. Der Vorschlag dazu ist
[ADR 0003](adr/0003-replace-http-layer.md), Status `vorgeschlagen`.
