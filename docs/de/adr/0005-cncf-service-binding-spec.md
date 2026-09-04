# ADR 0005: CNCF Service Binding Specification als Zielformat

> [English](../../en/adr/0005-cncf-service-binding-spec.md) · Führende Fassung: deutsch

**Status:** angenommen · **Betrifft:** `internal/definition/servicebinding.go`, `definitions/`

## Kontext

Den Namen des Credential-Secrets aus einem Namenstemplate abzuleiten —
`{{ .safeName }}-app` bei CloudNativePG, `{{ .safeName }}-default-user` bei
RabbitMQ — heißt raten statt wissen: der Broker baut ein Schema nach, das sich
jeder Operator selbst ausdenkt und jederzeit ändern kann.

Und ein Binding, das **alle** Schlüssel des Secrets durchreicht, enthält, was
der Operator zufällig hineinschreibt. Beim RabbitMQ-Operator sind das
`default_user.conf` — eine Konfigurationsdatei — und `connection_string`. Beides
hat in einem Binding nichts verloren, und die Anwendung muss raten, welche
Schlüssel sie verwenden darf.

## Entscheidung

**Die CNCF Service Binding Specification wird das Zielformat.** Drei Teile:

### 1. Der Operator sagt selbst, wo die Credentials liegen

Mit `provisionedService: true` liest der Broker den Secret-Namen aus
`.status.binding.name` des provisionierten CR — dem „Provisioned Service"-Duck-
Type der Spezifikation. Der Operator gibt die Auskunft, statt dass der Broker sie
rekonstruiert.

**Der Pfad ist absichtlich nicht konfigurierbar.** Der Kommentar im Code sagt
es kurz: wäre er es, wäre es wieder eine Konvention und kein Standard.

Ein leeres oder fehlendes Feld fällt auf `credentialsFromSecret` zurück — für
Operator-Versionen, die die Spezifikation nicht erfüllen. Ein Wert, der
existiert, aber keine Zeichenkette ist, ist dagegen ein harter Fehler und wird
nicht als „nicht vorhanden" behandelt.

### 2. `mapping` definiert die Zielform, es ergänzt sie nicht

Ist `mapping` gesetzt, besteht das Ergebnis **genau** aus den genannten
Schlüsseln plus `type` und `provider`. Ein Adapter, der zusätzlich alle
Originalschlüssel durchreicht, macht das Ergebnis unvorhersehbar und den Zweck —
eine definierte Zielform — zunichte.

- `from` kopiert einen Schlüssel. **Fehlt er zur Bindezeit, ist das ein harter
  Fehler**, kein stilles Weglassen: eine Anwendung, der ein Feld fehlt, bekommt
  sonst einen unverständlichen Folgefehler.
- `value` ist ein Go-Template über `.credentials`, etwa um eine URI aus mehreren
  Feldern zusammenzusetzen. Diese Templates werden schon beim **Laden** der
  Definition übersetzt, nicht erst beim Binden.

`type` und `provider` werden **nach** dem Mapping gesetzt, damit ein
Mapping-Eintrag namens `type` den Wert aus der Definition nicht überschreiben
kann.

### 3. Optional ein spec-konformes Secret im Ziel-Namespace

Mit `projectSecret: true` schreibt der Broker die Credentials zusätzlich als
Secret vom Typ `servicebinding.io/<type>` in den Namespace der Instanz — für
Konsumenten, die kein Cloud Foundry sind. Das Secret trägt eine
`OwnerReference` auf das provisionierte CR: wird die Instanz gelöscht, räumt
Kubernetes es mit ab, auch ohne vorheriges Unbind.

Deshalb ist `type` bei `projectSecret` Pflicht — die Spezifikation verlangt auf
jedem Binding-Secret einen Typ.

## Konsequenzen

**Gut:**

- Der Secret-Name ist gewusst statt geraten, wo der Operator mitspielt.
- Das Binding hat eine definierte Form. Eine Anwendung kann sich auf `username`,
  `password`, `host`, `port`, `uri`, `type` verlassen.
- Die Zielgruppe wächst über Cloud Foundry hinaus: ein projiziertes Secret ist
  für jeden Kubernetes-Workload verwendbar.
- Rückwärtskompatibel — bestehende Definitionen mit `credentialsFromSecret`
  laufen unverändert weiter.

**Preis:**

- `projectSecret` verlangt clusterweite Schreibrechte auf Secrets
  (`rbac.projectedBindingSecrets: true`). Das ist ein spürbares Recht und
  deshalb standardmäßig aus.
- Zwei Wege zum selben Ziel bleiben nebeneinander bestehen, solange Operatoren
  die Spezifikation nicht flächendeckend erfüllen.
- Ohne `mapping` reicht eine Definition alles durch. Das ist Absicht, heißt aber,
  dass die Zielform je Definition festgelegt werden muss.

## Verworfene Alternativen

| Option | Warum nicht |
|---|---|
| Eigene Konvention weiterpflegen | bricht bei jedem neuen Operator |
| CEL-Ausdrücke für die Zuordnung | mächtiger als Templates, aber eine zweite Ausdruckssprache im Schema; die Templates reichen bisher |
| Nur `credentialKeys` als Filter | filtert, formt aber nicht — eine URI ließe sich damit nicht zusammensetzen |
