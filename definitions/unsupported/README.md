# Definitionen, die dieser Broker nicht ausliefert

Was hier liegt, wird **nicht geladen**: `LoadFromDir` überspringt
Unterverzeichnisse. Die Dateien bleiben, weil sie Arbeit und einen Befund
tragen — ausgeliefert werden sie nicht, weil sie nachweislich nicht
funktionieren würden.

## `redis-standalone.yaml`

**Der Operator führt keinen auswertbaren Status.** Das CRD
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

**Damit sie wieder ausgeliefert werden kann**, bräuchte die Engine eine
Readiness aus einem *anderen* Objekt — der Operator legt ein StatefulSet an,
dessen `status.readyReplicas` die Frage beantwortet. Das ist eine Erweiterung
der Definitionssprache (etwa `readiness.from:` mit Art und Namensschema) und
keine Korrektur an dieser Datei.

`internal/definition/testdata/crds/unsupported-redis-standalone.yaml` hält den
Schema-Ausschnitt fest, damit der Befund nachprüfbar bleibt.
