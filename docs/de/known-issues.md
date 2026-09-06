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

Keiner der offenen Punkte blockiert derzeit eine Zielplattform.

## Strukturelle Probleme

## Definitionen und Deployment

**Ein Readiness-Pfad ist gegen das CRD seines Operators geprüft, nicht gegen
einen laufenden Operator.** `seaweedfs-s3` führt laut Schema
`status.conditions` — ob der Operator dort wirklich `Ready` schreibt, sagt ein
Schema nicht. Gegen einen echten CR gerechnet sind `cnpg-postgresql` und
`rabbitmq-cluster`; deren Operatoren laufen in der Entwicklungsplattform.

Trifft ein Pfad daneben, meldet `last_operation` den Grund samt der
Condition-Namen, die der Operator tatsächlich führt, und der Vorgang endet nach
`timeoutSeconds` in `failed` statt in einer endlosen Abfrage.

**`Dockerfile` gibt `EXPOSE 8080` an**, während das Chart mit TLS auf 8443
lauscht. Folgenlos, aber irreführend.

## Toter Code

Nichts davon schadet, alles davon kostet Lesezeit:

| Stelle | Zustand |
|---|---|
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

1. **Ein Durchlauf gegen ein Zielsystem.** Alles bisher Belegte stammt von der
   Entwicklungsplattform. Was dort nicht entschieden werden kann, ist der
   **Vertrauensanker**: eine fremde Plattform prüft das Zertifikat des Brokers
   gegen ihren eigenen Speicher, und welcher der drei Wege gilt, entscheidet
   der Betreiber des Zielsystems — das ist eine Absprache, kein Code.

   Die Bauform steht dagegen fest: der Broker läuft als Kubernetes-Deployment
   im Cluster der Operatoren ([ADR 0009](adr/0009-deployment-model.md)). Damit
   ist auch entschieden, dass er ein Controller sein darf.
2. **Was ein managed Dienst braucht.** Kontingente, Löschschutz und die
   Bestandsübersicht stehen; offen sind Sicherung und Wiederherstellung,
   Point-in-Time-Recovery und Upgrades bestehender Instanzen. Die Liste mit
   dem jeweiligen Stand steht in
   [target-platforms.md](target-platforms.md); seit
   [ADR 0008](adr/0008-depth-over-breadth.md) geht die Arbeit dorthin und nicht
   in weitere Katalogeinträge.

   **Upgrades gehen über das Protokoll.** `maintenanceInfo` je Plan: der
   Katalog nennt den Stand, die Instanz merkt sich den angewendeten,
   `cf services` zeigt die Differenz als `upgrade available`,
   `cf upgrade-service` löst aus, und ein veralteter Stand ist
   `422 MaintenanceInfoConflict`. Der Broker **bietet an**, statt zu verhängen,
   und ändert keine Instanz ohne Request.

   **Was dabei entfällt, ist eine Beobachtung.** Ein periodischer Durchlauf
   meldete auch Datensätze ohne Definition und Ressourcen, die verschwunden
   sind. Diese Zahlen gibt es nicht mehr; ein einmaliger Prüflauf auf Abruf
   wäre der Weg, sie zurückzuholen, ohne den Zeitgeber zurückzuholen.

   **Keine ausgelieferte Definition nennt einen Stand.** Ohne `maintenanceInfo`
   im Plan gibt es nichts zu vergleichen, und `cf services` zeigt nie ein
   Upgrade an. Ein Stand ist eine Zusage über das Gerenderte — sie gehört an
   eine Definition, deren Übergänge belegt sind.

   **Der Planwechsel ist möglich und wird nicht zugesagt.** Die Zusage steht je
   Plan und gilt dem Plan, den eine Instanz *verlässt* — darin liegt die
   Richtung, die es braucht: aus dem kleinen Plan heraus ist der Wechsel
   sicher, aus dem großen heraus schrumpfte der Speicher, den CloudNativePG
   nicht schrumpfen lässt, und die Instanz verlöre ihren Löschschutz. Keine
   ausgelieferte Definition setzt `planUpdateable`, weil kein Übergang gegen
   einen laufenden Operator belegt ist; bis dahin bleibt der Wechsel `422`.
3. **`seaweedfs-s3` gegen einen laufenden Operator** — das CRD-Schema sagt,
   dass es `status.conditions` gibt, nicht dass der Operator dort `Ready`
   schreibt.
