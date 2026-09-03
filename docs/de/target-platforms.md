# Zielplattformen

> [English](../en/target-platforms.md) · Führende Fassung: deutsch

Dieses Dokument steht am Anfang, weil es die Lesart aller übrigen bestimmt.

| Rolle | System | Bedeutung |
|---|---|---|
| **Zielplattform** | produktives Cloud Foundry | wofür der Broker gebaut wird |
| **Zielplattform** | Tanzu TAS | dito |
| **Zielplattform** | externe Marketplaces mit OSB-Anbindung | dito |
| **Entwicklungsplattform** | Korifi auf kind | Testgerät, kein Zielsystem |

**Warum das ausdrücklich dasteht.** Jeder Nachweis in diesem Repo ist gegen
Korifi auf kind aufgenommen, und die Beispiele im Quickstart sprechen von
`cf api https://localhost`. Wer das liest, ohne den Unterschied zu kennen, hält
Korifi für das Ziel und leitet daraus falsche Prioritäten ab: mehrere
Abweichungen von OSB 2.17 bleiben auf Korifi folgenlos und sind auf produktivem
Cloud Foundry oder TAS Blocker. Der Maßstab für „fertig" ist das Zielsystem,
nicht die Entwicklungsplattform.

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

**Zwei Punkte, die auf dem Zielsystem erst noch beantwortet werden müssen** und
heute niemand beantworten kann, weil es keinen Durchlauf gab: wie das
Zertifikatsvertrauen konkret hergestellt wird, und ob der Broker als
Kubernetes-Deployment neben der Plattform läuft oder als CF-App auf ihr. Beides
ändert nichts am Code, aber alles an der Betriebsanleitung.

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

## Verifikationsstand, ehrlich

**Verifiziert ist ausschließlich Korifi v0.18.0 auf kind.** Es gibt

- keinen Durchlauf gegen produktives Cloud Foundry,
- keinen gegen Tanzu TAS,
- keinen gegen einen externen Marketplace,
- keinen unter Last oder mit echter Mandantentrennung.

Das ist keine Lücke der Dokumentation, sondern der Stand der Arbeit. Er gehört
hierher, weil die Nachweistabellen in der [README](../../README.de.md) sonst
mehr versprechen, als sie belegen: sie belegen, dass der beschriebene Ablauf
*auf der Entwicklungsplattform* wirklich lief.

## Woran sich die Reife misst

Diese Tabelle ist die eigentliche Arbeitsliste. Sie zeigt, welche der bekannten
Abweichungen auf der Entwicklungsplattform durchgehen und auf dem Zielsystem
nicht. Die Langfassung je Punkt steht in
[known-issues.md](known-issues.md), die Codestellen in
[reference/osb-api.md](reference/osb-api.md).

| Abweichung | auf Korifi | auf produktivem CF / TAS |
|---|---|---|
| Provision antwortet immer `201`, nie `202` | fällt kaum auf, die Testservices sind schnell | **Blocker** — echte Backing-Services brauchen Minuten, die Plattform hält die Instanz sofort für fertig und bindet gegen ein nicht existierendes Secret |
| Definitions-Bindings werden nicht persistiert | unauffällig, solange niemand `GET binding` ruft | **Blocker** — `cf service-key` liest über `GET binding` und bekommt 404 |
| `last_operation` für Bindings antwortet hart `succeeded` | unauffällig | **Blocker**, sobald Bindings asynchron werden |
| Demo-Katalog `service-1` / `service-2` immer im Katalog | kosmetisch | **nicht vertretbar** in einem produktiven Marketplace |
| Benutzerparameter erreichen das Template nie | `cf create-service -c` wirkt nicht | funktionale Lücke, kein Bruch |
| `allowedParameters` nur bei `PATCH` geprüft | unauffällig | still falsch — Provision nimmt alles an und verwirft es kommentarlos |
| Fehlerklassifikation über Fehlertexte | unauffällig | falsche Statuscodes steuern die Retry-Logik der Plattform fehl |

Die vier mit **Blocker** markierten Punkte sind der Grund, warum der in
[ADR 0003](adr/0003-replace-http-layer.md) vorgeschlagene Umbau
zur Entscheidung steht: sie liegen alle in der HTTP- und State-Schicht, keiner
in der Engine.

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
