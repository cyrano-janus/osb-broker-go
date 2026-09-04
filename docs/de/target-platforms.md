# Zielplattformen

> [English](../en/target-platforms.md) · Führende Fassung: deutsch

Der Broker wird für die folgenden Systeme gebaut.

| Rolle | System | Bedeutung |
|---|---|---|
| **Zielplattform** | produktives Cloud Foundry | wofür der Broker gebaut wird |
| **Zielplattform** | Tanzu TAS | dito |
| **Zielplattform** | externe Marketplaces mit OSB-Anbindung | dito |
| **Entwicklungsplattform** | Korifi auf kind | Testgerät, kein Zielsystem |

**Die Unterscheidung ist nicht kosmetisch.** Mehrere Abweichungen von OSB 2.17
bleiben auf Korifi folgenlos und sind auf produktivem Cloud Foundry oder TAS
Blocker. Der Maßstab für „fertig" ist deshalb das Zielsystem, nicht die
Entwicklungsplattform — und alles, was dieses Repo an Nachweisen führt, ist auf
der Entwicklungsplattform aufgenommen.

## Was auf allen Plattformen gleich ist

Die Kopplung ist die **Open-Service-Broker-API 2.17** und sonst nichts. Der
Broker läuft als gewöhnliches Kubernetes-Deployment; jede Plattform *konsumiert*
dieselbe URL. Es gibt keinen plattformspezifischen Code im Broker, und es soll
keinen geben — die Begründung steht in [ADR 0006](adr/0006-platform-independence.md).

Gleich ist damit überall:

- der Katalog unter `GET /v2/catalog` und das, was daraus im Marketplace wird,
- der Lebenszyklus `provision → bind → unbind → deprovision`,
- die Registrierung als Broker mit URL und Basic-Auth-Zugangsdaten,
- das Freischalten der Pläne, bevor Nutzer sie sehen,
- die `X-Broker-API-Version`-Aushandlung.

Ein Broker, der gegen OSB 2.17 konform ist, funktioniert auf allen vier. Genau
deshalb ist Konformität kein Selbstzweck, sondern das ganze Produkt.

## Was sich unterscheidet

Die Unterschiede liegen nicht in der API, sondern drumherum — und sie sind der
Grund, warum ein Erfolg auf Korifi noch keiner auf TAS ist.

| Thema | Korifi (Entwicklung) | produktives CF / TAS |
|---|---|---|
| Registrierung | `CFServiceBroker`-CR oder `cf create-service-broker` | `cf create-service-broker` |
| Erreichbarkeit | clusterinterner Service-DNS-Name | der Broker muss aus dem Plattformnetz erreichbar sein — Route, Firewall, ggf. eigene App-Instanz |
| Zertifikatsvertrauen | eigene CA, in Korifi über `SSL_CERT_DIR` eingehängt | die Plattform-Truststore-Kette; zu klären, ob eine interne CA akzeptiert wird oder ein öffentlich vertrauenswürdiges Zertifikat nötig ist |
| Plan-Sichtbarkeit | jeder `CFServicePlan` muss einzeln auf `public` gepatcht werden | `cf enable-service-access` |
| Managed Services | Feature-Flag `experimental.managedServices.enabled=true` nötig | Standardfunktion, kein Schalter |
| Mandanten | ein kind-Cluster, ein Nutzer | echte Orgs und Spaces, echte Rechtetrennung |
| Last | ein Entwickler, ein Service auf einmal | viele gleichzeitige Operationen |

**Zwei Punkte sind je Zielsystem zu klären** und lassen sich nur dort
beantworten: wie das Zertifikatsvertrauen hergestellt wird, und ob der Broker
als Kubernetes-Deployment neben der Plattform läuft oder als CF-App auf ihr.
Beides ändert nichts am Code, aber alles an der Betriebsanleitung.

## Externe Marketplaces

Der dritte Zielfall ist der, bei dem der Broker nicht bei Cloud Foundry
registriert wird, sondern bei einer beliebigen Plattform, die die OSB-API
spricht. Für den Broker ist das derselbe Fall wie CF: ein Konsument, der
`/v2/catalog` liest und den Lebenszyklus fährt.

Praktisch heißt das zweierlei. Erstens sind die Felder, die Cloud Foundry
großzügig ignoriert, hier möglicherweise Pflicht — etwa ein echter
`dashboard_url` statt des heute fest verdrahteten
`https://dashboard.example.com/instances/<id>`. Zweitens ist die Konformitäts-
suite `cmd/osb-checker` das einzige Werkzeug, das diesen Fall überhaupt prüft;
sie ersetzt hier die Plattform.

## Verifikationsstand

**Verifiziert ist Korifi v0.18.0 auf kind.** Was dort nachgewiesen ist:

| Nachweis | Ergebnis |
|---|---|
| OSB-2.17-Lebenszyklus über HTTP | Integrationstest deckt catalog → provision → last_operation → bind → unbind → deprovision ab |
| Registrierung, Marketplace, `cf create-service` | gegen Korifi auf kind |
| Generic Engine Ende zu Ende | `cf create-service cnpg-postgresql large` erzeugt einen echten CloudNativePG-Cluster (3 Instanzen, 10Gi); `psql` im Pod antwortet, Credentials aus dem Operator-Secret |
| Neustart-Persistenz | Instanzen und Bindings überleben Kill und Rescheduling |
| Asynchrones Provision | `202` mit `operation`, `last_operation` meldet `in progress` bis der Operator fertig ist, danach `succeeded`; ohne `accepts_incomplete=true` antwortet der Broker `422 AsyncRequired` |
| Binding-Lebenszyklus vollständig | Bind `201`, Wiederholung `200` mit denselben Zugangsdaten, `GET binding` `200`, Unbind `200`, Unbind einer unbekannten Binding `410` |
| Ein Pfad, kein Rückfall | der Katalog besteht ausschließlich aus ServiceDefinitions; eine unbekannte `service_id` ist `400` und läuft nicht in eine zweite Implementierung |
| Statuscodes aus Fehlerwerten | ein unbekannter Plan ist auch auf einem `DELETE` `400` und nicht `410` — die Zuordnung hängt nicht mehr an Formulierungen |
| Secret-Name aus `status.binding.name` | der RabbitMQ-Operator meldet `osb-<id>-default-user`, der Broker nutzt ihn ohne Namenstemplate |
| Zielform per `mapping` | das Binding enthält genau `host, password, port, provider, type, uri, username`; `default_user.conf` und `connection_string` bleiben draußen |
| Spec-konformes Secret | Typ `servicebinding.io/rabbitmq`, Labels für Instanz und Binding, `OwnerReference` auf den `RabbitmqCluster` |
| Aufräumen beim Unbind | `cf delete-service-key` entfernt das projizierte Secret |
| Konformität gegen den CRD-Store | 24 von 24 im kind-Cluster gegen echtes RBAC, über das Helm-Chart ausgerollt |
| Sichtbarkeit des Zustands | `kubectl get osbi` und `osbb` zeigen Instanz und Binding mit Service, Plan und Ready |
| Credentials getrennt | nicht im Binding-CR, sondern in einem Secret mit `OwnerReference` darauf |
| Kontext vollständig | `platform`, `spaceGuid` und `organizationGuid` werden abgebildet |
| Konformität über HTTPS mit Client-Zertifikat | 24 von 24 — und 24 von 24 auch mit dem Client-Zertifikat allein, ohne Basic Auth |
| Zertifikatsrotation ohne Neustart | TLS-Secret im laufenden Pod getauscht: die ausgelieferte Seriennummer wechselt, `restartCount` bleibt 0 |
| Registrierung über `https://` | `CFServiceBroker` wird ready bei `trustInsecureServiceBrokers=false` — die Plattform prüft das Zertifikat |
| Voller Lebenszyklus über HTTPS | `cf create-service` bis `cf delete-service` einschließlich echter Credentials |
| mTLS-Autorisierung | ein von derselben CA signiertes, aber nicht gelistetes Client-Zertifikat bekommt 401 |
| Probes bleiben offen | `/healthz` ohne Client-Zertifikat erreichbar |

Nicht nachgewiesen ist alles Übrige. Es gibt

- keinen Durchlauf gegen produktives Cloud Foundry,
- keinen gegen Tanzu TAS,
- keinen gegen einen externen Marketplace,
- keinen unter Last oder mit echter Mandantentrennung.

Das ist der Stand der Arbeit, nicht eine Lücke der Beschreibung.

## Woran sich die Reife misst

Diese Tabelle ist die eigentliche Arbeitsliste. Sie zeigt, welche der bekannten
Abweichungen auf der Entwicklungsplattform durchgehen und auf dem Zielsystem
nicht. Die Langfassung je Punkt steht in
[known-issues.md](known-issues.md), die Codestellen in
[reference/osb-api.md](reference/osb-api.md).

| Abweichung | auf Korifi | auf produktivem CF / TAS |
|---|---|---|
| Benutzerparameter erreichen das Template nie | `cf create-service -c` wirkt nicht | funktionale Lücke, kein Bruch |
| `readiness.timeoutSeconds` wird nie durchgesetzt | unauffällig, die Testservices sind schnell | ein hängender Operator meldet `in progress`, bis die Plattform selbst aufgibt |
| Fünf Readiness-Pfade sind ungeprüft | die Operatoren sind gar nicht installiert | ein danebenliegender Pfad kostet ein Plattform-Zeitlimit je Instanz |

Keine dieser Abweichungen ist ein Ausschlusskriterium. Die Punkte, die es waren,
lagen alle in der HTTP-Schicht und sind mit ihr ersetzt worden —
[ADR 0003](adr/0003-replace-http-layer.md). Was bleibt, sind funktionale
Lücken und Sorgfaltsarbeit an den Definitionen.

## Was mit Korifi passiert

Korifi wird upstream archiviert. Die Cloud Foundry Foundation hat mit
**RFC 0060** (`toc/rfc/rfc-0060-archive-cf-on-k8s-wg.md`, Status `Accepted`)
beschlossen, die Working Group `CF on K8S` und die Korifi-Repositories zu
archivieren; die CI ist bereits abgeschaltet.

**Für dieses Projekt ist das ein Werkzeugproblem, kein Produktproblem.** Die
Zielplattformen sind davon nicht berührt, und die einzige Kopplung an Korifi ist
die OSB-API, die weiterlebt. Praktisch folgt daraus:

- Die vorhandene Entwicklungsplattform läuft weiter, bekommt aber keine
  Upstream-Korrekturen mehr. Die für den aktuellen Stand nötigen Artefakte sind
  lokal gespiegelt.
- Mittelfristig ist zu entscheiden, worauf entwickelt wird. Der RFC nennt
  `cloudfoundry/kind-deployment` als Nachfolger — echtes Cloud Foundry auf kind,
  was der Zielplattform näher wäre als Korifi es war.
- Ein Wechsel der Entwicklungsplattform ändert am Broker nichts. Das ist der
  Punkt von [ADR 0006](adr/0006-platform-independence.md).
