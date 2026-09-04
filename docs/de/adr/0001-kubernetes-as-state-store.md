# ADR 0001: Kubernetes ist der einzige Zustandsspeicher

> [English](../../en/adr/0001-kubernetes-as-state-store.md) · Führende Fassung: deutsch

**Status:** angenommen · **Betrifft:** `internal/broker`, `internal/apis/v1alpha1`, `deploy/crds`

## Kontext

Ein Service Broker muss sich merken, welche Instanzen und Bindings er angelegt
hat. Ohne dieses Gedächtnis kann er nach einem Neustart weder deprovisionieren
noch idempotent antworten.

Der naheliegende Weg wäre eine Datenbank. Das widerspricht dem Zielbild: der
Broker soll ein einzelner Prozess sein, der neben den Operatoren im Cluster
läuft und ausgerollt wird, indem man ein Deployment anlegt. Eine Datenbank wäre
ein zweiter Betriebsgegenstand mit eigenem Backup, eigenem Failover und eigenem
Lebenszyklus — für ein Gedächtnis, das aus wenigen Kilobyte je Instanz besteht.

## Entscheidung

**Der Zustand liegt in Kubernetes-Objekten, nicht in einer Datenbank — und zwar
je Datensatz in einem eigenen Objekt.**

Die Ressourcenarten sind `OSBServiceInstance` und `OSBServiceBinding`, Gruppe
`broker.osb.io`, Version `v1alpha1`. Ein Provision legt ein Objekt an, ein
Deprovision entfernt es; kein Aufruf fasst den Zustand anderer Instanzen an.

Vier Anforderungen ergeben sich daraus und sind der Grund für den Zuschnitt:

- **Kein Größenlimit.** Der Zustand wächst mit der Zahl der Instanzen, nicht
  gegen eine feste Obergrenze.
- **Kein Schreiben des Gesamtzustands je Aufruf.** Ein Provision schreibt genau
  ein Objekt.
- **Konfliktbehandlung statt stillem Überschreiben.** Schreibvorgänge laufen
  über `RetryOnConflict` und ersetzen nur `.Spec`; `resourceVersion` und fremde
  Annotationen bleiben stehen.
- **RBAC, das `create` überhaupt gewähren kann.** Rechte gelten je
  Ressourcenart, nicht je Objektname — `create` lässt sich in Kubernetes nicht
  über `resourceNames` einschränken, weil beim Anlegen der Objektname im
  Request-Body steht und zum Autorisierungszeitpunkt kein Name existiert, gegen
  den geprüft werden könnte.

## Detailentscheidungen

**Credentials liegen in einem eigenen Secret, nicht im Binding-CR.** Ein CR ist
für jeden lesbar, der die Ressourcenart lesen darf; ein Secret hat eine eigene
RBAC-Ebene. Das Secret heißt `<objektname>-credentials`, trägt eine
`OwnerReference` auf das Binding und wird beim Löschen **zusätzlich** explizit
entfernt — die Garbage Collection läuft asynchron, und ein liegengebliebenes
Secret enthält echte Datenbankzugangsdaten.

**Ein fehlendes Credential-Secret beim Lesen ist kein Fehler.** Ein harter
Fehler machte das Binding unlöschbar.

**Kein Status-Subresource.** Der Broker ist kein Controller; niemand reconciled
diese Objekte. Ein Status-Subresource würde eine Trennung suggerieren, die es
nicht gibt.

**Objektnamen werden aus der OSB-ID abgeleitet**, solange diese ein gültiges
DNS-1123-Label bis 63 Zeichen ist — bei Cloud Foundry immer der Fall, weil dort
UUIDs kommen. Sonst `osb-` plus gekürzter SHA-256. Die echte ID steht immer in
`spec.id` und wird bei jedem Lesen gegengeprüft, damit eine Hash-Kollision nicht
den falschen Datensatz liefert.

**Die CRDs liegen nicht im Helm-Chart.** Clusterweite Objekte in einem
namespace-gebundenen Release kollidieren zwischen Releases, und Helm
aktualisiert das Verzeichnis `crds/` bei einem `helm upgrade` nie. Sie werden
mit `kubectl apply -f deploy/crds/` installiert.

## Konsequenzen

**Gut:**

- Der Zustand ist mit `kubectl get osbi,osbb` sichtbar — ein Betriebsvorteil,
  den keine Datenbank bietet.
- Granulares RBAC je Ressourcenart statt einer Regel auf ein einzelnes Objekt.
- Ein defekter Datensatz betrifft einen Datensatz, nicht den ganzen Bestand.

**Preis:**

- Zwei CRDs müssen vor dem ersten Start installiert sein, sonst scheitert jedes
  Provision.
- Es gibt zwei Implementierungen der Schnittstelle — die CRD-gestützte und die
  im Speicher für Tests. Beide müssen dieselbe Vertragssuite bestehen
  (`internal/broker/statestore_contract_test.go`).
- Zustand in einem anderen Format lässt sich nicht einfach einlesen; dafür gibt
  es `cmd/osb-state-migrate`.

## Verworfene Alternativen

**Ein einzelnes geteiltes Objekt für den gesamten Zustand** — eine ConfigMap mit
JSON — ist der naheliegendste Weg und scheitert an vier Stellen zugleich: das
1-MiB-Limit reicht für rund 514 Instanzen und lässt danach *jeden* Schreibvorgang
scheitern; jeder Aufruf schreibt den Gesamtzustand neu und skaliert damit gegen
die Zahl der Instanzen; zwei überlappende Provisions überschreiben sich
konfliktfrei, sobald zwischen Lesen und Schreiben die `resourceVersion` neu
geholt wird; und `create` lässt sich per RBAC nicht auf einen Objektnamen
einschränken, das Recht gilt also ohnehin für die ganze Ressourcenart.

| Option | Warum nicht |
|---|---|
| Eine ConfigMap für den gesamten Zustand | Größenlimit, Schreiben des Gesamtzustands je Aufruf, keine brauchbare Konfliktbehandlung, kein sinnvolles RBAC |
| Externe Datenbank (PostgreSQL, MySQL) | widerspricht dem Ziel „kein externer Store"; zweiter Betriebsgegenstand |
| SQLite auf einem PVC | kein Server nötig, aber eine Storage-Abhängigkeit und kein `kubectl`-Einblick |
| Viele kleine ConfigMaps oder Secrets | löst das Größenlimit, aber ohne Query-Ebene und ohne Ressourcenart-RBAC |
