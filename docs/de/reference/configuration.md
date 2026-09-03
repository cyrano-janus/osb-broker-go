# Konfiguration

> [English](../../en/reference/configuration.md) · Führende Fassung: deutsch

Der Broker liest seine Konfiguration ausschließlich aus Umgebungsvariablen.
`internal/config` wandelt sie in **eine** validierte Struktur um und bricht beim
Start ab, wenn etwas nicht stimmt. Das ist der Ersatz für das frühere, über den
Code verstreute `os.Getenv`.

**Fail-Fast statt stiller Rückfall.** Ein unbekannter Wert in `STORE_BACKEND` war
früher ein Tippfehler, der still auf den In-Memory-Speicher zurückfiel — der
Broker lief, verlor aber jeden Zustand beim Neustart. Heute startet er nicht.

## Umgebungsvariablen

### Grundlagen

| Variable | Vorgabe | Wirkung |
|---|---|---|
| `PORT` | `8080` | Lauschport. Mit TLS setzt das Chart `8443`. |
| `STORE_BACKEND` | `memory` | `crd` oder `memory`. `k8s` ist ein Alias für `crd` und erzeugt eine Warnung. Alles andere ist ein **Startfehler**. |
| `POD_NAMESPACE` | leer | Namespace, in dem die State-CRs liegen. Bei `STORE_BACKEND=crd` Pflicht; das Chart füllt sie per `fieldRef`. |
| `DEFINITIONS_DIR` | leer | Verzeichnis mit den ServiceDefinitions. Leer heißt: keine Definitionen, nur der Legacy-Katalog. |
| `METRICS_ENABLED` | an | **Nur der exakte Wert `0` schaltet ab.** Kein `ParseBool` — `false` ließ die Metriken vor Einführung dieses Pakets an und muss das weiter tun. |

### Authentifizierung

| Variable | Vorgabe | Wirkung |
|---|---|---|
| `AUTH_METHODS` | abgeleitet | Kommaliste aus `basic`, `mtls`. Leer: wird aus dem ermittelt, was konfiguriert ist. |
| `AUTH_REALM` | `osb-broker` | Realm in der `WWW-Authenticate`-Antwort. |
| `BROKER_AUTH_USER` | leer | Basic-Auth-Benutzer. Leer heißt: keine Basic-Auth. |
| `BROKER_AUTH_PASSWORD` | leer | dazugehöriges Passwort. |

### mTLS

| Variable | Vorgabe | Wirkung |
|---|---|---|
| `MTLS_ENABLED` | `false` | Client-Zertifikate als Authentifizierungsmethode. |
| `MTLS_REQUIRE` | `false` | Erzwingt ein gültiges Client-Zertifikat auf TLS-Ebene. |
| `MTLS_CLIENT_CA_FILE` | leer | CA, gegen die Client-Zertifikate geprüft werden. |
| `MTLS_ALLOWED_CNS` | leer | Allowlist über den Common Name. |
| `MTLS_ALLOWED_DNS_NAMES` | leer | Allowlist über SAN-DNS-Namen. |
| `MTLS_ALLOWED_URIS` | leer | Allowlist über SAN-URIs. |

**Die Allowlists sind Autorisierung, nicht Authentifizierung.** Sind sie leer,
wird jedes Zertifikat akzeptiert, das die CA signiert hat. Jede Liste prüft nur
ihr eigenes Feld.

**`MTLS_REQUIRE=true` wirkt nur, wenn mTLS die einzige Methode ist.** Sonst wird
auf `VerifyClientCertIfGiven` heruntergestuft und eine Warnung ausgegeben —
andernfalls könnte sich niemand mehr per Basic Auth anmelden. `RequireAnyClientCert`
wird nie verwendet, weil es ein Zertifikat verlangt, ohne es zu prüfen.

Mit `MTLS_REQUIRE=true` müssen auch die Kubernetes-Probes umgestellt werden; das
Chart schaltet sie dann auf `tcpSocket`.

### TLS

| Variable | Vorgabe | Wirkung |
|---|---|---|
| `TLS_ENABLED` | `false` | TLS-Terminierung im Broker selbst. |
| `TLS_CERT_FILE` | leer | Serverzertifikat. |
| `TLS_KEY_FILE` | leer | zugehöriger Schlüssel. |
| `TLS_MIN_VERSION` | `1.2` | `1.2` oder `1.3`. |
| `TLS_RELOAD_INTERVAL` | `30s` | Prüfintervall des Zertifikats-Reloaders. |

Cipher Suites sind bewusst nicht festgelegt — die Go-Vorgaben sind aktueller als
jede eingefrorene Liste. Begründung in
[ADR 0004](../adr/0004-tls-and-mtls-no-oauth2.md).

### Server-Zeitschranken

| Variable | Vorgabe |
|---|---|
| `SERVER_READ_HEADER_TIMEOUT` | `10s` |
| `SERVER_READ_TIMEOUT` | `30s` |
| `SERVER_WRITE_TIMEOUT` | `60s` |
| `SERVER_IDLE_TIMEOUT` | `120s` |
| `SERVER_SHUTDOWN_TIMEOUT` | `15s` |

### Warnungen beim Start

Fünf Zustände sind nicht fatal, aber gemeldet: In-Memory-Speicher aktiv, keine
Authentifizierung konfiguriert, kein TLS, leere mTLS-Allowlist, und ein
heruntergestuftes `MTLS_REQUIRE`. Wer den Broker betreibt, sollte die erste
Logausgabe lesen.

## Helm-Chart

Das Chart liegt unter `deploy/helm/osb-broker-go`. Die wichtigsten Werte:

| Wert | Vorgabe | Wirkung |
|---|---|---|
| `image.repository` | `ghcr.io/cyrano-janus/osb-broker-go` | |
| `image.tag` | leer | leer bedeutet `.Chart.AppVersion` |
| `service.port` | `443` | Port des ClusterIP-Service |
| `tls.enabled` | `true` | |
| `tls.certManager.enabled` | `true` | Zertifikat über cert-manager |
| `tls.certManager.issuerRef.name` | leer | **Pflichtangabe** |
| `tls.minVersion` | `1.2` | |
| `tls.reloadInterval` | `30s` | |
| `auth.create` | `true` | erzeugt das Secret `<fullname>-auth` |
| `auth.username` / `auth.password` | `broker-user` / `change-me` | im Betrieb zu überschreiben |
| `auth.mtls.enabled` | `false` | |
| `config.storeBackend` | `crd` | |
| `definitions.create` | `true` | erzeugt `<fullname>-definitions` aus `definitions.files` |
| `rbac.operatorCRDs` | sechs Gruppen | siehe Warnung unten |
| `rbac.secretsAcrossNamespaces` | `true` | Secret-Lesen über Namespaces hinweg, für Bind |
| `rbac.projectedBindingSecrets` | `false` | nötig für `spec.bind.projectSecret` |
| `metrics.enabled` | `true` | |
| `podSecurityContext` | nonroot 65532 mit `fsGroup` | die `fsGroup` ist nötig, damit der Prozess die mit Modus `0440` eingehängten Zertifikatsdateien lesen kann |

### Drei Fallstricke im Chart

**Mit den Vorgaben rendert das Chart nicht.** `tls.certManager.enabled: true`
trifft auf einen leeren `issuerRef.name` und damit auf ein `{{ required }}`. Das
ist Absicht — ein Zertifikat ohne Aussteller wäre sinnlos —, heißt aber, dass
`helm install` ohne eigene Werte fehlschlägt.

**`rbac.operatorCRDs` deckt nicht alle mitgelieferten Definitionen ab.** Es
fehlen `minio.min.io/tenants` und `redis.redis.opstreelabs.in/redis`. Wer diese
Definitionen ausrollt, bekommt beim Provision einen 403. Die Liste ist bewusst
handgepflegt: Rechte auf CRDs, die gar nicht installiert sind, verschleiern, was
der Broker wirklich anfassen darf.

**`config.logRequests` wird von keinem Template gelesen** und hat keine
entsprechende Umgebungsvariable. Der Wert wirkt nicht.

### Die State-CRDs liegen nicht im Chart

```bash
kubectl apply -f deploy/crds/
kubectl wait --for condition=established --timeout=60s \
  crd/osbserviceinstances.broker.osb.io crd/osbservicebindings.broker.osb.io
```

Das ist Absicht: clusterweite Objekte in einem namespace-gebundenen Release
kollidieren zwischen Releases, und Helm aktualisiert das Verzeichnis `crds/`
beim `helm upgrade` nie. Begründung in
[ADR 0001](../adr/0001-kubernetes-as-state-store.md).

### Mitgelieferte Wertedateien

| Datei | Zweck |
|---|---|
| `values.yaml` | Vorgaben, siehe Fallstrick oben |
| `values-kind.yaml` | lokale Entwicklung; enthält die Definitionen eingebettet |
| `values-ci.yaml` | das TLS/mTLS-Gate der CI |

**`values-kind.yaml` ist von `definitions/` abgedriftet.** Die dort eingebettete
RabbitMQ-Definition hat keines der Merkmale aus Phase 6, und drei Schlüssel
kommen doppelt vor. Nichts prüft diese Datei. Siehe
[known-issues.md](../known-issues.md).

## Nicht konfigurierbar

Fest verdrahtet, damit man nicht danach sucht:

| Wert | Ort |
|---|---|
| Einhängepfad `/definitions` | `deployment.yaml` |
| Secret-Schlüssel `username` / `password` | Auth-Secret |
| Secret-Schlüssel `credentials.json` | `crdstate.go` |
| Name des Credential-Secrets `<objektname>-credentials` | `crdstate.go` |
| Suffix des projizierten Secrets `-binding` | `servicebinding.go` |
| Statuspfad `.status.binding.name` | `servicebinding.go`, bewusst |
| `https://dashboard.example.com/instances/<id>` | beide Broker-Pfade |
| Rückfall-Namespace `default` | `definition_instances.go` |
