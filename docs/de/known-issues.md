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

**Zwei Readiness-Pfade sind gegen das CRD ihres Operators geprüft, nicht gegen
einen laufenden Operator.** `valkey-cluster` und `seaweedfs-s3` führen laut
Schema `status.conditions` — ob der Operator dort wirklich `Ready` schreibt,
sagt ein Schema nicht. Gegen einen echten CR gerechnet sind nur
`cnpg-postgresql` und `rabbitmq-cluster`; deren Operatoren laufen in der
Entwicklungsplattform.

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

Die Punkte hängen zusammen; in dieser Reihenfolge abgearbeitet macht jeder den
nächsten sichtbar oder billiger.

1. **`valkey-cluster` und `seaweedfs-s3` gegen einen laufenden Operator** —
   das CRD-Schema sagt, dass es `status.conditions` gibt, nicht dass der
   Operator dort `Ready` schreibt. Braucht diese Operatoren im Cluster.
2. **Ein Durchlauf gegen ein Zielsystem** — bis hierhin ist alles auf der
   Entwicklungsplattform belegt, und das ist nicht dasselbe wie einsatzfähig.
   Siehe [target-platforms.md](target-platforms.md).
