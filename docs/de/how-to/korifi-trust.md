# Korifi dem Broker-Zertifikat vertrauen lassen

> [English](../../en/how-to/korifi-trust.md) · Führende Fassung: deutsch

Der Broker terminiert TLS selbst. Damit die Entwicklungsplattform ihn über
`https://` registrieren kann, muss die Korifi-Seite dem ausstellenden CA
vertrauen. Dieser Schritt liegt **außerhalb dieses Repos** — er steht hier, weil
der TLS-Betrieb ohne ihn nicht funktioniert.

## Warum das nötig ist

Korifi validiert Service-Broker-Zertifikate. Der einzige Schalter dafür ist:

```yaml
experimental:
  managedServices:
    trustInsecureBrokers: false   # Default
```

> *„Disable service broker certificate validation. Not recommended to be set
> to 'true' in production environments."*

Anders als beim Container-Registry-Pfad, der `containerRegistryCACertSecret`
kennt, gibt es **keinen Wert für eine broker-spezifische CA**. Das Vertrauen muss
also aus dem Trust-Store der Korifi-Pods selbst kommen: das Broker-Zertifikat
muss auf eine CA zurückführen, der diese Pods ohnehin vertrauen.

## Der reguläre Weg: eine eigene CA

1. **CA-Issuer anlegen** (cert-manager vorausgesetzt):

   ```bash
   openssl req -x509 -newkey rsa:4096 -nodes -days 3650 -sha256 \
     -keyout ca.key -out ca.crt -subj "/CN=osb-platform-ca"
   kubectl create secret tls osb-ca -n cert-manager --cert=ca.crt --key=ca.key
   ```

   ```yaml
   apiVersion: cert-manager.io/v1
   kind: ClusterIssuer
   metadata:
     name: osb-ca-issuer
   spec:
     ca:
       secretName: osb-ca
   ```

2. **Broker daraus ausstellen lassen** — das Chart macht das selbst:

   ```bash
   helm upgrade --install osb deploy/helm/osb-broker-go -n osb \
     -f deploy/helm/osb-broker-go/values-kind.yaml \
     --set tls.certManager.issuerRef.name=osb-ca-issuer
   ```

3. **CA in die Korifi-Pods bringen.** Zwei Varianten:

   - **trust-manager**, wo vorhanden: ein `Bundle`, das `ca.crt` zusammen mit
     den öffentlichen Roots in ein ConfigMap schreibt, das in den
     Korifi-Deployments als `/etc/ssl/certs/ca-certificates.crt` gemountet wird.
   - **Direkter Patch**, ausreichend für kind: dasselbe ConfigMap von Hand
     anlegen und in `korifi-controllers-controller-manager` sowie
     `korifi-api-deployment` mounten.

   Betroffen sind die Pods, die die OSB-Calls ausführen — Controller und API.
   Nach dem Patch neu ausrollen.

4. **Registrieren:**

   ```bash
   cf create-service-broker go-reference-broker broker-user broker-secret \
     "https://osb-broker-go.osb.svc.cluster.local"
   ```

## Die Abkürzung, nur zum Eingrenzen

```yaml
experimental:
  managedServices:
    trustInsecureBrokers: true
```

Die Verbindung bleibt verschlüsselt, aber der Broker wird **nicht
authentifiziert** — wer in der Lage ist, den Service-Namen umzubiegen, bekommt
die Broker-Credentials und damit Zugriff auf produktive Datenbankzugangsdaten.
Nur benutzen, um einzugrenzen, ob ein Fehler wirklich am Trust liegt.

## Eine bestehende Registrierung umziehen

Eine mit `http://` registrierte Broker-URL bleibt auf HTTP stehen und muss
umgezogen werden:

```bash
cf update-service-broker go-reference-broker broker-user broker-secret \
  "https://osb-broker-go.osb.svc.cluster.local"
```

Der Service-Port des Charts ist mit TLS **443**.

## App-Seite

Korifi terminiert App-Routen über TLS, per Default mit einem selbstsignierten
Zertifikat aus `workloadsTLSSecret`. Für eine durchgängige Vertrauenskette wird
dieses Secret aus demselben CA-Issuer ausgestellt wie das Broker-Zertifikat. Das
ist Konfiguration am Korifi-Chart, kein Code in diesem Repo.

## Was der Broker selbst tut

- Rotation braucht **keinen Pod-Neustart**: Zertifikat und Client-CA werden alle
  `TLS_RELOAD_INTERVAL` (Default 30s) neu eingelesen. Im kind-Cluster
  nachgewiesen: die Seriennummer wechselt, `restartCount` bleibt 0.
- `/healthz` und `/metrics` bleiben ohne Client-Zertifikat erreichbar, solange
  `MTLS_REQUIRE` nicht gesetzt ist — siehe
  [reference/configuration.md](../reference/configuration.md).
