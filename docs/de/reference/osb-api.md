# OSB-API: Umfang und Abweichungen

> [English](../../en/reference/osb-api.md) · Führende Fassung: deutsch

**Maschinenlesbare Quelle ist `docs/openapi.yaml`.** Dieses Dokument nennt den
Umfang und **wo der Broker von OSB 2.17 abweicht**. Die Abweichungen fallen auf
der Entwicklungsplattform nicht auf; ihre Wirkung je Zielplattform steht in
[target-platforms.md](../target-platforms.md).

## Endpunkte

Alle `/v2`-Routen liegen hinter der Authentifizierung und der
`X-Broker-API-Version`-Prüfung.

| Methode und Pfad | Verhalten |
|---|---|
| `GET /v2/catalog` | genau `engine.Catalog()`. Ohne geladene Definitionen eine leere Liste |
| `PUT /v2/service_instances/:id` | Provision. `202` mit `operation`; ohne `?accepts_incomplete=true` **422** `AsyncRequired`; bei bekannter Instanz `200`, bei abweichenden Parametern `409`; unbekannter Service oder Plan `400`. Prüft `allowedParameters` |
| `PATCH /v2/service_instances/:id` | Update. Prüft `allowedParameters`, rendert neu, schreibt nur bei echter Änderung; `200`. Ein Wechsel auf einen anderen Plan ist **422** `PlanChangeNotSupported`, solange die Definition `planUpdateable` nicht setzt |
| `DELETE /v2/service_instances/:id` | Deprovision. `410` bei unbekannter Instanz, `409` bei bestehenden Bindings. `service_id` darf fehlen, dann kommt der Service aus dem Datensatz |
| `GET /v2/service_instances/:id` | aus dem Zustandsspeicher; `404` bei unbekannter Instanz |
| `GET /v2/service_instances/:id/last_operation` | Zustand aus dem CR des Operators. `service_id` darf fehlen, dann kommt der Service aus dem Datensatz. `410` bei unbekannter Instanz, `failed` wenn der Datensatz da ist und das Objekt fehlt |
| `PUT …/service_bindings/:bid` | Bind; `201`, bei bekanntem Binding `200` |
| `DELETE …/service_bindings/:bid` | Unbind; `200`, bei unbekannter Binding `410` |
| `GET …/service_bindings/:bid` | aus dem Zustandsspeicher; `404` bei unbekannter Binding oder fremder Instanz |
| `GET …/service_bindings/:bid/last_operation` | `succeeded` für eine bekannte Binding, `410` für eine unbekannte. Bind ist synchron, es gibt keinen laufenden Vorgang |

Ohne Authentifizierung erreichbar, absichtlich:

| Pfad | Zweck |
|---|---|
| `GET /healthz` | Liveness und Readiness. Vor der Auth registriert, damit ein Probe kein Zertifikat braucht |
| `GET /metrics` | Prometheus. Die Metrics-Middleware hängt **vor** der Auth, damit auch `401` gezählt wird |
| `GET /openapi.yaml` | die Spezifikation, in die Binärdatei einkompiliert |
| `GET /schemas/service-definition.schema.json` | das Definitionsschema, ebenso |

## Abweichungen von OSB 2.17

### `dashboard_url` ist eine Konstante

Jede Instanz bekommt `https://dashboard.example.com/instances/<id>`. Für Cloud
Foundry folgenlos, für einen Marketplace, der den Link anbietet, nicht.

## Was korrekt ist

Damit das Bild nicht schief wird — diese Punkte sind konform und geprüft:

- Die Katalogstruktur, inklusive Plänen, Tags und `bindable`.
- **Die Zusagen des Katalogs decken sich mit dem Verhalten.** `free` je Plan
  steht immer da, auch als `false` — fehlt es, liest OSB `true`.
  `plan_updateable` stammt aus der Definition und ist ohne Angabe `false`; ein
  nicht zugesagter Wechsel ist `422`. `instances_retrievable` und
  `bindings_retrievable` sind fest zugesagt, weil die GET-Endpunkte für jede
  Definition registriert sind. `maximum_polling_duration` je Plan ist die
  Bereitschaftsfrist des Brokers, damit die Plattform nicht länger fragt, als
  er antwortet.
- **`metadata` je Angebot und je Plan** geht unverändert in den Katalog — der
  Anzeigeblock, den ein Marktplatz rendert.
- **Plan-Schemas** (`schemas.service_instance.create/update.parameters`): jeder
  Plan veröffentlicht, welche Parameter er annimmt — abgeleitet aus
  `allowedParameters` und `parameterLimits`, also aus dem, was der Broker
  ohnehin durchsetzt.
- Die Aushandlung über `X-Broker-API-Version`: der Header ist **Pflicht**, eine
  fremde Hauptversion ist `412 Precondition Failed`. Eine neuere Nebenversion
  wird bedient — die Plattform nennt, was sie zu sprechen bereit ist, und ein
  Broker, der weniger kann, antwortet mit dem, was er kann. Frei bleiben
  `/healthz`, `/metrics`, `/openapi.yaml` und `/schemas`.
- `409 Conflict`, wenn dieselbe `instance_id` mit abweichendem Service, Plan
  oder abweichenden Parametern wiederholt wird.
- Jede Fehlerantwort trägt `error` und `description`.
- Idempotentes Provision und Bind, soweit der Zustand bekannt ist.
- `410 Gone` beim Deprovision einer unbekannten Instanz.
- Ein Pfad ohne Rückfallebene: eine unbekannte `service_id` ist `400`, kein
  stiller Wechsel in eine zweite Implementierung.
- Statuscodes aus Fehlerwerten statt aus Fehlertexten — ein unbekannter Plan
  ist auch auf einem `DELETE` `400` und nicht `410`.
- Die Trennung von authentifizierten und freien Pfaden.
- Basic Auth mit konstantzeitigem Vergleich; mTLS gleichrangig daneben.
- Der Lebenszyklus Ende zu Ende gegen echte Operatoren — CloudNativePG und
  RabbitMQ, jeweils auf der Entwicklungsplattform.
- Benutzerparameter: `allowedParameters` je Plan, geprüft auf `PUT` und
  `PATCH`; `plan_id` ist im `PATCH` optional; ein wiederholtes `PUT` mit
  abweichenden Parametern ist `409`.

Eine Festlegung, die OSB 2.17 offenlässt: **beim `PATCH` werden Parameter
verschmolzen**, nicht ersetzt. Gesendete Schlüssel überschreiben die
gespeicherten, ungenannte bleiben stehen — damit meldet
`GET /v2/service_instances` denselben Satz, unter dem die Instanz tatsächlich
läuft, auch wenn die Plattform nur das Geänderte schickt. Begründung in
[ADR 0007](../adr/0007-user-parameters.md).

Die Konformitätssuite `cmd/osb-gate` läuft in der CI zweimal: einmal über
HTTP gegen CloudNativePG, einmal über HTTPS mit Client-Zertifikat gegen das
Helm-Chart. Der eigenständige Checker läuft als blockierende Gegenprobe daneben.
