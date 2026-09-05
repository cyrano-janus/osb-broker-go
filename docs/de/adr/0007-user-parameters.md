# ADR 0007: Benutzerparameter überlagern den Plan, ein Update verschmilzt

> [English](../../en/adr/0007-user-parameters.md) · Führende Fassung: deutsch

**Status:** angenommen · **Betrifft:** `internal/definition`, `internal/handlers`, alle ServiceDefinitions

## Kontext

Eine ServiceDefinition kennt zwei Quellen für Werte, die ins Manifest gerendert
werden: die `params` des gewählten Plans, die der Betreiber festlegt, und die
`parameters` aus dem Request, die der Benutzer mit `cf create-service -c`
schickt. Welche Schlüssel er überhaupt schicken darf, steht in
`allowedParameters` des Plans.

Der Template-Punkt bietet für beide eine eigene Sicht an, `.plan` und
`.parameters`. Damit sind zwei Fragen zu beantworten, die OSB 2.17 nicht
beantwortet.

**Erstens: was sieht das Template?** Eine strikte Trennung — `.plan` sind nur
Planwerte, `.parameters` nur Benutzerwerte — klingt sauber, verschiebt die
Arbeit aber in jedes Template. Es gilt `missingkey=error`, also ist
`{{ .parameters.storageSize }}` kein leerer Wert, sondern ein Renderfehler,
sobald der Benutzer den Parameter nicht schickt. Jeder optionale Parameter
bräuchte eine Fallunterscheidung mit einem Vorgabewert, der dann in *zwei*
Fassungen im Repository steht: einmal in `params`, einmal im Template.

**Zweitens: was passiert beim `PATCH`?** Ein Update trägt in aller Regel nur
die geänderten Schlüssel. Der Broker speichert die Parameter je Instanz und gibt
sie bei `GET /v2/service_instances` zurück; er muss also entscheiden, ob das
gesendete Objekt die vollständige neue Konfiguration ist oder eine Ergänzung.

## Entscheidung

**Ein erlaubter Benutzerparameter überschreibt unter `.plan` den gleichnamigen
Planwert. `.parameters` bleibt daneben als reine Benutzersicht bestehen.**

Der Plan liefert damit den Vorgabewert, die Freigabeliste bestimmt, welcher
davon verhandelbar ist, und das Template liest nur eine Stelle:

```yaml
params:
  storageSize: 1Gi      # Vorgabe
  instances: 1          # nicht überschreibbar
allowedParameters: [storageSize]
```

```gotemplate
size: {{ .plan.storageSize }}   # 1Gi, oder der Wert des Benutzers
```

**Beim `PATCH` werden Parameter verschmolzen, nicht ersetzt.** Gesendete
Schlüssel überschreiben die gespeicherten, ungenannte bleiben stehen. Fehlt
`plan_id`, gilt der Plan, unter dem die Instanz angelegt wurde. Geprüft wird
gegen `allowedParameters` der **verschmolzene** Satz, nicht nur das Neue: nach
einem Planwechsel muss die gesamte Konfiguration im Zielplan erlaubt sein.

## Konsequenzen

**Gut:**

- Die sieben mitgelieferten Definitionen mussten für diese Fähigkeit nicht
  angefasst werden. Ein Wert wird überschreibbar, indem man seinen Namen in
  `allowedParameters` aufnimmt — nicht, indem man das Template umschreibt.
- Der Vorgabewert steht an genau einer Stelle, in `params`. Es gibt keine
  zweite Fassung im Template, die davon abweichen könnte.
- Ein Teil-Update verliert nichts. `GET /v2/service_instances` meldet den Satz,
  unter dem die Instanz wirklich läuft.
- Ein wiederholtes `PUT` mit abweichenden Parametern lässt sich als `409`
  beantworten, weil der gespeicherte Satz vergleichbar ist.

**Preis:**

- Ein Parameter lässt sich per `PATCH` nur ändern, nicht entfernen. Wer zurück
  auf den Planwert will, muss ihn ausdrücklich auf diesen Wert setzen.
- `.plan` heißt nicht mehr „was im Plan steht", sondern „was gilt". Wer die
  Herkunft eines Wertes im Template unterscheiden muss, braucht `.parameters` —
  und muss dort mit `missingkey=error` umgehen.
- Ein Betreiber, der einen Schlüssel in `allowedParameters` aufnimmt, gibt die
  Plangrenze für diesen Schlüssel auf. Deshalb steht in den mitgelieferten
  Definitionen nur `storageSize` darin und nicht `instances` oder `replicas`:
  eine Größe zu verhandeln ist etwas anderes, als die Topologie eines Plans zu
  umgehen.

## Abgrenzung

Die Entscheidung sagt nichts darüber, ob ein Operator eine Änderung überhaupt
annimmt. CloudNativePG lässt Speicher wachsen, aber nicht schrumpfen; ein
`PATCH` auf einen kleineren Wert wird vom Broker angewendet und vom Operator
abgelehnt. Der Grund erscheint dann in `last_operation` — siehe
[known-issues.md](../known-issues.md) zur Frage, wie lange das dauern darf.
