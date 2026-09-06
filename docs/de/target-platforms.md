# Zielplattformen

> [English](../en/target-platforms.md) · Führende Fassung: deutsch

Der Broker wird für die folgenden Systeme gebaut.

| Rolle | System | Bedeutung |
|---|---|---|
| **Zielplattform** | produktives Cloud Foundry | wofür der Broker gebaut wird |
| **Zielplattform** | Tanzu TAS | dito |
| **Zielplattform** | externe Marketplaces mit OSB-Anbindung | dito |
| **Entwicklungsplattform** | echtes Cloud Foundry auf kind | Testgerät, kein Zielsystem |

**Die Unterscheidung ist nicht kosmetisch, aber sie verläuft anders, als man
denkt.** Die Entwicklungsplattform fährt `cloud_controller_ng` — dieselbe
Software wie ein Zielsystem. Was sie über das **Protokoll** sagt, gilt deshalb
auch dort: Katalog, Zusagen, Fehlercodes, der Update-Pfad.

Was sie **nicht** sagen kann, ist alles über den Betrieb. Der Broker liegt dort
zufällig im selben Kubernetes-Cluster wie die Plattform; auf einem Zielsystem
läuft er getrennt ([ADR 0009](adr/0009-deployment-model.md)). Ziel-Namespace,
Vertrauensanker und Netzweg bleiben damit offen.

Wer einen Nachweis von der Entwicklungsplattform auf ein Zielsystem überträgt,
muss sagen, in welche der beiden Hälften er fällt.

## Was auf allen Plattformen gleich ist

Die Kopplung ist die **Open-Service-Broker-API 2.17** und sonst nichts. Der
Broker läuft als gewöhnliches Kubernetes-Deployment; jede Plattform *konsumiert*
dieselbe URL. Es gibt keinen plattformspezifischen Code im Broker, und es soll
keinen geben — die Begründung steht in [ADR 0006](adr/0006-platform-independence.md).

Gleich ist damit überall:

- der Katalog unter `GET /v2/catalog` und das, was daraus im Marketplace wird,
- der Lebenszyklus `provision → bind → unbind → deprovision`,
- die Registrierung als Broker mit URL und Basic-Auth-Zugangsdaten,
- das Freischalten der Pläne, bevor Nutzer sie sehen,
- die `X-Broker-API-Version`-Aushandlung.

Ein Broker, der gegen OSB 2.17 konform ist, funktioniert auf allen vier. Genau
deshalb ist Konformität kein Selbstzweck, sondern das ganze Produkt.

## Was sich unterscheidet

Die Unterschiede liegen nicht in der API, sondern drumherum — und sie sind der
Grund, warum ein Erfolg auf der Entwicklungsplattform noch keiner auf TAS ist.

| Thema | Entwicklung (CF auf kind) | produktives CF / TAS |
|---|---|---|
| Registrierung | `cf create-service-broker` | dasselbe |
| Erreichbarkeit | clusterinterner Service-DNS-Name | der Broker muss aus dem Plattformnetz erreichbar sein — Route, Firewall, ggf. eigene App-Instanz |
| Zertifikatsvertrauen | **wird nicht geprüft** — der Cloud Controller läuft dort mit `skip_cert_verify: true` | die Plattform-Truststore-Kette; zu klären, ob eine interne CA akzeptiert wird oder ein öffentlich vertrauenswürdiges Zertifikat nötig ist |
| Plan-Sichtbarkeit | `cf enable-service-access` | dasselbe |
| Ziel-Namespace | von Hand angelegt, weil der Broker ihn aus der Space-GUID ableitet | **es gibt keinen** — Spaces sind Datensätze im Cloud Controller, keine Kubernetes-Objekte |
| Mandanten | ein kind-Cluster, ein Nutzer | echte Orgs und Spaces, echte Rechtetrennung |
| Bauform | Broker im selben Cluster wie die Plattform | Plattform auf BOSH-VMs, Broker in einem getrennten Cluster |
| Last | ein Entwickler, ein Service auf einmal | viele gleichzeitige Operationen |

**Die Bauform steht fest:** der Broker läuft als Kubernetes-Deployment im
Cluster der Operatoren, und eine Plattform erreicht ihn über eine
Netzwerkadresse ([ADR 0009](adr/0009-deployment-model.md)).

**Offen bleibt der Vertrauensanker**, und er lässt sich nur beim Betreiber des
Zielsystems klären. Drei gleichberechtigte Wege: ein Zertifikat von einer CA,
der die Plattform ohnehin traut; die Cluster-CA in ihren Vertrauensspeicher —
bei TAS ein Feld im Ops Manager, bei Cloud Foundry ein BOSH-Trusted-Certificate;
oder mTLS in beide Richtungen, wenn die Plattform Client-Zertifikate ausstellt.
Der Broker verlangt keinen bestimmten. Das ändert nichts am Code, aber alles an
der Betriebsanleitung.

### Woher der Broker sein Zertifikat bekommt

Er holt es nicht selbst. Er liest `TLS_CERT_FILE` und `TLS_KEY_FILE` von der
Platte und lädt sie alle `TLS_RELOAD_INTERVAL` neu — eine Erneuerung wirkt
ohne Neustart. Wer die Dateien schreibt, ist ihm gleich.

Im Chart schreibt cert-manager sie, und `tls.certManager.issuerRef` zeigt auf
**einen beliebigen** Issuer. Ein ACME-Issuer funktioniert damit ohne
Codeänderung:

```yaml
tls:
  certManager:
    issuerRef: {kind: ClusterIssuer, name: acme-dns01}
    duration: ""          # ACME: der Server bestimmt die Laufzeit
    renewBefore: ""       # sonst ungültig, sobald er kürzer ausstellt
    dnsNames:
      - osb-broker.svc.example.com
```

Drei Dinge sind dabei zu beachten:

1. **`dnsNames` ist der Name, unter dem die Plattform den Broker erreicht** —
   nicht der Service-DNS-Name im Cluster. Cloud Foundry prüft das Zertifikat
   gegen die URL aus `cf create-service-broker`. Bei ACME muss es außerdem ein
   Name sein, für den das ACME-Konto autorisiert ist.
2. **HTTP-01 scheitert an einem internen Broker.** Die Challenge verlangt, dass
   der ACME-Server `http://<name>/.well-known/acme-challenge/…` erreicht. Für
   einen Broker hinter der Firewall geht das nicht — **DNS-01** braucht nur die
   API des DNS-Anbieters.
3. **`duration` und `renewBefore` leer lassen.** Bei ACME bestimmt der Server
   die Laufzeit; eine Vorgabe im `Certificate` gilt dort nicht, und ein
   `renewBefore`, das länger ist als die tatsächliche Laufzeit, macht das
   `Certificate` ungültig.

Damit ist auch der [Vertrauensanker](adr/0009-deployment-model.md) auf Weg 1
gelöst: ein ACME-Zertifikat kommt von einer CA, der die Plattform ohnehin
traut.

## Externe Marketplaces

Der dritte Zielfall ist der, bei dem der Broker nicht bei Cloud Foundry
registriert wird, sondern bei einer beliebigen Plattform, die die OSB-API
spricht. Für den Broker ist das derselbe Fall wie CF: ein Konsument, der
`/v2/catalog` liest und den Lebenszyklus fährt.

Praktisch heißt das dreierlei.

**Erstens zählt der Katalog mehr als bei Cloud Foundry.** Er ist das Einzige,
was ein Marktplatz vom Broker sieht, bevor er ihn benutzt — und was dort nicht
steht, benutzt niemand. Jedes Angebot und jeder Plan trägt deshalb einen
`metadata`-Block mit `displayName`, `longDescription` und den Links auf
Dokumentation und Support; jeder Plan sagt ausdrücklich, ob er kostenlos ist,
und nennt seine Pollfrist. Was der Broker kann und anmeldet, steht in
[reference/osb-api.md](reference/osb-api.md).

**Zweitens sind Felder, die Cloud Foundry großzügig ignoriert, hier
möglicherweise Pflicht** — etwa ein echter `dashboard_url` statt des heute fest
verdrahteten `https://dashboard.example.com/instances/<id>`.

**Drittens ist `cmd/osb-gate` das einzige Werkzeug, das diesen Fall überhaupt
prüft**; es ersetzt hier die Plattform. Zwei seiner Prüfungen zielen genau
darauf: `catalog-display` sagt, was ein Marktplatz anzeigen könnte und heute
nicht kann, und `catalog-promises` hält jede Zusage des Katalogs gegen das
Verhalten. Eine Zusage, die der Broker nicht hält, fällt sonst erst dem
Anwender auf.

## Verifikationsstand

**Verifiziert ist echtes Cloud Foundry auf kind** — `cloud_controller_ng`,
UAA, Diego, gorouter. Was dort nachgewiesen ist:

| Nachweis | Ergebnis |
|---|---|
| OSB-2.17-Lebenszyklus über HTTP | Integrationstest deckt catalog → provision → last_operation → bind → unbind → deprovision ab |
| Registrierung, Marketplace, `cf create-service` | gegen echtes Cloud Foundry |
| Parameter-Update über die Plattform | `cf update-service -c` erreicht den Broker als `PATCH`, und die Ressource des Operators ändert sich wirklich |
| Nicht zugesagter Planwechsel | der Cloud Controller lehnt selbst ab (`ServicePlanNotUpdateable`), ohne den Broker zu fragen — die plan-genaue Zusage wird also gelesen |
| Upgrade-Pfad vollständig | ein Plan mit `maintenanceInfo` erscheint als `upgrade available`, `cf upgrade-service` löst aus, danach steht die Instanz auf dem neuen Stand |
| Generic Engine Ende zu Ende | `cf create-service cnpg-postgresql large` erzeugt einen echten CloudNativePG-Cluster (3 Instanzen, 10Gi); `psql` im Pod antwortet, Credentials aus dem Operator-Secret |
| Mehrteiliges Manifest Ende zu Ende | `cnpg-pgvector` legt Cluster **und** `Database` an; `status.extensions[vector].applied=true`, in der Datenbank `pg_extension` = `vector 0.8.1` auf PostgreSQL 18.6, ein `<->`-Vergleich liefert den Nachbarn |
| Neustart-Persistenz | Instanzen und Bindings überleben Kill und Rescheduling |
| Asynchrones Provision | `202` mit `operation`, `last_operation` meldet `in progress` bis der Operator fertig ist, danach `succeeded`; ohne `accepts_incomplete=true` antwortet der Broker `422 AsyncRequired` |
| Binding-Lebenszyklus vollständig | Bind `201`, Wiederholung `200` mit denselben Zugangsdaten, `GET binding` `200`, Unbind `200`, Unbind einer unbekannten Binding `410` |
| Ein Pfad, kein Rückfall | der Katalog besteht ausschließlich aus ServiceDefinitions; eine unbekannte `service_id` ist `400` und läuft nicht in eine zweite Implementierung |
| Statuscodes aus Fehlerwerten | ein unbekannter Plan ist auch auf einem `DELETE` `400` und nicht `410` — die Zuordnung hängt nicht mehr an Formulierungen |
| Secret-Name aus `status.binding.name` | der RabbitMQ-Operator meldet `osb-<id>-default-user`, der Broker nutzt ihn ohne Namenstemplate |
| Zielform per `mapping` | das Binding enthält genau `host, password, port, provider, type, uri, username`; `default_user.conf` und `connection_string` bleiben draußen |
| Spec-konformes Secret | Typ `servicebinding.io/rabbitmq`, Labels für Instanz und Binding, `OwnerReference` auf den `RabbitmqCluster` |
| Aufräumen beim Unbind | `cf delete-service-key` entfernt das projizierte Secret |
| Konformität gegen den CRD-Store | 24 von 24 im kind-Cluster gegen echtes RBAC, über das Helm-Chart ausgerollt |
| Sichtbarkeit des Zustands | `kubectl get osbi` und `osbb` zeigen Instanz und Binding mit Service, Plan und Ready |
| Credentials getrennt | nicht im Binding-CR, sondern in einem Secret mit `OwnerReference` darauf |
| Kontext vollständig | `platform`, `spaceGuid` und `organizationGuid` werden abgebildet |
| Konformität über HTTPS mit Client-Zertifikat | 24 von 24 — und 24 von 24 auch mit dem Client-Zertifikat allein, ohne Basic Auth |
| Zertifikatsrotation ohne Neustart | TLS-Secret im laufenden Pod getauscht: die ausgelieferte Seriennummer wechselt, `restartCount` bleibt 0 |
| Registrierung über `https://` | `CFServiceBroker` wird ready bei `trustInsecureServiceBrokers=false` — die Plattform prüft das Zertifikat |
| Voller Lebenszyklus über HTTPS | `cf create-service` bis `cf delete-service` einschließlich echter Credentials |
| mTLS-Autorisierung | ein von derselben CA signiertes, aber nicht gelistetes Client-Zertifikat bekommt 401 |
| Probes bleiben offen | `/healthz` ohne Client-Zertifikat erreichbar |

Nicht nachgewiesen ist alles Übrige. Es gibt

- keinen Durchlauf gegen produktives Cloud Foundry,
- keinen gegen Tanzu TAS,
- keinen gegen einen externen Marketplace,
- keinen unter Last oder mit echter Mandantentrennung.

Das ist der Stand der Arbeit, nicht eine Lücke der Beschreibung.

## Woran sich die Reife misst

Diese Tabelle ist die eigentliche Arbeitsliste. Sie zeigt, welche der bekannten
Abweichungen auf der Entwicklungsplattform durchgehen und auf dem Zielsystem
nicht. Die Langfassung je Punkt steht in
[known-issues.md](known-issues.md), die Codestellen in
[reference/osb-api.md](reference/osb-api.md).

**Was die Entwicklungsplattform nicht prüfen kann.** Sie beantwortet die
Protokollfragen und keine einzige Betriebsfrage — der Broker liegt dort im
selben Cluster wie die Plattform. `cf marketplace` zeigt außerdem die
Katalogkopie des Cloud Controllers, nicht den Katalog des Brokers; nur ein
direkter `GET /v2/catalog` sagt, was der laufende Broker wirklich ausstellt.

| Abweichung | in der Entwicklung | auf produktivem CF / TAS |
|---|---|---|
| **Ziel-Namespace aus der Space-GUID** | der Namespace wird von Hand angelegt | **Ausschlusskriterium** — es gibt keine Namespaces je Space, jedes Provision scheitert |
| Vertrauensanker | ungeprüft, `skip_cert_verify: true` | die Plattform prüft das Zertifikat des Brokers wirklich |
| `seaweedfs-s3`: Readiness-Pfad nur gegen das CRD-Schema geprüft | der Operator ist nicht installiert | ein danebenliegender Pfad kostet ein Plattform-Zeitlimit je Instanz |

**Der Ziel-Namespace ist ein Ausschlusskriterium**, und er ist die einzige
offene Frage dieser Art. Der Broker leitet den Namespace der
Operator-Ressourcen aus der Space-GUID ab; auf einer Plattform, die keine
Namespaces je Space anlegt, existiert er nie. Das ist keine Nachlässigkeit,
sondern eine Entscheidung, die als [ADR 0010](adr/0010-instance-namespace.md)
zur Annahme bereitliegt: der Betreiber konfiguriert die Abbildung, die Vorgabe
ist ein fester Namespace, und der Broker legt keinen an, solange es ihm niemand
erlaubt. Gebaut ist davon nichts. Die Langfassung steht in
[known-issues.md](known-issues.md).

Alle übrigen Punkte sind funktionale Lücken und Sorgfaltsarbeit an den
Definitionen. Die Protokollschicht selbst trägt kein Ausschlusskriterium: sie
besteht aus einer Engine und N Definitionen, ohne zweiten Pfad daneben
([ADR 0003](adr/0003-replace-http-layer.md)).

## Was ein managed Dienst braucht und hier fehlt

Seit [ADR 0008](adr/0008-depth-over-breadth.md) geht die Arbeit in die Tiefe der
wenigen Dienste statt in weitere Katalogeinträge. Diese Tabelle ist die Liste
dazu — nicht Aufgaben mit Terminen, sondern die Lücke, so wie sie ist. Jeder
Punkt wiegt für einen Betreiber schwerer als ein vierter Dienst im Marketplace.

| | Stand |
|---|---|
| **Kontingente** | ✅ `parameterLimits` je Plan, durchgesetzt auf `PUT` und `PATCH` und als OSB-Plan-Schema im Katalog veröffentlicht |
| **Löschschutz** | ✅ `retainOnDeprovision` je Plan; die Instanz wird aufgegeben, die Daten bleiben und tragen `osb.io/retained-instance` |
| **Bestandsübersicht** | ✅ `osb_active_instances{service_id,plan_id}` — welches Angebot wie oft genutzt wird |
| **Auffindbarkeit im Marktplatz** | ✅ `metadata` je Angebot und Plan, `free`, `maximum_polling_duration`, `instances_retrievable`, `bindings_retrievable` — und `catalog-promises` hält jede Zusage gegen das Verhalten |
| **Planwechsel** | offen, und zwar bewusst: der Broker kann ihn, sagt ihn aber für keine Definition zu und lehnt ihn mit `422` ab. Er ist nur in eine Richtung sicher — CloudNativePG lässt Speicher wachsen, nicht schrumpfen —, und ein Katalogflag kennt keine Richtung |
| **Sicherung und Wiederherstellung** | offen. CloudNativePG kann es (`Backup`, `ScheduledBackup`, Barman) — der Broker bietet es weder als Planmerkmal noch als Service-Key an |
| **Point-in-Time-Recovery beim Provision** | offen; eine Instanz entsteht immer leer |
| **Upgrades bestehender Instanzen** | ✅ `maintenanceInfo` je Plan: der Katalog nennt den Stand, die Instanz trägt den angewendeten, `cf upgrade-service` löst aus. Der Besitzer entscheidet, der Broker ändert nichts ungefragt |
| **Verhalten unter Last, echte Mandantentrennung** | ungeprüft — siehe Verifikationsstand |

**Was der Broker bewusst nicht misst: die Gesundheit der Dienste.** Ein Broker,
der Postgres-Metriken nachbaut, dupliziert, was CloudNativePG und der
RabbitMQ-Operator selbst exportieren, und kostete je Scrape einen API-Aufruf
pro Instanz. Er misst, was nur er weiß — den OSB-Bestand; die Gesundheit misst
der Operator, der den Dienst betreibt.

**Woran die drei offenen Punkte jeweils hängen** — und das ist nicht dasselbe:

- **Sicherung und PITR** hängen am Zielsystem-Durchlauf. Dort entscheidet sich,
  wohin Sicherungen gehen und wer sie verwaltet; die Broker-Seite ist ohne
  diese Antwort nicht sinnvoll zu entwerfen.
- **Der Planwechsel** hängt **nicht** daran. Er ist nur in eine Richtung
  sicher, und die Zusage dafür steht je Plan: `planUpdateable` gilt dem Plan,
  den eine Instanz *verlässt*. Was fehlt, ist nicht der Mechanismus, sondern
  ein Übergang, der gegen einen laufenden Operator belegt ist.
- **Last und Mandantentrennung** sind keine Funktion, sondern eine
  Messung — die braucht ein Zielsystem.
