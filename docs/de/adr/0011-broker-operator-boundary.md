# ADR 0011: Der Broker besitzt das Protokoll, der Operator den Lebenszyklus

> [English](../../en/adr/0011-broker-operator-boundary.md) · Führende Fassung: deutsch

**Status:** angenommen · **Betrifft:** jede Frage der Form „soll der Broker das tun?"

## Kontext

Der Broker übersetzt OSB in Kubernetes. Auf beiden Seiten dieser Übersetzung
steht Software, die dasselbe könnte: der Broker *kann* eine Ressource
nachziehen, Zustände überwachen, aufräumen — und der Operator kann es auch, für
den Dienst, den er betreibt.

**Ohne eine Grenze wächst der Broker zu einem zweiten Operator.** Jeder Schritt
dorthin ist einzeln vernünftig. Eine Schleife, die abweichende Instanzen
nachzieht, klingt nach Sorgfalt. Ein Aufräumer für verwaiste Objekte auch. Erst
in der Summe entsteht ein Stück Software, das den Lebenszyklus von Diensten
führt, die es nicht versteht — und das zweimal tut, was der Operator schon tut,
nur schlechter, weil ihm dessen Wissen fehlt.

[ADR 0009](0009-deployment-model.md) hat entschieden, dass der Broker im
Cluster der Operatoren läuft, und daraus abgeleitet, dass er ein Controller
sein *darf*. Was er tun *soll*, hat es ausdrücklich offen gelassen. Das
entscheidet dieses ADR.

## Entscheidung

> **Der Broker besitzt das OSB-Protokoll, der Operator besitzt den
> Lebenszyklus der Ressource.**
>
> Jede Änderung, die der Broker an **fremdem** Zustand vornimmt, muss die
> Antwort auf eine Frage sein, die gerade jemand in OSB-Begriffen gestellt hat.
> **Was auch dann geschieht, wenn niemand fragt, ist Arbeit des Operators.**

**„Fremder Zustand" ist die entscheidende Einschränkung.** Was der Broker an
sich selbst tut, fällt nicht darunter: er lädt sein Zertifikat periodisch neu,
er hält seine Metriken. Beides berührt nichts außerhalb seines Prozesses und
dient nur seiner Fähigkeit zu antworten. Die Regel gilt für Operator-Ressourcen,
Secrets, Namespaces — für alles, was jemand anderes sieht.

Damit sortiert sich jeder Einzelfall von selbst:

| Was | Wer fragt? | Wessen Arbeit |
|---|---|---|
| Katalog, `free`, `plan_updateable`, Plan-Schemas | `GET /v2/catalog` | **Broker** — der Operator kennt keine Pläne |
| Parametergrenzen prüfen | `PUT`/`PATCH` | **Broker** — an der Protokollgrenze, sonst bekommt der Anwender einen Apply-Fehler statt einer Meldung |
| Readiness aus dem CR lesen | `last_operation` | **Broker** — Übersetzung, keine Entscheidung |
| Credentials aus dem Secret lesen | `PUT binding` | **Broker** — das Secret *erzeugt* der Operator |
| Zertifikat nachladen, Metriken | — | **Broker** — eigener Zustand, siehe oben |
| Bestehende Instanzen nachziehen | niemand | **Operator** |
| Verwaiste Objekte aufräumen | niemand | **Operator** |

## Konsequenzen

- **Kein Zeitgeber im Broker, der fremden Zustand ändert.** Ein Wächter hält
  den Weg zu: `TestKeinZeitgeberSchreibtOhneRequest` sucht die abgeschaffte
  Konfiguration im ganzen Quellbaum. Wer sie wiederbelebt, muss ihn zuerst
  löschen — und trifft die Entscheidung damit bewusst.
- **Upgrades laufen über das Protokoll.** `maintenanceInfo` je Plan: der
  Katalog nennt den Stand, die Plattform zeigt die Abweichung, der Besitzer
  löst aus. Der Broker rendert dann in einem Request neu, mit
  `last_operation` und einem Fehler, den jemand sieht.
- **Der Broker legt nichts an, was er nicht wieder entfernen kann.** OSB kennt
  das Löschen einer *Instanz*, nicht das Ende eines Mandanten. Deshalb erzeugt
  er keinen Namespace, solange es ihm niemand ausdrücklich erlaubt
  ([ADR 0010](0010-instance-namespace.md)), und `delete` auf Namespaces steht
  in keiner Rolle.
- **Ein neuer Dienst braucht einen Operator, der sein Credentials-Secret selbst
  erzeugt.** Das ist keine Bequemlichkeit, sondern dieselbe Regel: ein Broker,
  der Zugangsdaten *herstellt*, statt sie durchzureichen, hat den Lebenszyklus
  übernommen. An diesem Kriterium sind mehr Kandidaten gescheitert als an der
  Lizenz.
- **Was die Regel kostet, ist eine Beobachtung.** Ein periodischer Durchlauf
  meldete auch Datensätze ohne Definition und Ressourcen, die verschwunden
  sind. Diese Zahlen gibt es nicht. Ein einmaliger Prüflauf auf Abruf wäre der
  Weg, sie zurückzuholen, ohne den Zeitgeber zurückzuholen — er liefe auf
  Anfrage und fiele damit auf die richtige Seite.

## Abgrenzung

**Dies verbietet keinen Controller.** Es verbietet einen Hintergrundschreiber
*im Broker*. Wo Controller-Arbeit nötig ist, gehört sie in eine Komponente auf
der Seite des Operators — mit eigenem Lebenszyklus, eigener Begründung und
eigener Entscheidung. [ADR 0009](0009-deployment-model.md) bleibt gültig: der
Broker *darf* einer sein. Er *soll* keiner sein.

**Eine Lücke bleibt offen und ist benannt.** Stirbt der Pod zwischen dem
Anwenden der Manifeste und dem Schreiben des Datensatzes, bleibt ein verwaistes
CR zurück; das Zurückrollen läuft nur, solange der Prozess lebt. Das zu
schließen ist Controller-Arbeit, und sie steht damit auf der anderen Seite der
Grenze. Sie wartet nicht auf dieses ADR, sondern auf eine eigene Entscheidung.

**Dies sagt nichts darüber, wie viel der Broker anbietet.** Der Umfang des
Katalogs regelt [ADR 0008](0008-depth-over-breadth.md); hier geht es nur um die
Frage, wer welche Arbeit tut.
