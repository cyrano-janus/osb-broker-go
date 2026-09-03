# ADR 0002: Deklarative ServiceDefinitions statt Code je Service

> [English](../../en/adr/0002-declarative-service-definitions.md) · Führende Fassung: deutsch

**Status:** angenommen · **Betrifft:** `internal/definition`, `definitions/`, `schemas/`

## Kontext

Die erste Fassung des Projekts hatte einen Broker **je Service-Typ**: ein
Repository für PostgreSQL, eines für Redis, und so fort. Jeder davon war zu 90
Prozent derselbe Code — OSB-Endpunkte, Zustandsverwaltung, Authentifizierung —
und zu 10 Prozent operator-spezifisch.

Das skaliert nicht. Bei N Services entstehen N Codebasen, N Deployments, N
Sicherheitsaktualisierungen und N Stellen, an denen eine OSB-Feinheit falsch
umgesetzt sein kann.

## Entscheidung

**Eine Broker-Engine, N YAML-Definitionen.**

```
v1:  ein Broker je Service-Typ           →  N Codebasen, N Deployments
v2:  eine Engine + N ServiceDefinitions  →  1 Codebasis, Konfiguration statt Code
```

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

Das ist zugleich die Grenze der Entscheidung. Von den sieben mitgelieferten
Definitionen können vier nicht binden, weil ihr Operator kein Credential-Secret
erzeugt. Das Versprechen lautet nicht „jeder Operator mit nur YAML", sondern
„jeder Operator, der dem Dreier-Muster folgt, mit nur YAML".

## Konsequenzen

**Gut:**

- Ein neuer Service ist eine Datei, kein Deployment.
- Ein Fehler in der OSB-Umsetzung wird einmal behoben und wirkt überall.
- Der RabbitMQ-Durchlauf hat die Generizität belegt: ein Operator mit anderer
  CRD-Gruppe, anderen Condition-Typen und anderem Credential-Layout brauchte
  **keine einzige** Änderung an `internal/definition`. Das war die offene Frage
  gegenüber einem Beweis mit nur einem Service, und sie ist beantwortet.

**Preis:**

- **Templates statt Typen.** Ein Fehler im Template fällt zur Renderzeit auf,
  nicht beim Übersetzen. Abgefedert durch `missingkey=error` und dadurch, dass
  eine nicht parsende Definition den Broker-Start abbricht statt den ersten
  Request.
- **Die Engine muss allgemeiner sein als jeder Einzelfall.** Multi-Doc-Templates,
  Zahlnormalisierung beim Vergleich, No-Op-Erkennung, drei Stufen beim
  Deprovision — all das existiert nur, weil kein Service-spezifischer Code die
  Sonderfälle abfangen darf.
- **Konventionsabhängigkeit.** Wo ein Operator sein Credential-Secret ablegt, war
  zunächst geraten. Die Antwort darauf ist [ADR 0005](0005-cncf-service-binding-spec.md).

## Verworfene Alternativen

| Option | Warum nicht |
|---|---|
| Ein Broker je Service (Status quo v1) | N Codebasen, siehe Kontext |
| Plugin-Schnittstelle in Go | wieder Code je Service, nur mit mehr Zeremonie |
| Crossplane-Compositions als Unterbau | löst ein ähnliches Problem, brächte aber eine zweite große Abhängigkeit und eine zweite Sprache für Templates |
