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

**Die Readiness-Pfade von fünf Definitionen sind ungeprüft.**
`minio-objectstorage`, `redis-standalone`, `valkey-cluster`, `redpanda-cluster`
und `seaweedfs-s3` nennen alle `status.conditions.#(type=="Ready").status`, ohne
dass je ein CR des jeweiligen Operators dagegen gehalten wurde — die Operatoren
sind in der Entwicklungsplattform nicht installiert. Belegt sind nur
`cnpg-postgresql` und `rabbitmq-cluster`.

Trifft ein Pfad daneben, meldet `last_operation` seit der Diagnose in
`EvaluateReadiness` den Grund samt der Condition-Namen, die der Operator
tatsächlich führt. Der Provisioning-Vorgang läuft trotzdem in das Zeitlimit der
Plattform; der Unterschied ist, dass man danach weiß, warum.

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

1. **Die fünf ungeprüften Readiness-Pfade** — je Operator einmal ein CR
   anlegen und den Pfad dagegen rechnen. Braucht die Operatoren im Cluster.
   Seit das Zeitlimit greift, endet ein falscher Pfad nach zehn Minuten in
   `failed` statt in einer endlosen Abfrage — sichtbar ist er trotzdem erst,
   wenn jemand hinsieht.
2. **Ein Durchlauf gegen ein Zielsystem** — bis hierhin ist alles auf der
   Entwicklungsplattform belegt, und das ist nicht dasselbe wie einsatzfähig.
   Siehe [target-platforms.md](target-platforms.md).
