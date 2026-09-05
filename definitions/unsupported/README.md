# Definitionen, die dieser Broker nicht ausliefert

Was hier liegt, wird **nicht geladen**: `LoadFromDir` überspringt
Unterverzeichnisse. Die Dateien bleiben, weil sie Arbeit und einen Befund
tragen — ausgeliefert werden sie nicht, weil sie nachweislich nicht
funktionieren würden.

**Der Katalog hat damit keinen In-Memory-Cache mehr.** Das ist eine
Entscheidung, kein Versehen: die CNCF führt kein Cache-Projekt, und die beiden
naheliegenden Kandidaten scheitern aus verschiedenen Gründen — siehe unten.

## `redis-standalone.yaml`

Zwei Gründe, unabhängig voneinander. Der zweite wiegt schwerer.

### 1. Die Lizenz erlaubt genau diesen Anwendungsfall nicht

Redis war bis 7.2.x BSD-3-Clause. Seit März 2024 gilt ab Version 7.4
**RSALv2 oder SSPLv1** — beides „source available", keines OSI-konform; Redis
8.0 (Mai 2025) hat AGPLv3 als dritte Option ergänzt.

Entscheidend ist nicht das Copyleft, sondern der Nutzungsfall: **RSALv2
untersagt ausdrücklich, die Software Dritten als *managed Service*
bereitzustellen**, und SSPLv1 §13 verlangt in dem Fall die Veröffentlichung des
gesamten Service-Stacks unter SSPL. Ein Service Broker, der Instanzen für
Konsumenten eines Marketplace herstellt, ist der Managed-Service-Fall — beide
Lizenzen sind geschrieben, um das zu verhindern. AGPLv3 bei Redis 8 erlaubt es,
verpflichtet aber zum Angebot des Corresponding Source an Netznutzer.

Die Definition pinnt `quay.io/opstree/redis:v7.0.15`, also eine Version *vor*
der Umstellung — für sich genommen unbedenklich, aber 7.0.x ist End-of-Life.
Wer auf eine gepflegte Version hebt, läuft ohne Warnung in RSAL/SSPL. Ein
Angebot ohne Sicherheitsfixes ist ebenso wenig tragfähig.

Der Operator selbst ist unkritisch: OT-CONTAINER-KIT/redis-operator steht unter
Apache 2.0. Das Problem ist das Server-Image, nicht der Controller.

**Wer Redis trotzdem anbieten will**, braucht eine kommerzielle Vereinbarung mit
Redis Ltd. oder muss die AGPL-Pflichten erfüllen — das ist eine Rechts-, keine
Technikfrage. Der protokollkompatible Weg ohne diese Frage ist
[Valkey](../valkey-cluster.yaml) (BSD-3-Clause, Linux Foundation, Fork von
Redis 7.2.4). Dessen Definition ist heute aber selbst noch nicht belastbar —
siehe ihre Annotation `experimental-operator-incompatible`.

### 2. Der Operator führt keinen auswertbaren Status

Das CRD
`redis.redis.opstreelabs.in/v1beta2` (OT-CONTAINER-KIT/redis-operator)
beschreibt `status` als

```yaml
status:
  description: RedisStatus defines the observed state of Redis
  type: object
```

— ohne eine einzige Eigenschaft. Ein strukturelles Schema ohne
`x-kubernetes-preserve-unknown-fields` heißt: der API-Server schneidet alles
weg, was der Operator dort hineinschreiben wollte. In den Releases v0.18.0,
v0.19.1 und v0.20.1 ist das gleich.

Die Engine liest Readiness aus dem provisionierten CR. Für dieses CRD gibt es
dort nichts zu lesen — **jede** Instanz liefe in das Zeitlimit und meldete
danach `failed`. Ein Dienst, der so ausgeliefert wird, ist ein Versprechen, das
der Broker nicht halten kann.

Die Definition trug bis dahin den kopierten Pfad
`status.conditions.#(type=="Ready").status`, wie vier weitere. Zwei davon waren
aus demselben Grund falsch und sind korrigiert (MinIO auf
`status.currentState == Initialized`, Redpanda auf die einzige Condition, die
sein CRD kennt: `ClusterConfigured`). Diese hier lässt sich nicht korrigieren,
solange die Readiness aus dem CR selbst kommen muss.

Technisch bräuchte es dafür eine Readiness aus einem *anderen* Objekt — der
Operator legt ein StatefulSet an, dessen `status.readyReplicas` die Frage
beantwortet. Das wäre eine Erweiterung der Definitionssprache (etwa
`readiness.from:` mit Art und Namensschema), keine Korrektur an dieser Datei.

**Diese Erweiterung ist aber kein Weg zurück für Redis.** Sie löst Grund 2 und
lässt Grund 1 unberührt; wer sie baut, sollte das mit einem anderen Operator
begründen. Eine Definition kommt hier nur heraus, wenn **beide** Gründe
entfallen — und der erste entfällt nicht durch Code.

## `valkey-cluster.yaml`

Lizenzrechtlich wäre Valkey der saubere Weg: BSD-3-Clause, Linux Foundation,
Fork von Redis 7.2.4, protokollkompatibel — die Antwort, die das Ökosystem auf
die Redis-Umstellung gegeben hat. Daran liegt es nicht.

**Der Operator kann nicht binden.** `hyperspike/valkey-operator` legt kein
Credentials-Secret an. Die Definition trug deshalb die Konvention „ein
Platform-Admin erzeugt das Secret vor dem ersten Bind" — das ist kein Broker,
das ist ein Ticket. Ein Service, dessen Bind einen manuellen Schritt braucht,
gehört nicht in einen Marketplace-Katalog.

Dazu die eigene Annotation der Definition,
`broker.osb.io/status: experimental-operator-incompatible`, und ein
Readiness-Pfad, der nur gegen das CRD-Schema geprüft ist, nie gegen ein
laufendes CR.

Valkey selbst ist nicht das Problem — sein Operator-Ökosystem ist noch nicht
so weit. Wenn sich das ändert (ein Operator, der ein Secret erzeugt und es in
seinem Status nennt), ist die Definition schnell wieder gültig.

## Die Schema-Ausschnitte bleiben

`internal/definition/testdata/crds/unsupported-*.yaml` hält je einen
`status`-Ausschnitt aus dem CRD des Operators fest, damit die Befunde
nachprüfbar bleiben, ohne den Operator zu installieren.
