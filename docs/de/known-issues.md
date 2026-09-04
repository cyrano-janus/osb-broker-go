# Bekannte Probleme

> [English](../en/known-issues.md) · Führende Fassung: deutsch

Diese Liste ist bewusst vollständig und bewusst unbeschönigt. Wer den Broker
weiterentwickelt, soll die Minen kennen, bevor er hineinläuft.

**Langfassung je Befund:** `korifi-platform/FINDINGS.md`. Dort steht das
Messprotokoll mit Beobachtung, verifizierter Ursache und Vorschlag, sortiert
nach Schwere und nach Quelle des Durchlaufs. Hier steht die Kurzform mit der
Codestelle — und, was dort fehlt, die Einordnung: **blockiert es eine
Zielplattform oder nur die Entwicklungsplattform?** Was das unterscheidet, steht
in [target-platforms.md](target-platforms.md).

## Funktionale Lücken

### Blocker für produktives Cloud Foundry und TAS

**Provision antwortet immer synchron.** `accepts_incomplete` ist in
`internal/broker/types.go:24` als Feld im Request-Body modelliert; OSB überträgt
es als Query-Parameter. Der Zweig in `internal/handlers/service_instances.go:42`
ist damit unerreichbar, `StatusAccepted` kommt im Repo nie vor. Der Broker meldet
„fertig", sobald das CR angelegt ist — bei CloudNativePG Minuten zu früh. Der
gesamte `last_operation`-Apparat existiert und läuft leer.
*FINDINGS #4 und #15.*

**Bindings des Definitions-Pfads werden nicht persistiert.** `bindDefinition`
ruft nie `state.PutBinding`. `GET …/service_bindings/:bid` läuft immer über den
State Store und liefert daher 404; `cf service-key` scheitert daran. Ein
wiederholtes Bind antwortet fälschlich `201`, und die 409-Prüfung „Instanz hat
noch Bindings" kann für Definitions-Services nie zuschlagen.
*FINDINGS #28.*

**`last_operation` für Bindings ist eine Konstante.**
`GetLastBindingOperation` gibt unabhängig von allem `succeeded` zurück, auch für
unbekannte IDs.

**Zwei Demo-Services im Produktivkatalog.** `internal/store` liefert einen fest
verdrahteten Katalog mit `service-1` und `service-2`, der jedem
`GET /v2/catalog` vorangestellt wird. Einen Schalter dagegen gibt es nicht.
*FINDINGS #9.*

### Funktional fehlend, kein Bruch

**Benutzerparameter erreichen das Template nicht.**
`Engine.ProvisionInstance` nimmt `parameters` entgegen und verwendet sie nicht;
`RenderProvision` setzt nur `InstanceID`, `SafeName` und `Plan`.
`TemplateData.Parameters` wird nirgends gefüllt. `cf create-service -c` bleibt
wirkungslos, und ein `{{ .parameters.x }}` im Template scheitert wegen
`missingkey=error`. *FINDINGS #17.*

**`allowedParameters` wird nur beim Update geprüft.**
`UpdateServiceInstance` validiert, `ProvisionServiceInstance` nicht. Beim
Provision werden beliebige Parameter angenommen und danach verworfen — kein
Fehler, nur Wirkungslosigkeit, und das ist die unangenehmere Variante.

**`readiness.timeoutSeconds` wird nie durchgesetzt.** Ein hängender Operator
lässt die Instanz ewig `in progress` melden. Einen Zustand `failed` kennt die
Engine gar nicht.

**Fehlgeschlagenes Provision hinterlässt verwaiste CRs.** Bricht das Anwenden
zwischen zwei Dokumenten ab, bleibt das erste stehen, ohne dass ein Datensatz
darauf verweist. *FINDINGS #6.*

**Unbind löscht den Datensatz nicht.** Für Definitions-Services entfernt Unbind
nur das projizierte Secret und ruft `broker.Unbind` nicht.

**`osb_active_instances` und `osb_active_bindings` werden nie gesetzt.** Beide
Gauges sind registriert und melden dauerhaft 0.

## Strukturelle Probleme

**Zwei vollständige Broker-Implementierungen nebeneinander.** Jeder Handler
verzweigt einzeln über `resolveDefinition`
(`internal/handlers/definition_instances.go:17`); liefert es `nil` — auch im
Fehlerfall —, fällt der Request stumm auf `internal/broker/broker.go` zurück,
einen zweiten kompletten Broker mit eigenem Katalog. Das ist die Wurzel mehrerer
der obigen Punkte. *FINDINGS #13.*

**Nichts belegt, dass die Prüfungen des Gates fehlschlagen können.**
`cmd/osb-checker` hat keine Mutationssuite: geprüft sind nur `pickService` und
`checkServiceBindingSpec`, nicht die Prüfungen selbst. Ein Gate, dessen
Prüfungen wirkungslos sind, ist von einem grünen nicht zu unterscheiden — genau
das war lange der Fall, als die Auswahl immer den Demo-Service traf
(*FINDINGS #20*, behoben). Der eigenständige Checker hat eine solche Suite.

**Der HTTP-Status wird aus Fehlertexten geraten.**
`internal/handlers/errors.go` entscheidet mit `strings.Contains`. Jeder
DELETE-Fehler mit „not found" im Text wird `410`, auch „service not found".
*FINDINGS #18.*

**Was daraus folgt:** der Vorschlag, die HTTP-Schicht zu ersetzen und die
Engine zu behalten, steht als [ADR 0003](adr/0003-replace-http-layer.md) im
Status *vorgeschlagen*. Der Zustandsspeicher ist davon nicht betroffen.

## Definitionen und Deployment

**Die RabbitMQ-Definition prüft eine Condition, die es nicht gibt.**
`definitions/rabbitmq-cluster.yaml` nennt `type=="Ready"`; der Operator
veröffentlicht `AllReplicasReady` und `ClusterAvailable`. *FINDINGS #22.*

**`values-kind.yaml` ist von `definitions/` abgedriftet.** Die Datei dupliziert
alle Definitionen als eingebettete YAML-Strings. Die eingebettete
RabbitMQ-Definition fehlen `provisionedService`, `mapping` und `type` — ein
Deployment damit liefert stillschweigend ungeformte Bindings. Außerdem kommen drei
Schlüssel doppelt vor (`cnpg-postgresql.yaml`, `minio-objectstorage.yaml`,
`redis-standalone.yaml`), und die Valkey-Definition nennt eine andere CRD-Gruppe
als die Datei unter `definitions/`. **Nichts prüft diese Datei.**
*FINDINGS #21.*

**`rbac.operatorCRDs` deckt nicht alle mitgelieferten Definitionen ab.** Es
fehlen `minio.min.io/tenants` und `redis.redis.opstreelabs.in/redis`; beide
Services bekämen beim Provision einen 403.

**Das Chart rendert mit seinen Vorgaben nicht.** `tls.certManager.enabled: true`
trifft auf einen leeren `issuerRef.name` und damit auf ein `{{ required }}`.
Beabsichtigt, aber unerwartet.

**Die Zweitmeinung in der CI blockiert noch nicht.** Der standalone-Checker
laeuft im Job `conformance` mit `continue-on-error: true`, weil er ueber die
oben genannten Blocker stolpert — Bind und `GET binding` antworten 404. Sein
Bericht steht in der Job-Zusammenfassung und als Artefakt. Sobald die Blocker
weg sind, faellt `continue-on-error` weg.

**`config.logRequests` wird von keinem Template gelesen** und hat keine
entsprechende Umgebungsvariable.

**Die Image-Version ist uneinheitlich gepinnt.** `deploy/k8s/broker.yaml` nennt
`v14`, `Chart.yaml` hat `appVersion: v9`, `values-kind.yaml` pinnt `v9`.

**Der Modulpfad ist ein Platzhalter.** `go.mod` sagt
`github.com/example/osb-broker`, während `schemas/service-definition.schema.json`
(`$id`), `docs/openapi.yaml` (`contact.url`) und die Image-Referenz des Charts
auf `github.com/cyrano-janus/osb-broker-go` zeigen.

**`Dockerfile` gibt `EXPOSE 8080` an**, während das Chart mit TLS auf 8443
lauscht. Folgenlos, aber irreführend.

## Toter Code

Nichts davon schadet, alles davon kostet Lesezeit:

| Stelle | Zustand |
|---|---|
| `internal/handlers/definition_instances.go` | `deprovisionDefinition` wird nie gerufen |
| `internal/broker/broker.go` | `createOperation` und die gesamte `operations`-Map ungenutzt |
| `internal/definition/operator.go` | `ApplyCR` und `ApplyManifests` nur noch aus Tests gerufen; `jsonField` ungenutzt |
| `internal/definition/render.go` | die Methoden `instanceID()` und `safeName()` sind unerreichbar; der Mechanismus ist `lowerCase()` |
| `internal/definition/engine.go`, `internal/handlers/*` | `var _ = …` als Import-Halter |
| `internal/handlers/engine.go` | `NewEngineHolder` nimmt einen Namespace entgegen und verwendet ihn nicht |
| `.github/workflows/ci.yml` | `actions/setup-go` steht im Job `conformance` doppelt |

## Die Entwicklungsplattform

Korifi wird upstream archiviert — Cloud Foundry RFC 0060
(`toc/rfc/rfc-0060-archive-cf-on-k8s-wg.md`, Status `Accepted`) archiviert die
Working Group `CF on K8S` und die Korifi-Repositories; die CI ist bereits
abgeschaltet.

**Das ist ein Werkzeugproblem, kein Produktproblem.** Die Zielplattformen sind
nicht berührt, und die einzige Kopplung ist die OSB-API. Die für den aktuellen
Stand nötigen Artefakte — Helm-Chart, die drei Images per Digest, der
Quellbaum — sind lokal gespiegelt. Offen ist, worauf mittelfristig entwickelt
wird; der RFC nennt `cloudfoundry/kind-deployment` als Nachfolger, was der
Zielplattform näher wäre als Korifi. Einordnung in
[target-platforms.md](target-platforms.md).

## Empfohlene Reihenfolge

Die Punkte hängen zusammen; in dieser Reihenfolge abgearbeitet macht jeder den
nächsten sichtbar oder billiger.

1. **Async** — `accepts_incomplete` aus der Query lesen, `202` antworten,
   `last_operation` gegen die echte Readiness beantworten. Der teuerste offene
   Punkt, und der einzige, der eine Zielplattform sofort blockiert.
2. **Die RabbitMQ-Condition** — sie wird durch den Async-Fix erst sichtbar, weil
   heute niemand auf Readiness wartet. Gehört in dieselbe Runde.
3. **Doppelpfad und Konformitätssuite** — solange die Suite den Fallback-Pfad
   prüft, misst jede weitere Arbeit ins Leere.
4. **Binding-Persistenz** — hängt strukturell am Doppelpfad und fällt mit ihm.
5. **Benutzerparameter und `allowedParameters`** — zusammen, weil beide denselben
   Weg durch die Engine betreffen.
