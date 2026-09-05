# ADR 0008: Tiefe statt Breite — der Katalog wächst entlang eines Bedarfs

> [English](../../en/adr/0008-depth-over-breadth.md) · Führende Fassung: deutsch

**Status:** angenommen · **Betrifft:** das Angebot, nicht die Engine

## Kontext

Der Broker stellt Software Dritten als *managed Service* bereit. Ob ein Dienst
in einen Katalog gehört, entscheidet sich deshalb an drei Fragen, und alle drei
liegen **außerhalb** des Brokers:

1. Erlaubt die Lizenz die Bereitstellung als managed Service?
2. Führt der Operator einen auswertbaren Status im CR?
3. Erzeugt der Operator das Credentials-Secret **selbst**?

Von sieben mitgelieferten ServiceDefinitions haben vier eine dieser Fragen mit
Nein beantwortet: Redis und Redpanda an Frage 1 (RSALv2/SSPLv1 und BSL 1.1
untersagen genau diesen Anwendungsfall), MinIO an einem aufgegebenen Projekt,
Valkey an Frage 3. Redis scheitert zusätzlich an Frage 2 — sein CRD führt einen
`status` ohne jede Eigenschaft.

**Der Katalog nennt damit drei Angebote, von denen zwei Ende zu Ende gegen einen
laufenden Operator belegt sind.** Keine dieser Absagen hat eine Ursache im
Broker-Code. Eine mächtigere Engine hätte an keiner etwas geändert.

Daraus folgt eine Einsicht über die Zusage „ein neuer Service ist eine
YAML-Datei": sie stimmt und ist trotzdem kein Versprechen, das der Broker
allein einlösen kann. Die Datei ist schnell geschrieben — ob daraus ein
anbietbarer Dienst wird, entscheiden Lizenzgeber und Operator-Autoren.

## Entscheidung

**Der Katalog wächst nur noch entlang eines benannten Bedarfs.** Ein Dienst
kommt hinzu, wenn beides zutrifft:

1. **Eine konkrete Last verlangt ihn.** Nicht „wäre gut zu haben", sondern ein
   Team mit einem Anwendungsfall. Ein Katalogeintrag kostet dauerhaft einen
   Operator im Cluster, RBAC, eine Definition und einen Readiness-Pfad; einer
   ohne Besteller ist Wartungslast ohne Gegenwert.
2. **Er erfüllt die drei Kriterien oben.** Geprüft wird vor dem Schreiben der
   Definition, nicht danach.

Die Arbeit geht stattdessen in die **Tiefe der wenigen Dienste**: was ein
managed Dienst braucht und heute fehlt, steht in
[target-platforms.md](../target-platforms.md).

## Konsequenzen

**Gut:**

- Was im Katalog steht, ist rechtssicher bestellbar und funktioniert. Ein
  Angebot, das ein Betreiber nicht anbieten darf, ist schlechter als keines.
- Die Prüfkriterien sind explizit und stehen **vor** der Arbeit. Vier
  Definitionen wurden geschrieben, bevor jemand Frage 1 oder 3 gestellt hat.
- Der Aufwand richtet sich auf das, was einem Betreiber wirklich fehlt —
  Sicherung, Wiederherstellung, Upgrades, Kontingente, Verhalten unter Last.
- Die Konformitätsprüfung gewinnt an Bedeutung: unter diesem Ziel ist die
  OSB-Oberfläche die Produktgrenze, nicht die Zahl der Angebote.

**Preis:**

- **Die Erzählung wird unscheinbarer.** „Generische Broker-Engine für beliebige
  Kubernetes-Operatoren" klingt nach Plattformprodukt, „drei Dienste, gut
  betrieben" nach Betrieb. Wer Sichtbarkeit an der ersten Erzählung festmacht,
  verliert etwas.
- **Auf einen neuen Bedarf lautet die Antwort „noch nicht".** Dagegen
  zu halten ist, dass sie das auch ohne diese Entscheidung tut: die
  Redpanda-Definition durfte nicht angeboten werden, die Valkey-Definition
  konnte nicht binden. Der Unterschied ist, dass es hier ausgesprochen wird.
- Ein Dienst, den ein Team dringend braucht, kostet Vorlaufzeit für die Prüfung
  der drei Fragen.

## Abgrenzung

**Dies ist kein Widerruf von [ADR 0002](0002-declarative-service-definitions.md).**
Der Mechanismus — eine Engine plus N deklarative Definitionen — bleibt richtig
und bleibt unverändert. Er ist auch für drei Dienste die passende Bauform: kein
Code je Service, kein zweites Deployment, ein Prüfpfad für alle.

Begrenzt ist nicht die Engine, sondern was Operatoren und Lizenzen hergeben.
Diese Entscheidung ändert das **Ziel**, nicht die **Architektur**.

Ebenso unberührt: [ADR 0006](0006-platform-independence.md) — die OSB-API
bleibt die einzige Kopplung an die konsumierende Plattform, und der Maßstab für
„fertig" bleibt das Zielsystem.
