# Fehlersuche

> [English](../../en/how-to/debugging.md) · Führende Fassung: deutsch

Von außen nach innen: erst die API, dann der Zustand, dann die Objekte im
Cluster, zuletzt die Logs.

## Der erste Griff: der Katalog ohne Cloud Foundry

Die meisten Fehler, die wie ein Cloud-Foundry-Problem aussehen, sind keins.
Frage den Broker direkt:

```bash
cd ../korifi-platform && make broker-catalog
```

Das Skript zieht die CA aus dem TLS-Secret, die Zugangsdaten aus dem
Auth-Secret, öffnet einen Port-Forward und ruft `/v2/catalog` mit passendem
Servernamen auf — bewusst mit `--resolve` statt `-k`, denn ein Aufruf, der das
Zertifikat nicht prüft, prüft gar nichts.

| Ergebnis | Bedeutung |
|---|---|
| Katalog kommt, Service fehlt | Der Pod wurde nach der Definitionsänderung nicht neu gestartet |
| `401` | Zugangsdaten stimmen nicht |
| Verbindung abgewiesen | Der Pod läuft nicht oder lauscht auf einem anderen Port |
| Zertifikatsfehler | Das TLS-Secret passt nicht zum angefragten Namen |

## Der Zustand

Der Broker legt je Datensatz ein CR an. Das ist der Punkt gegenüber der früheren
ConfigMap: der Zustand ist mit `kubectl` sichtbar.

```bash
kubectl get osbi,osbb -A                       # Kurznamen fuer Instanzen und Bindings
kubectl get osbi -n osb-broker -o wide
kubectl describe osbi <name> -n osb-broker
```

Die echte OSB-ID steht in `spec.id`, nicht im Objektnamen — der Name ist daraus
abgeleitet und kann gehasht sein:

```bash
kubectl get osbi -n osb-broker \
  -o custom-columns='OBJEKT:.metadata.name,OSB-ID:.spec.id,NS:.spec.namespace,READY:.spec.ready'
```

Die Credentials eines Bindings liegen **nicht** im Binding-CR, sondern in einem
eigenen Secret daneben:

```bash
kubectl get secret <binding-objektname>-credentials -n osb-broker \
  -o jsonpath='{.data.credentials\.json}' | base64 -d | jq
```

**Ein Binding ohne Datensatz ist derzeit der Normalfall**, wenn der Service über
eine Definition läuft: der Definitions-Pfad persistiert Bindings nicht. `GET
binding` antwortet dann 404. Siehe
[reference/osb-api.md](../reference/osb-api.md).

## Die Objekte im Cluster

```bash
kubectl get clusters.postgresql.cnpg.io -A      # oder die Art des Operators
kubectl describe cluster <osb-name> -n <space-ns>
```

Instanzen landen im **Space-Namespace**, nicht im Broker-Namespace. Der
Objektname beginnt mit `osb-`.

**Der Readiness-Pfad ist die häufigste Fehlerquelle.** Prüfe ihn am lebenden
Objekt gegen den Wert in der Definition:

```bash
kubectl get cluster <osb-name> -n <ns> -o json | \
  jq '.status.conditions[] | {type, status}'
```

Steht dort kein Eintrag mit dem in `statusJSONPath` genannten Typ, meldet der
Broker ewig `in progress` — ein nicht auffindbarer gjson-Pfad bedeutet „noch
nicht bereit", nie „falsch konfiguriert". Das ist der Unterschied zwischen einem
Service, der hängt, und einer Definition, die falsch ist, und von außen sehen
beide gleich aus.

## Die Logs

Eine JSON-Zeile je Request. Der Einstieg ist die Correlation-ID:

```bash
kubectl logs -n osb-broker deploy/osb-broker -f | jq -c \
  '{t:.timestamp, cid:.correlation_id, m:.method, p:.path, s:.status, ms:.duration_ms, auth:.auth_method}'
```

Einen einzelnen Vorgang verfolgen:

```bash
kubectl logs -n osb-broker deploy/osb-broker | jq -c 'select(.correlation_id=="<id>")'
```

Die ID kommt aus dem eingehenden `X-Correlation-ID`-Header oder wird erzeugt,
und sie steht auch im Antwort-Header. Wer selbst ruft, kann sie vorgeben:

```bash
curl -H 'X-Correlation-ID: meine-suche-42' ...
```

**Die erste Logausgabe nach dem Start lesen.** `internal/config` meldet fünf
nicht fatale Zustände als Warnung: In-Memory-Speicher aktiv, keine
Authentifizierung, kein TLS, leere mTLS-Allowlist, heruntergestuftes
`MTLS_REQUIRE`. Jede davon erklärt später ein merkwürdiges Verhalten.

## Häufige Fehlerbilder

| Symptom | Wahrscheinliche Ursache |
|---|---|
| Neuer Service fehlt im Katalog | Pod nach Definitionsänderung nicht neu gestartet |
| `cf marketplace` leer, obwohl der Katalog stimmt | Pläne nicht sichtbar geschaltet |
| Provision endet mit 403 | `rbac.operatorCRDs` deckt die CRD nicht ab |
| Provision endet mit 500 beim ersten Mal | Rechte auf die State-CRDs fehlen, oder die CRDs sind nicht installiert |
| Instanz ewig „in progress" | Readiness-Pfad zeigt auf eine Condition, die es nicht gibt |
| `cf service-key` liefert 404 | Definitions-Bindings werden nicht persistiert |
| Binding enthält Konfigurationsdateien | `mapping` fehlt in der Definition |
| Instanz gilt sofort als fertig, Bind schlägt fehl | Provision antwortet synchron `201`; der Operator ist noch nicht so weit |
| `cf create-service -c '{…}'` wirkt nicht | Benutzerparameter erreichen das Template nie |
| Pod startet nicht, Fehler beim Laden | eine Datei in `definitions/` parst nicht |
| Zertifikatsdateien nicht lesbar | `fsGroup` fehlt; die Dateien liegen mit Modus `0440` |

Die dauerhaften unter diesen Punkten sind in
[known-issues.md](../known-issues.md) mit Codestelle beschrieben.

## Metriken

```bash
kubectl port-forward -n osb-broker deploy/osb-broker 9090:8443
curl -sk https://localhost:9090/metrics | grep '^osb_'
```

Neun Kollektoren, eigene Registry statt der Standardregistry. Nützlich sind
`osb_requests_total` nach Status und `osb_last_operation_state`.

**`osb_active_instances` und `osb_active_bindings` sind registriert, werden aber
nie gesetzt** und melden immer 0. Nicht darauf verlassen.

## Wenn die Plattform selbst verdächtig ist

Die Entwicklungsplattform hat zwei eigene Werkzeuge:

```bash
cd ../korifi-platform
make status      # Ist-Zustand auf einen Blick
make doctor      # Diagnose, wenn status etwas Rotes zeigt
```

`make doctor` prüft unter anderem Volume-Verweise auf nicht existierende Secrets
— das Fehlerbild, das nach einem `cf unbind-service` zurückbleibt und die App
beim nächsten Start hängen lässt.
