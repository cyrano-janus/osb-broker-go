# ADR 0004: TLS und mTLS im Broker, kein OAuth2

> [English](../../en/adr/0004-tls-and-mtls-no-oauth2.md) · Führende Fassung: deutsch

**Status:** angenommen · **Betrifft:** `internal/server`, `internal/auth`, `internal/config`, `deploy/helm`

## Kontext

Der Broker liest produktive Datenbankzugangsdaten aus Secrets und gibt sie an
die Plattform weiter. Wer den Datenverkehr mitliest, hat damit die Zugangsdaten
jeder Instanz. Unverschlüsselter Transport ist für diesen Prozess also keine
Option, und Basic Auth allein reicht als Nachweis nicht überall aus.

## Entscheidung

**Der Broker terminiert TLS selbst und unterstützt mTLS gleichrangig neben Basic
Auth. OAuth2 fällt raus.**

### TLS im Broker, nicht davor

Ein Sidecar oder ein Ingress-Terminator würde das Problem verschieben, nicht
lösen: der letzte Sprung zum Broker bliebe unverschlüsselt, und der Broker wäre
auf eine Umgebung angewiesen, die ihm etwas vorschaltet. Er soll ohne solche
Voraussetzungen betreibbar sein.

Der Prozess betreibt einen `http.Server` mit gesetzten Zeitschranken und
Graceful Shutdown auf SIGTERM.

### Zertifikatsrotation durch Polling, nicht durch inotify

Der `CertReloader` liest Zertifikat, Schlüssel und Client-CA turnusmäßig neu und
vergleicht SHA-256-Summen.

**Der Grund ist eine Eigenheit von Kubernetes:** ein Secret wird über einen
atomar getauschten `..data`-Symlink in den Pod eingeblendet. Ein inotify-Watch
auf dem Blattpfad verstummt nach dem ersten solchen Tausch — er beobachtet dann
eine Datei, die es nicht mehr gibt. Polling ist hier die robustere und, bei
einem Intervall von 30 Sekunden, ausreichend billige Lösung.

Ein fehlgeschlagener Reload lässt das vorherige Material gültig und wird nur
geloggt. Ein kaputtes neues Zertifikat darf einen laufenden Broker nicht
umbringen.

Das Chart trägt bewusst **keine** Prüfsummen-Annotation auf dem Pod-Template:
ein neues Zertifikat soll gerade **keinen** Neustart auslösen. Der Nachweis, dass
das funktioniert, ist ein `restartCount` von 0 nach einer Rotation.

### mTLS als gleichrangige Methode

Die Authenticator-Kette kennt Basic und mTLS nebeneinander. Der Vertrag ist
dreiwertig: erfolgreich, keine Zugangsdaten vorgelegt (die Kette macht weiter),
ungültige Zugangsdaten (die Kette merkt es sich und macht trotzdem weiter). Erst
am Ende wird entschieden.

Drei Feinheiten, die im Betrieb zählen:

- **Die Fehlerantwort ist für alle Fehlerarten identisch.** Welche Methode
  gescheitert ist, würde einem nicht authentifizierten Aufrufer verraten, welche
  Methoden überhaupt aktiv sind.
- **Die Allowlists sind Autorisierung, nicht Authentifizierung.** Sind
  `MTLS_ALLOWED_CNS` und die verwandten Listen leer, wird jedes Zertifikat
  akzeptiert, das die CA signiert hat. Jede Liste prüft nur ihr eigenes Feld.
- **`MTLS_REQUIRE=true` wirkt nur, wenn mTLS die einzige Methode ist.** Sonst
  wird auf `VerifyClientCertIfGiven` heruntergestuft und gewarnt — andernfalls
  könnte sich niemand mehr per Basic Auth anmelden. `RequireAnyClientCert` wird
  nie verwendet: es verlangt ein Zertifikat, ohne es zu prüfen, und ist damit
  Sicherheitstheater.

Mit erzwungenem mTLS müssen auch die Kubernetes-Probes umgestellt werden; das
Chart schaltet sie dann auf `tcpSocket`.

### Cipher Suites bleiben offen

Nicht festgelegt. Die Go-Vorgaben sind aktueller als jede eingefrorene Liste im
Repo, und eine solche Liste altert genau so lange unbemerkt, bis sie zum Problem
wird. `TLS_MIN_VERSION` ist konfigurierbar, Vorgabe 1.2.

### OAuth2 fällt raus

Der Broker muss ohne Identity Provider betreibbar sein. OAuth2 brächte eine
Laufzeitabhängigkeit auf ein System, das im Zielkontext nicht garantiert
vorhanden ist, und Basic Auth über TLS plus mTLS deckt ab, was Cloud Foundry und
TAS von einem Broker verlangen.

## Konsequenzen

**Gut:**

- Zugangsdaten sind auf dem Weg geschützt, ohne Vorschaltsystem.
- Zertifikatswechsel ohne Neustart, nachgewiesen.
- `internal/auth`, `internal/server` und `internal/config` sprechen `net/http`
  statt gin und überleben damit einen Austausch der HTTP-Schicht
  ([ADR 0003](0003-replace-http-layer.md)) unverändert.

**Preis:**

- Ein Zertifikat muss von irgendwoher kommen. Das Chart nutzt cert-manager und
  verlangt einen Aussteller — mit den Vorgaben rendert es deshalb nicht.
- Die Plattform muss dem Aussteller vertrauen. Auf der Entwicklungsplattform ist
  das über eine eigene CA gelöst; wie es auf einer Zielplattform aussieht, ist
  offen — siehe [target-platforms.md](../target-platforms.md).
- Die Zertifikatsdateien werden mit Modus `0440` eingehängt. Ohne `fsGroup` im
  `podSecurityContext` kann der nonroot-Prozess sie nicht lesen.
- Der CI-Job `L2b` ist der einzige, der das Helm-Chart überhaupt ausführt.
