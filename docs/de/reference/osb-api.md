# OSB-API: Umfang und Abweichungen

> [English](../../en/reference/osb-api.md) · Führende Fassung: deutsch

**Maschinenlesbare Quelle ist `docs/openapi.yaml`.** Dieses Dokument nennt den
Umfang und, wichtiger, **wo der Broker von OSB 2.17 abweicht**. Die
Abweichungen sind der Grund, warum es existiert: mehrere davon fallen auf der
Entwicklungsplattform nicht auf und blockieren auf einer Zielplattform. Welche
das sind, steht in [target-platforms.md](../target-platforms.md).

## Endpunkte

Alle `/v2`-Routen liegen hinter der Authentifizierung und der
`X-Broker-API-Version`-Prüfung.

| Methode und Pfad | Verhalten |
|---|---|
| `GET /v2/catalog` | Vereinigung aus dem statischen Demo-Katalog und `engine.Catalog()` |
| `PUT /v2/service_instances/:id` | Provision. Definitions-Pfad oder Legacy-Pfad; `201`, bei bekannter Instanz `200` |
| `PATCH /v2/service_instances/:id` | Plan-Wechsel. Prüft `allowedParameters`, rendert neu, schreibt nur bei echter Änderung; `200` |
| `DELETE /v2/service_instances/:id` | Deprovision. `410` bei unbekannter Instanz, `409` bei bestehenden Bindings (nur Legacy-Pfad) |
| `GET /v2/service_instances/:id` | **Immer** über den Legacy-Pfad, also über den State Store |
| `GET /v2/service_instances/:id/last_operation` | Definitions-Pfad nur mit `?service_id=`, sonst hart `succeeded` |
| `PUT …/service_bindings/:bid` | Bind; `201`, bei bekanntem Binding `200` |
| `DELETE …/service_bindings/:bid` | Unbind |
| `GET …/service_bindings/:bid` | **Immer** über den State Store |
| `GET …/service_bindings/:bid/last_operation` | hart `succeeded` |

Ohne Authentifizierung erreichbar, absichtlich:

| Pfad | Zweck |
|---|---|
| `GET /healthz` | Liveness und Readiness. Vor der Auth registriert, damit ein Probe kein Zertifikat braucht |
| `GET /metrics` | Prometheus. Die Metrics-Middleware hängt **vor** der Auth, damit auch `401` gezählt wird |
| `GET /openapi.yaml` | die Spezifikation, in die Binärdatei einkompiliert |
| `GET /schemas/service-definition.schema.json` | das Definitionsschema, ebenso |

## Abweichungen von OSB 2.17

### Provision antwortet immer synchron

OSB überträgt `accepts_incomplete` als **Query-Parameter**. Der Broker
modelliert es als Feld im Request-Body (`internal/broker/types.go:24`) und liest
es entsprechend nie:

```go
AcceptsIncomplete bool `json:"accepts_incomplete"`
```

Der Zweig in `internal/handlers/service_instances.go:42` ist damit unerreichbar,
`StatusAccepted` kommt im Repo kein einziges Mal vor. Der gesamte
`last_operation`-Apparat existiert, wird aber nie angesprochen.

**Wirkung:** Der Broker meldet „fertig", sobald das CR angelegt ist — nicht,
wenn der Service bereit ist. Bei CloudNativePG liegen dazwischen Minuten. Die
Plattform bindet gegen ein Secret, das der Operator noch gar nicht geschrieben
hat.

### Bindings des Definitions-Pfads werden nicht persistiert

`bindDefinition` ruft die Engine und gibt die Credentials zurück. Es ruft nie
`state.PutBinding`. Drei Folgen:

- `GET …/service_bindings/:bid` läuft immer über den State Store und liefert
  daher **404** für jeden Service, der über eine Definition läuft.
- Ein wiederholtes Bind antwortet `201` statt `200`, weil nichts bekannt ist.
- Die `409`-Prüfung „Instanz hat noch Bindings" im Deprovision kann für
  Definitions-Services nie zuschlagen.

### `last_operation` für Bindings ist eine Konstante

`GetLastBindingOperation` gibt unabhängig von allem `succeeded` zurück, auch für
unbekannte IDs.

### Benutzerparameter erreichen das Template nicht

`Engine.ProvisionInstance` nimmt ein `parameters`-Argument entgegen und
verwendet es nicht; `RenderProvision` setzt nur `InstanceID`, `SafeName` und
`Plan`. `cf create-service -c '{...}'` bleibt damit wirkungslos.

### `allowedParameters` wird nur beim Update geprüft

`UpdateServiceInstance` ruft `ValidatePlanParamsForService`,
`ProvisionServiceInstance` nicht. Beim Provision werden beliebige Parameter
angenommen und danach verworfen. Das ist die unangenehmere Variante von
Punkt vier: kein Fehler, nur Wirkungslosigkeit.

### Der HTTP-Status wird aus Fehlertexten geraten

`internal/handlers/errors.go` entscheidet mit `strings.Contains`:

| Text im Fehler | Status |
|---|---|
| `has existing bindings` | 409 |
| `already exists with different` | 409 |
| `instance not found` | 404 |
| `not found` | 400 |
| sonst | 500 |

Anschließend überschreibt `respondOSBError` jeden **DELETE**-Fehler mit „not
found" im Text auf `410 Gone` — auch „service not found", wo `400` richtig wäre.

### Zwei Demo-Services im Katalog

`internal/store` liefert einen fest verdrahteten Katalog mit `service-1`
(`example-service`) und `service-2` (`database-service`), der bei jedem
`GET /v2/catalog` den Definitions-Services vorangestellt wird. Einen Schalter
dagegen gibt es nicht.

Das hat einen zweiten, unangenehmeren Effekt: die eigene Konformitätssuite
`cmd/osb-checker` nimmt in `pickService` den **ersten** Treffer aus dem Katalog —
und das ist immer `service-1`. Die Suite prüft damit den Legacy-Pfad, nicht die
Engine. Deshalb ist die fehlende Binding-Persistenz nie aufgefallen.

### `dashboard_url` ist eine Konstante

Beide Pfade setzen `https://dashboard.example.com/instances/<id>`. Für Cloud
Foundry folgenlos, für einen Marketplace, der den Link anbietet, nicht.

## Was korrekt ist

Damit das Bild nicht schief wird — diese Punkte sind konform und geprüft:

- Die Katalogstruktur, inklusive Plänen, Tags und `bindable`.
- Die Aushandlung über `X-Broker-API-Version`.
- Idempotentes Provision und Bind, soweit der Zustand bekannt ist.
- `410 Gone` beim Deprovision einer unbekannten Instanz.
- Die Trennung von authentifizierten und freien Pfaden.
- Basic Auth mit konstantzeitigem Vergleich; mTLS gleichrangig daneben.
- Der Lebenszyklus Ende zu Ende gegen echte Operatoren — CloudNativePG und
  RabbitMQ, jeweils auf der Entwicklungsplattform.

Die Konformitätssuite `cmd/osb-checker` fährt 24 Prüfungen und läuft in der CI
zweimal: einmal über HTTP gegen CloudNativePG, einmal über HTTPS mit
Client-Zertifikat gegen das Helm-Chart.
