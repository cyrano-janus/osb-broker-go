# Making Korifi trust the broker certificate

> [Deutsch](../../de/how-to/korifi-trust.md) · Leading version: German

The broker terminates TLS itself. For the development platform to register it
over `https://`, the Korifi side has to trust the issuing CA. That step lies
**outside this repository** — it is documented here because TLS operation does
not work without it.

## Why it is necessary

Korifi validates service broker certificates. The only switch for it is:

```yaml
experimental:
  managedServices:
    trustInsecureBrokers: false   # default
```

> *"Disable service broker certificate validation. Not recommended to be set
> to 'true' in production environments."*

Unlike the container registry path, which has `containerRegistryCACertSecret`,
there is **no value for a broker-specific CA**. Trust therefore has to come from
the Korifi pods' own trust store: the broker certificate must chain up to a CA
those pods already trust.

## The regular route: an own CA

1. **Create a CA issuer** (cert-manager assumed):

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

2. **Have the broker issued from it** — the chart does that itself:

   ```bash
   helm upgrade --install osb deploy/helm/osb-broker-go -n osb \
     -f deploy/helm/osb-broker-go/values-kind.yaml \
     --set tls.certManager.issuerRef.name=osb-ca-issuer
   ```

3. **Get the CA into the Korifi pods.** Two variants:

   - **trust-manager**, where available: a `Bundle` that writes `ca.crt`
     together with the public roots into a ConfigMap, mounted into the Korifi
     deployments as `/etc/ssl/certs/ca-certificates.crt`.
   - **A direct patch**, sufficient for kind: create the same ConfigMap by hand
     and mount it into `korifi-controllers-controller-manager` and
     `korifi-api-deployment`.

   The pods that matter are the ones making the OSB calls — controller and API.
   Roll them out again after the patch.

4. **Register:**

   ```bash
   cf create-service-broker go-reference-broker broker-user broker-secret \
     "https://osb-broker-go.osb.svc.cluster.local"
   ```

## The shortcut, for narrowing a fault down only

```yaml
experimental:
  managedServices:
    trustInsecureBrokers: true
```

The connection stays encrypted, but the broker is **not authenticated** —
anyone able to redirect the service name gets the broker credentials and with
them access to production database credentials. Use it only to determine whether
a fault really is about trust.

## Moving an existing registration

A broker URL registered with `http://` stays on HTTP and has to be moved:

```bash
cf update-service-broker go-reference-broker broker-user broker-secret \
  "https://osb-broker-go.osb.svc.cluster.local"
```

The chart's service port with TLS is **443**.

## The app side

Korifi terminates app routes over TLS, by default with a self-signed certificate
from `workloadsTLSSecret`. For an unbroken chain of trust that secret is issued
from the same CA issuer as the broker certificate. That is configuration on the
Korifi chart, not code in this repository.

## What the broker itself does

- Rotation needs **no pod restart**: certificate and client CA are re-read every
  `TLS_RELOAD_INTERVAL` (default 30s). Demonstrated in the kind cluster: the
  serial number changes, `restartCount` stays 0.
- `/healthz` and `/metrics` stay reachable without a client certificate as long
  as `MTLS_REQUIRE` is not set — see
  [reference/configuration.md](../reference/configuration.md).
