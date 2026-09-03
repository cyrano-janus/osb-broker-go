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

**Der Zustand liegt in Kubernetes-Objekten, nicht in einer Datenbank.**

Umgesetzt wurde das in zwei Stufen:

1. **Zuerst als ConfigMap.** Ein einzelnes Objekt, das den gesamten Zustand als
   JSON trug. Einfach und für einen Beweis ausreichend.
2. **Seit Phase 5 als eigene Ressourcenarten** — je Datensatz ein
   `OSBServiceInstance` beziehungsweise ein `OSBServiceBinding`, Gruppe
   `broker.osb.io`, Version `v1alpha1`.

Die zweite Stufe war kein Geschmacksurteil. Die ConfigMap hatte drei konkrete
Mängel:

- **Das 1-MiB-Limit** von ConfigMaps reicht für etwa 514 Instanzen. Danach
  scheitert jeder weitere Schreibvorgang, und zwar der *gesamte* Zustand auf
  einmal.
- **Jeder Aufruf schrieb den kompletten Zustand neu.** Das skaliert nicht mit
  der Zahl der Instanzen, sondern gegen sie.
- **Schreibvorgänge gingen still verloren.** Es gab keinen Mutex, und `save`
  las die `resourceVersion` frisch, statt sie aus dem `load` zu übernehmen. Zwei
  überlappende Provisions überschrieben sich konfliktfrei. In der CI fiel das
  nie auf, weil dort `STORE_BACKEND=memory` gesetzt war — der ConfigMap-Store
  wurde dort **nie** ausgeführt.

Zusätzlich verlangte die ConfigMap eine RBAC-Regel, die Kubernetes so nicht
gewährt: `create` lässt sich nicht über `resourceNames` einschränken, weil beim
Anlegen der Objektname im Request-Body steht und zum Autorisierungszeitpunkt
noch kein Name existiert, gegen den geprüft werden könnte.

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

- Kein Größenlimit mehr, kein Schreiben des Gesamtzustands je Aufruf.
- Schreibkonflikte werden über `RetryOnConflict` sauber aufgelöst.
- Der Zustand ist mit `kubectl get osbi,osbb` sichtbar — ein Betriebsvorteil,
  den keine Datenbank bietet.
- Granulares RBAC je Ressourcenart statt einer Regel auf ein einzelnes Objekt.

**Preis:**

- Zwei CRDs müssen vor dem ersten Start installiert sein, sonst scheitert jedes
  Provision.
- Der Umstieg brauchte ein Migrationswerkzeug (`cmd/osb-state-migrate`), und das
  musste die alten Strukturen **neu deklarieren**: die früheren Typen hatten
  keine JSON-Tags, während der eingebettete Context welche hatte. Mit den
  heutigen Typen gelesen käme lautlos ein leerer Datensatz heraus.
- Der In-Memory-Speicher bleibt als zweite Implementierung bestehen. Beide
  müssen dieselbe Vertragssuite bestehen
  (`internal/broker/statestore_contract_test.go`).

## Verworfene Alternativen

| Option | Warum nicht |
|---|---|
| Externe Datenbank (PostgreSQL, MySQL) | widerspricht dem Ziel „kein externer Store"; zweiter Betriebsgegenstand |
| SQLite auf einem PVC | kein Server nötig, aber eine Storage-Abhängigkeit und kein `kubectl`-Einblick |
| Viele kleine ConfigMaps oder Secrets | löst das Größenlimit, aber ohne Query-Ebene und ohne Ressourcenart-RBAC |
