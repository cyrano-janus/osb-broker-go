# ADR 0012: Drei Major-Versionen zur Wahl, die Nebenversion gehört dem Betreiber

> [English](../../en/adr/0012-postgres-version-window.md) · Führende Fassung: deutsch

**Status:** angenommen · **Betrifft:** `definitions/cnpg-postgresql.yaml`, der `ClusterImageCatalog` der Plattform, `internal/definition/postgres_major_test.go`

## Kontext

Eine Definition, die kein Image nennt, bekommt die Vorgabe des Operators.
Für `cnpg-postgresql` hieß das: der Katalog versprach „PostgreSQL", die
Instanz bekam, was CloudNativePG 1.30.0 mitbrachte, und mit dem nächsten
Operator-Stand hätte eine neue Instanz eine andere Version bekommen — ohne
dass eine Zeile des Katalogs sich geändert hätte. Die Major-Version einer
Datenbank ist aber die Entscheidung, an der Treiber, Erweiterungen und
abgekündigte Funktionen der Anwendung hängen. Sie gehört dem Besitzer der
Anwendung, nicht dem Zufall des Operator-Updates.

Zugleich erscheint PostgreSQL jedes Jahr als neue Major und wird fünf Jahre
gepflegt. Ein Angebot, das nur **eine** Major kennt, zwingt jeden Besitzer
zum selben Zeitpunkt zum Wechsel — oder lässt alle auf einem Stand stehen,
bis er ausläuft.

Drei Dinge sind zu entscheiden: wer die Major wählt, wer die Nebenversion
pflegt, und wie viele Majors gleichzeitig wählbar sind.

## Entscheidung

**Die Major wählt der Besitzer, aus einem Fenster von drei.** Sie ist ein
Plan-Parameter `majorVersion` mit `oneOf` über die neueste Major und die zwei
davor; die Vorgabe ist die zweitneueste. Der Wechsel ist ein gewöhnliches
`cf update-service -c` und führt zu einem Upgrade an Ort und Stelle durch
den Operator.

**Die Nebenversion pflegt der Betreiber, über den Katalog des Operators.**
Die Definition nennt nur die Major; das Image dahinter steht in einem
`ClusterImageCatalog` namens `postgresql`, den das Helm-Chart des Brokers
mitliefert. Ein geänderter Eintrag rollt alle Cluster dieser Major nach,
ohne Zutun des Besitzers und ohne Umweg über den Broker-Prozess.

**Das Fenster wandert mit maintenanceInfo.** Öffnet die Definition eine neue
Major, steigt der Stand des Plans; bestehende Instanzen sehen ein Angebot,
kein Ereignis. Wer die Major gesetzt hat, behält sie; wer die Vorgabe nahm,
folgt ihr auf eigenen Befehl.

**Ein Test hält das fest.** Jede Definition, die einen CloudNativePG-Cluster
anlegt, muss die Major ausdrücklich nennen — als Parameter über den Katalog
oder als festes Image mit Major. Eine, die es nicht tut, baut nicht.

## Konsequenzen

- **Ein Angebot statt dreier, zwei Pläne statt sechs.** Die Version ist eine
  Stellschraube im Plan, kein Plan je Version.
- **Die Lieferung bleibt in sich vollständig.** Der Katalog kommt aus dem
  Chart, nicht aus einem fremden Repositorium; die Plattform reicht Werte
  durch, wie bei den Definitionen auch. Wer den Katalog selbst stellt, sagt
  es mit `existing` — ohne eines von beiden verweigert das Chart das
  Rendern, statt dass das erste Provision am Readiness-Timeout scheitert.
- **Nebenversionen kommen ungefragt, Majors nie.** Das ist eine bewusste
  Asymmetrie: ein Sicherheitsstand ist keine Entscheidung des Besitzers, ein
  Major-Wechsel mit Stillstand ist eine.
- **Der Betreiber des Brokers hat eine jährliche Aufgabe:** Katalogeintrag,
  `oneOf`, Vorgabe und Stand in einem Commit; ein Test hält Katalog und
  `oneOf` auf derselben Liste. Wie, steht in
  [postgres-major-versions.md](../postgres-major-versions.md).
- **`cnpg-pgvector` bleibt außen vor,** solange die Erweiterung die Major
  vorschreibt und es kein Image für die neueste gibt.
- **Große Cluster mit Replikas brauchen ein Wartungsfenster,** das der Katalog
  nicht nennt: die Replikas werden nach dem Upgrade neu geklont, und das
  dauert mit der Datenmenge. Offen.

## Abgrenzung

Das Upgrade führt der Operator aus, nicht der Broker — der reicht ein Feld
durch ([ADR 0011](0011-broker-operator-boundary.md)). Was CloudNativePG dabei
tut, verspricht dieses ADR nicht; es ist gemessen und in
[postgres-major-versions.md](../postgres-major-versions.md) festgehalten.

Eine Sicherung vor dem Wechsel ist nicht Teil dieser Entscheidung. Der
Broker bietet keine an ([target-platforms.md](../target-platforms.md)), und
das bleibt so, bis ein Zielsystem sagt, wohin sie gehört.
