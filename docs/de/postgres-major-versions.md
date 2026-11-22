# PostgreSQL: drei Major-Versionen zur Wahl

> [English](../en/postgres-major-versions.md) · Führende Fassung: deutsch

Das Angebot `cnpg-postgresql` führt drei Major-Versionen gleichzeitig. Der
Besitzer einer Instanz wählt eine davon und wechselt, wann er will. Diese
Seite sagt, was dabei gilt, wer was entscheidet, wie das Fenster wandert und
was ein Wechsel kostet. Die Entscheidung dahinter steht in
[ADR 0012](adr/0012-postgres-version-window.md).

## Was gilt

| Frage | Antwort |
|---|---|
| Welche Versionen | die neueste Major und die zwei davor, heute **17, 18, 19** |
| Vorgabe | die zweitneueste, heute **18** |
| Wie wählen | `cf create-service cnpg-postgresql small db -c '{"majorVersion":"19"}'` |
| Wie wechseln | `cf update-service db -c '{"majorVersion":"19"}'` — nur aufwärts |
| Wer pflegt die Nebenversion | der Betreiber, rollierend, ohne Zutun des Besitzers |
| Was `cnpg-pgvector` tut | bleibt an PostgreSQL 18 gebunden, solange es kein pgvector-Image für 19 gibt |

Die Grenze steht als `oneOf` in der Definition und damit als `enum` im
Plan-Schema des Katalogs. Eine Major außerhalb des Fensters ist `400`:

```
parameter "majorVersion" in plan "small" must be one of [17, 18, 19], got "16"
```

## Wer entscheidet was

Die Definition nennt nur die **Major**. Welches Image dahintersteht, sagt ein
`ClusterImageCatalog` namens `postgresql`, den der Betreiber der Plattform
pflegt — auf der Entwicklungsplattform liegt er in
`cfk8s-platform/services/cloudnative-pg/image-catalog.yaml`, die Tags stehen
in deren `versions.env`.

| Sache | Entscheidet | Mechanismus | Wirkung auf laufende Instanzen |
|---|---|---|---|
| Major einer Instanz | ihr Besitzer | Parameter `majorVersion` | Upgrade an Ort und Stelle, rund eine Minute Stillstand |
| Nebenversion je Major | Betreiber der Plattform | Katalogeintrag ändern | rollierender Neustart aller Cluster dieser Major |
| Fenster und Vorgabe | Betreiber des Brokers | `oneOf` und `params` in der Definition, `maintenanceInfo` steigt | `upgrade available`, der Besitzer löst aus |
| Operator-Version | Betreiber der Plattform | `CNPG_VERSION` | Neustart jedes Instanz-Pods, gemessen: unter einer Minute |

Die Richtung ist die aus [ADR 0011](adr/0011-broker-operator-boundary.md):
der Broker besitzt das Protokoll und reicht den Wunsch des Besitzers als
Feld an den Operator durch. Das Upgrade selbst führt CloudNativePG aus.

## Wie das Fenster wandert

PostgreSQL erscheint einmal im Jahr als neue Major und wird fünf Jahre
gepflegt. Das Fenster folgt dem so:

1. **Eine neue Major N erscheint.** Sobald ghcr.io ein `-system-trixie`-Image
   dafür trägt, bekommt der Katalog einen Eintrag `N`.
2. **Die Definition öffnet N und schließt N-3.** `oneOf` wird `[N-2, N-1, N]`,
   die Vorgabe wird N-1, `maintenanceInfo.version` steigt um eine Minor.
   Beides in einem Commit, denn getrennt wäre der Katalog einen Moment lang
   größer oder kleiner als das Fenster.
3. **Bestehende Instanzen auf N-3 laufen weiter.** Nichts zieht sie nach.
   Der Katalogeintrag N-3 bleibt stehen, solange eine Instanz ihn nutzt —
   ohne ihn bekäme sie keine Nebenversion mehr, und das wäre eine stille
   Strafe. Gestrichen wird er, wenn der Bestand (`osb_instances` nach Plan)
   keine mehr zählt.
4. **Instanzen auf der alten Vorgabe sehen `upgrade available`.** Wer die
   Major nie gesetzt hat, folgt mit `cf upgrade-service` der neuen Vorgabe —
   das ist ein Major-Wechsel mit Stillstand, und die `description` des
   Standes sagt das. Wer sie gesetzt hat, behält sie; `cf upgrade-service`
   rendert ihn mit seinen Parametern neu und ändert nichts.

Das gibt jedem Stand rund zwei Jahre, in denen er wählbar ist, und mindestens
drei, in denen er läuft. Wer die Frist reißt, bekommt keine neue Instanz auf
dem alten Stand mehr — mehr nicht.

## Was ein Major-Wechsel kostet

Gemessen am 2026-11-22 auf der Entwicklungsplattform, CloudNativePG 1.30.2,
über `cf update-service`:

| Instanz | Daten | Instanzen | Sprung | Stillstand bis zum Primary | bis alle Replikas stehen |
|---|---|---|---|---|---|
| small | 16 MB | 1 | 17 → 18 | 51 s | — |
| small | 16 MB | 1 | 18 → 19 | 51 s | — |
| small, 5Gi | 1,5 GB | 1 | 17 → 18 | 56 s | — |
| large | 456 MB | 3 | 17 → 18 | 50 s | 94 s |

Der Upgrade-Job selbst läuft in jedem Fall rund 30 Sekunden; der Rest ist
Herunterfahren und Hochkommen. Zum Vergleich die beiden Vorgänge, die keinen
Major-Wechsel bedeuten, am selben Tag gemessen: eine Bestandsinstanz, die
noch ohne Katalog lief, kam per `cf upgrade-service` auf den Katalogeintrag
ihrer Major (18.4 → 18.7) mit **17 Sekunden** Neustart; ein Update des
Operators (1.30.0 → 1.30.2) startete jeden Instanz-Pod neu, bei drei
Instanzen rollierend mit Switchover, in unter einer Minute. Die Zeilenzahl war danach in jedem Fall die
von vorher, die Planerstatistiken waren erhalten (`pg_stats` gefüllt), die
Zählerstatistiken (`pg_stat_user_tables`) standen auf null.

Was dabei geschieht, in der Reihenfolge des Operators: alle Pods des Clusters
werden beendet, ein Job führt `pg_upgrade --link` auf dem Datenverzeichnis
des Primary aus, der Primary startet mit dem neuen Image, und die Replikas
werden **neu geklont** — ihre Datenträger sind nach dem Upgrade nicht mehr
gültig und werden verworfen.

## Was bei 20 Terabyte anders ist

Die Frage, ob die Daten migriert werden müssen, hat zwei Antworten.

**Die Daten selbst: nein.** `pg_upgrade --link` kopiert keine Tabellendaten.
Es legt für jede Datei einen zweiten Verzeichniseintrag an und schreibt nur
die Systemkataloge neu. Die Dauer hängt an der Zahl der Objekte, nicht an
ihrer Größe — zwischen 16 MB und 1,5 GB lagen oben fünf Sekunden. Für 20 TB
auf einer Instanz ist deshalb mit Minuten zu rechnen, nicht mit Stunden. Das
ist nicht gemessen; die Messung hier endet bei 1,5 GB.

**Die Replikas: ja, und das ist der Posten.** Jede Replika wird nach dem
Upgrade per `pg_basebackup` vom Primary neu geklont. Bei 456 MB dauerte das
44 Sekunden für zwei Replikas; bei 20 TB sind es Stunden, über das Netz, und
in dieser Zeit läuft der Primary **ohne Failover-Schutz**. Ein Plan mit drei
Instanzen und 20 TB braucht deshalb ein Wartungsfenster, das der Besitzer
kennt — und das steht heute nirgends im Katalog.

**Die Anwendung: das ist die eigentliche Migration.** Was ein Major-Wechsel
an der Anwendung verlangt, sind Treiber, Erweiterungen und abgekündigte
Funktionen. Dafür ist das Fenster da: der Besitzer prüft seine Anwendung
gegen eine neue Instanz auf N und wechselt die bestehende, wenn sie besteht.

**Was vor jedem Wechsel fehlt: eine Sicherung.** CloudNativePG empfiehlt sie
ausdrücklich, und der Broker bietet sie nicht an — siehe
[target-platforms.md](target-platforms.md). Fällt das Upgrade fehl, setzt der
Betreiber die Major zurück; der Operator räumt den Job weg und startet die
alte Version, die Daten sind nicht angefasst. Das ist der dokumentierte Weg
des Operators und hier nicht gemessen.

## Was noch fehlt

- **Cloud Foundry meldet `update succeeded`, bevor die Datenbank wieder
  da ist.** Der Broker beantwortet das Update synchron, sobald das Objekt
  geschrieben ist; gemessen stand die Datenbank danach noch 45 Sekunden.
  Ein asynchrones Update mit `last_operation` wäre der Weg des Protokolls.
- **Kein Wartungsfenster im Katalog.** Die `description` des Standes nennt
  „rund eine Minute"; für große Cluster mit Replikas stimmt das nicht.
- **`cnpg-pgvector` hat kein Fenster**, bis es ein pgvector-Image für 19
  gibt. Die Definition sagt das im Katalog.
