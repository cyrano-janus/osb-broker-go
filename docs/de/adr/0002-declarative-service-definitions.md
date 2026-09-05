# ADR 0002: Deklarative ServiceDefinitions statt Code je Service

> [English](../../en/adr/0002-declarative-service-definitions.md) · Führende Fassung: deutsch

**Status:** angenommen · **Betrifft:** `internal/definition`, `definitions/`, `schemas/`

## Kontext

Ein Broker **je Service-Typ** — einer für PostgreSQL, einer für Redis, und so
fort — besteht zu rund 90 Prozent aus demselben Code: OSB-Endpunkte,
Zustandsverwaltung, Authentifizierung. Operator-spezifisch sind die restlichen
zehn Prozent.

Das skaliert nicht. Bei N Services entstehen N Codebasen, N Deployments, N
Sicherheitsaktualisierungen und N Stellen, an denen eine OSB-Feinheit falsch
umgesetzt sein kann.

## Entscheidung

**Eine Broker-Engine, N YAML-Definitionen — eine Codebasis, Konfiguration statt
Code.**

Ein Service wird vollständig durch eine ServiceDefinition beschrieben: das
Katalogangebot mit seinen Plänen, das Template für die anzulegenden Kubernetes-
Objekte, das Kriterium für Readiness und die Herkunft der Credentials. Kein Go-
Code je Service.

Die Felder beschreibt [service-definitions.md](../service-definitions.md), die
maschinenlesbare Quelle ist `schemas/service-definition.schema.json`.

## Die Voraussetzung, die das erst möglich macht

Der Ansatz funktioniert, weil Kubernetes-Operatoren einem wiederkehrenden Muster
folgen. Ein Operator ist anbindbar, wenn er **alle drei** Punkte erfüllt:

1. eine CRD für Service-Instanzen,
2. Credentials als Kubernetes-Secret,
3. ein Statusfeld für Readiness.

Das ist zugleich die Grenze der Entscheidung. Das Versprechen lautet nicht
„jeder Operator mit nur YAML", sondern „jeder Operator, der dem Dreier-Muster
folgt, mit nur YAML".

Wie eng diese Grenze in der Praxis ist, hält [ADR 0008](0008-depth-over-breadth.md)
fest: von sieben Definitionen sind drei geblieben, und keine der vier Absagen
hatte eine Ursache im Broker-Code. Der Mechanismus dieser Entscheidung bleibt
davon unberührt — begrenzt ist das Angebot, nicht die Bauform.

## Konsequenzen

**Gut:**

- Ein neuer Service ist eine Datei, kein Deployment.
- Ein Fehler in der OSB-Umsetzung wird einmal behoben und wirkt überall.
- Die Generizität ist belegt: zwei Operatoren mit unterschiedlichen
  CRD-Gruppen, Condition-Typen und Credential-Layouts laufen über dieselbe
  Engine, ohne dass `internal/definition` je Service etwas davon wüsste.

**Preis:**

- **Templates statt Typen.** Ein Fehler im Template fällt zur Renderzeit auf,
  nicht beim Übersetzen. Abgefedert durch `missingkey=error` und dadurch, dass
  eine nicht parsende Definition den Broker-Start abbricht statt den ersten
  Request.
- **Die Engine muss allgemeiner sein als jeder Einzelfall.** Multi-Doc-Templates,
  Zahlnormalisierung beim Vergleich, No-Op-Erkennung, drei Stufen beim
  Deprovision — all das existiert nur, weil kein Service-spezifischer Code die
  Sonderfälle abfangen darf.
- **Konventionsabhängigkeit.** Wo ein Operator sein Credential-Secret ablegt,
  lässt sich aus einem Namensschema nur raten. Die Antwort darauf ist
  [ADR 0005](0005-cncf-service-binding-spec.md).

## Verworfene Alternativen

| Option | Warum nicht |
|---|---|
| Ein Broker je Service-Typ | N Codebasen, siehe Kontext |
| Plugin-Schnittstelle in Go | wieder Code je Service, nur mit mehr Zeremonie |
| Crossplane-Compositions als Unterbau | löst ein ähnliches Problem, brächte aber eine zweite große Abhängigkeit und eine zweite Sprache für Templates |
