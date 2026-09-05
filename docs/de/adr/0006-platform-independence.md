# ADR 0006: Die OSB-API ist die einzige Kopplung an die Plattform

> [English](../../en/adr/0006-platform-independence.md) · Führende Fassung: deutsch

**Status:** angenommen · **Betrifft:** das gesamte Repository

## Kontext

Der Broker wird für **produktives Cloud Foundry**, **Tanzu TAS** und die
Anbindung an **externe Marketplaces mit Service-Broker-Schnittstelle** gebaut.
Entwickelt und geprüft wird er gegen **Korifi auf kind** — einer
Entwicklungsplattform, die ausdrücklich kein Zielsystem ist.

Diese Konstellation lädt zu einem Fehler ein: es wäre an vielen Stellen
bequemer, auf eine Eigenheit der Entwicklungsplattform hin zu programmieren.
Korifi verzeiht mehrere Abweichungen von OSB 2.17 — synchrone Antworten,
fehlende Binding-Persistenz —, die auf einem Zielsystem Blocker sind. Wer nur
gegen Korifi misst, hält den Broker für fertig, wenn er es nicht ist.

## Entscheidung

**Die OSB-API 2.17 ist die einzige Kopplung an die konsumierende Plattform. Es
gibt keinen plattformspezifischen Code im Broker, und es soll keinen geben.**

Daraus folgen drei Regeln:

1. **Der Broker läuft als gewöhnliches Kubernetes-Deployment.** Er ist ohne
   Cloud Foundry nutzbar — per `curl`, per `cmd/osb-gate` oder von jeder
   OSB-Plattform aus. Jede Plattform *konsumiert* dieselbe URL; ein zweites
   Deployment je Plattform gibt es nicht.
2. **Kein `if korifi` im Code.** Wo eine Plattform sich eigentümlich verhält,
   wird das spezifikationskonform abgefangen, nicht plattformspezifisch. Das
   sichtbarste Beispiel: Cloud Foundry sendet die Space-GUID ausschließlich im
   veralteten obersten Feld des Requests, nicht im verschachtelten `context`.
   Der Broker wertet **beide** von der Spezifikation erlaubten Quellen aus, mit
   Vorrang für die neuere — das ist konform und funktioniert überall, statt eine
   Korifi-Sonderbehandlung zu sein.
3. **Der Maßstab für „fertig" ist das Zielsystem.** Ein Kompromiss an der
   OSB-Konformität wird daran gemessen, was produktives Cloud Foundry oder TAS
   damit tut — nicht daran, ob Korifi ihn durchgehen lässt.

Wo Anpassungen an eine Plattform nötig sind, gehören sie **außerhalb** des
Brokers: in die Wertedatei des Helm-Charts, in die Registrierung, in die
Betriebsanleitung. Nicht in den Go-Code.

## Konsequenzen

**Gut:**

- Ein Wechsel der Entwicklungsplattform ändert am Broker nichts. Das ist keine
  Theorie mehr: Korifi wird upstream archiviert (RFC 0060), und genau deshalb
  ist das ein Werkzeugproblem und kein Produktproblem.
- Der externe Marketplace ist kein Sonderfall, sondern derselbe Fall wie Cloud
  Foundry — ein Konsument, der `/v2/catalog` liest und den Lebenszyklus fährt.
- Die Konformitätssuite `cmd/osb-gate` ist damit ein sinnvolles Gate und
  nicht nur eine Formalität: sie prüft genau die einzige Kopplung.

**Preis:**

- Abweichungen von der Spezifikation sind teurer, als sie sich anfühlen. Sie
  fallen auf der Entwicklungsplattform nicht auf und müssen deshalb aus dem Code
  heraus erkannt werden, nicht aus dem Verhalten. Die Liste dazu steht in
  [reference/osb-api.md](../reference/osb-api.md).
- Bequeme Abkürzungen entfallen. Wo Korifi ein Feld ignoriert, muss der Broker
  es trotzdem richtig behandeln.
- Es bleibt Arbeit, die nur ein echter Durchlauf gegen eine Zielplattform
  erledigen kann: Erreichbarkeit, Zertifikatsvertrauen, Verhalten unter Last.
  Verifiziert ist bisher **ausschließlich** Korifi auf kind, und das gehört so
  gesagt — siehe [target-platforms.md](../target-platforms.md).

## Abgrenzung

Diese Entscheidung sagt **nicht**, dass der Broker plattformneutral gegenüber
*Kubernetes* wäre. Er ist tief an Kubernetes gekoppelt: der Zustand liegt in
CRDs ([ADR 0001](0001-kubernetes-as-state-store.md)), die Services sind
Operatoren, die Bindings folgen einer Kubernetes-Spezifikation
([ADR 0005](0005-cncf-service-binding-spec.md)). Unabhängig ist er von der
Plattform, die ihn **konsumiert**, nicht von der, auf der er **läuft**.
