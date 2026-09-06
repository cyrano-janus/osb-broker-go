# ADR 0010: Der Ziel-Namespace kommt aus der Konfiguration, nicht aus einer Annahme

> [English](../../en/adr/0010-instance-namespace.md) · Führende Fassung: deutsch

**Status:** angenommen · **Betrifft:** `targetNamespace`, das Chart, die Rechte

## Kontext

Der Broker legt die Ressourcen einer Instanz in einem Kubernetes-Namespace an.
Heute leitet er ihn aus der Space-GUID ab: `targetNamespace` in
`internal/handlers/definition_instances.go` gibt `ctx.SpaceGUID` zurück, sonst
`default`.

**Das ist eine Annahme über die Plattform, und sie trägt nicht.** Sie geht auf,
wenn die Plattform je Space einen Kubernetes-Namespace mit genau diesem Namen
anlegt. Cloud Foundry tut das nicht — Spaces sind Datensätze im Cloud
Controller, keine Kubernetes-Objekte. Jedes Provision endet dort mit

```
Service broker error: apply Cluster "osb-…": namespaces "<space-guid>" not found
```

Tanzu TAS macht es noch deutlicher: dort läuft die Plattform auf BOSH-VMs, und
der Broker steht nach [ADR 0009](0009-deployment-model.md) in einem **eigenen**
Cluster. Zwischen einem CF-Space und einem Namespace dieses Clusters gibt es
keine Verbindung, die jemand hergestellt hätte.

**Was die Plattform wirklich liefert.** Cloud Foundry schickt im Provision einen
`context`-Block mit `platform`, `organization_guid`, `organization_name`,
`space_guid`, `space_name`, `instance_name` sowie `organization_annotations` und
`space_annotations` (`context_hash` in
`lib/services/service_brokers/v2/client.rb`). Die Mandanten-Identität ist also
da — nur die Abbildung auf einen Namespace fehlt, und die kann OSB nicht
liefern, weil OSB von Kubernetes nichts weiß.

**Zwei Randbedingungen, die jede Lösung einhalten muss.** Erstens trägt nur das
Provision einen Kontext; Deprovision, Bind und `last_operation` tragen keinen.
Die Abbildung ist deshalb **einmal** zu berechnen und zu speichern — das tut der
Broker schon, `Instance.Namespace`, und `instanceNamespace` löst sie in drei
Stufen wieder auf (FINDINGS #7/#16). Zweitens ist ein Namespace-Name ein
RFC-1123-Label; Org- und Space-*Namen* sind freier Text.

## Entscheidung

**Der Betreiber konfiguriert die Abbildung, der Broker wendet sie an. Zwei
Werte, beide mit einer sicheren Vorgabe:**

| Wert | Vorgabe | Wirkung |
|---|---|---|
| `instanceNamespace.template` | `osb-instances` | Go-Template über den Provision-Kontext: `.orgGUID`, `.orgName`, `.spaceGUID`, `.spaceName`, `.platform`. Ohne Platzhalter ein fester Name. |
| `instanceNamespace.create` | `false` | Legt der Broker einen fehlenden Namespace an? |

**Die Vorgabe ist ein fester Namespace, und der Broker legt ihn nicht an.** Sie
funktioniert auf jeder Plattform, braucht kein einziges neues Recht, und ein
falsch konfigurierter Broker scheitert beim ersten Provision mit einer Meldung,
die den fehlenden Namespace beim Namen nennt — statt still etwas anzulegen, das
niemand bestellt hat.

**Das Ergebnis wird validiert, nicht zurechtgebogen.** Ergibt das Template
keinen gültigen RFC-1123-Namen — weil ein Space „Team Grün (Q3)" heißt —, ist
das Provision ein Fehler mit ebendieser Begründung. Ein stillschweigend
verstümmelter Name führt zwei Mandanten in denselben Namespace, und das fällt
niemandem auf.

**`create: true` ist der ausdrückliche Ausstieg**, kein Standard. Wer ihn setzt,
gibt dem Broker `create` auf `namespaces` und nimmt in Kauf, dass **niemand sie
je wieder abräumt**: OSB kennt nur das Löschen einer Instanz, nicht das Ende
eines Mandanten. Der Broker legt in dem Fall an und lässt stehen.

## Konsequenzen

- **Kein neues Recht in der Vorgabe.** Der Broker braucht `create namespaces`
  nur, wenn jemand `create: true` setzt. Bei einem festen Namespace lassen sich
  die heute clusterweiten Rechte auf `rbac.operatorCRDs` sogar auf diesen einen
  Namespace einschränken — der Schadensradius eines übernommenen Broker-Pods
  sinkt.
- **Bestehende Instanzen bleiben unberührt.** Ihr Namespace steht in
  `Instance.Namespace` und wird von dort gelesen. Die Regel gilt nur für neue
  Provisions; eine Migration gibt es nicht und braucht es nicht.
- **Ein fester Namespace trennt Mandanten auf Kubernetes-Ebene nicht.** Das ist
  vertretbar und muss trotzdem gesagt werden: die Konsumenten sind
  CF-Entwickler, die nie `kubectl` sehen — ihre Grenze ist die OSB-API, nicht
  der Namespace. Wer den Cluster mit fremden Arbeitslasten teilt oder
  Kontingente je Mandant braucht, setzt ein Template mit `.orgGUID` und legt die
  Namespaces vorab an.
- **Namenskollisionen gibt es nicht.** Objektnamen leiten sich aus der
  Instanz-ID ab (`osb-<guid>`), die global eindeutig ist; ein gemeinsamer
  Namespace ändert daran nichts.
- **Der Betreiber muss etwas tun.** Ohne Konfiguration und ohne angelegten
  Namespace läuft nichts. Das ist Absicht: eine Plattform, auf der Instanzen
  landen, ohne dass jemand entschieden hat wo, ist schlimmer als eine, die beim
  ersten Versuch stehenbleibt.

## Abgrenzung

**Nicht entschieden ist, ob der Broker je ein Controller wird.** Ein Controller
könnte Namespaces führen und wieder abräumen. Er steht hinter dem
Zielsystem-Durchlauf, und diese Entscheidung wartet nicht auf ihn.

**Ausdrücklich verworfen: die Annotationen als Platzierungsquelle.** Cloud
Foundry reicht `space_annotations` und `organization_annotations` durch, und es
ist verführerisch, den Namespace dort hineinschreiben zu lassen — deklarativ,
ohne Broker-Konfiguration, je Mandant. **Space-Annotationen setzt aber der Space
Manager, also der Mandant selbst.** Er könnte damit den Namespace eines anderen
Mandanten wählen. Eine Platzierungsentscheidung gehört dem Betreiber der
Plattform, nicht ihrem Nutzer.

**Ausdrücklich verworfen: eine gepflegte Zuordnungstabelle je Space.** Sie wäre
präzise und wäre ein Ticket je Space — dieselbe Bauform, an der die
Valkey-Definition gescheitert ist: ein Dienst, dessen Bereitstellung einen
manuellen Schritt braucht, gehört nicht in einen Marktplatz.
