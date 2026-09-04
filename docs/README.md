# Documentation index · Dokumentationsverzeichnis

The documentation exists in two languages. **German is the leading version** —
new text is written there first, the English one follows. A guard test
(`internal/docs/sync_test.go`) checks that both trees carry the same files, the
same outline and no dead links.

Die Dokumentation liegt zweisprachig vor. **Deutsch ist die führende Fassung** —
neuer Text entsteht dort zuerst, die englische zieht nach. Ein Wächtertest
(`internal/docs/sync_test.go`) prüft, dass beide Bäume dieselben Dateien, dieselbe
Gliederung und keine toten Verweise haben.

| Topic · Thema | English | Deutsch |
|---|---|---|
| What the broker is built for · Wofür der Broker gebaut wird | [target-platforms](en/target-platforms.md) | [Zielplattformen](de/target-platforms.md) |
| How the layers fit together · Wie die Schichten zusammenhängen | [architecture](en/architecture.md) | [Architektur](de/architecture.md) |
| Describing a service · Einen Service beschreiben | [service-definitions](en/service-definitions.md) | [ServiceDefinitions](de/service-definitions.md) |
| Adding a service · Einen Service anbinden | [how-to/add-a-service](en/how-to/add-a-service.md) | [how-to/add-a-service](de/how-to/add-a-service.md) |
| Developing locally · Lokal entwickeln | [how-to/local-development](en/how-to/local-development.md) | [how-to/local-development](de/how-to/local-development.md) |
| Troubleshooting · Fehlersuche | [how-to/debugging](en/how-to/debugging.md) | [how-to/debugging](de/how-to/debugging.md) |
| Korifi certificate trust · Korifi-Trust einrichten | [how-to/korifi-trust](en/how-to/korifi-trust.md) | [how-to/korifi-trust](de/how-to/korifi-trust.md) |
| API scope and deviations · API-Umfang und Abweichungen | [reference/osb-api](en/reference/osb-api.md) | [reference/osb-api](de/reference/osb-api.md) |
| Configuration · Konfiguration | [reference/configuration](en/reference/configuration.md) | [reference/configuration](de/reference/configuration.md) |
| Known issues · Bekannte Probleme | [known-issues](en/known-issues.md) | [known-issues](de/known-issues.md) |

## Architecture decisions · Architekturentscheidungen

| # | Decision · Entscheidung | Status | English | Deutsch |
|---|---|---|---|---|
| 0001 | Kubernetes as the only state store · Kubernetes als einziger Zustandsspeicher | accepted | [en](en/adr/0001-kubernetes-as-state-store.md) | [de](de/adr/0001-kubernetes-as-state-store.md) |
| 0002 | Declarative ServiceDefinitions · Deklarative ServiceDefinitions | accepted | [en](en/adr/0002-declarative-service-definitions.md) | [de](de/adr/0002-declarative-service-definitions.md) |
| 0003 | Replace the HTTP layer · HTTP-Schicht ersetzen | **proposed** | [en](en/adr/0003-replace-http-layer.md) | [de](de/adr/0003-replace-http-layer.md) |
| 0004 | TLS and mTLS, no OAuth2 · TLS und mTLS, kein OAuth2 | accepted | [en](en/adr/0004-tls-and-mtls-no-oauth2.md) | [de](de/adr/0004-tls-and-mtls-no-oauth2.md) |
| 0005 | CNCF Service Binding Specification | accepted | [en](en/adr/0005-cncf-service-binding-spec.md) | [de](de/adr/0005-cncf-service-binding-spec.md) |
| 0006 | The OSB API is the only coupling · Die OSB-API ist die einzige Kopplung | accepted | [en](en/adr/0006-platform-independence.md) | [de](de/adr/0006-platform-independence.md) |

## Machine-readable sources · Maschinenlesbare Quellen

Prose refers to these; it does not duplicate them. They are embedded into the
binary and served without authentication.

Die Prosa verweist darauf, statt sie zu kopieren. Beide sind in die Binärdatei
einkompiliert und ohne Authentifizierung abrufbar.

| File · Datei | Served at · Ausgeliefert unter |
|---|---|
| [`openapi.yaml`](openapi.yaml) | `/openapi.yaml` |
| [`../schemas/service-definition.schema.json`](../schemas/service-definition.schema.json) | `/schemas/service-definition.schema.json` |

## Where else documentation lives · Wo sonst noch Dokumentation liegt

| Level · Ebene | Location · Ort |
|---|---|
| What a package does · Was ein Paket tut | godoc, `doc.go` |
| Why a line reads this way · Warum eine Zeile so lautet | comment on the line · Kommentar an der Zeile |
| Measurement log with all findings · Messprotokoll mit allen Befunden | `korifi-platform/FINDINGS.md` |
| Development platform · Entwicklungsplattform | `korifi-platform/README.md` |
