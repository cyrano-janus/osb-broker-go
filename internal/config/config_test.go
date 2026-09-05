package config

import (
	"crypto/tls"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// env turns a map into a getenv func so tests never touch the process
// environment (no t.Setenv, no ordering hazards with -race).
func env(kv map[string]string) func(string) string {
	return func(k string) string { return kv[k] }
}

func TestLoad_Defaults(t *testing.T) {
	c, err := LoadFrom(env(nil))
	require.NoError(t, err)

	assert.Equal(t, "8080", c.Port)
	// Leer bedeutet jetzt ausdruecklich "memory" statt eines Leerwerts, der
	// sich erst im Auswahl-switch als In-Memory herausstellte.
	assert.Equal(t, BackendMemory, c.StoreBackend)
	assert.True(t, c.MetricsEnabled, "metrics are on unless explicitly disabled")

	assert.Equal(t, 10*time.Second, c.Server.ReadHeaderTimeout)
	assert.Equal(t, 30*time.Second, c.Server.ReadTimeout)
	assert.Equal(t, 60*time.Second, c.Server.WriteTimeout)
	assert.Equal(t, 120*time.Second, c.Server.IdleTimeout)
	assert.Equal(t, 15*time.Second, c.Server.ShutdownTimeout)

	assert.False(t, c.TLS.Enabled)
	assert.Equal(t, "osb-broker", c.Auth.Realm)
	assert.Empty(t, c.Auth.Methods)
	assert.False(t, c.AuthEnabled())
}

// The legacy contract from main.go: METRICS_ENABLED disables only on "0".
func TestLoad_MetricsDisabledOnlyByZero(t *testing.T) {
	for value, want := range map[string]bool{"0": false, "1": true, "": true} {
		c, err := LoadFrom(env(map[string]string{"METRICS_ENABLED": value}))
		require.NoError(t, err)
		assert.Equal(t, want, c.MetricsEnabled, "METRICS_ENABLED=%q", value)
	}
}

// Backwards compatibility: the Phase 1.2 deployment sets only the two basic
// auth vars and must keep working unchanged.
func TestLoad_BasicAuthOnlyIsUnchanged(t *testing.T) {
	c, err := LoadFrom(env(map[string]string{
		"BROKER_AUTH_USER":     "broker-user",
		"BROKER_AUTH_PASSWORD": "broker-secret",
	}))
	require.NoError(t, err)

	assert.Equal(t, []string{"basic"}, c.Auth.Methods)
	assert.Equal(t, "broker-user", c.Auth.Basic.User)
	assert.False(t, c.TLS.Enabled)
	assert.Equal(t, tls.NoClientCert, c.TLS.ClientAuth)
	assert.True(t, c.AuthEnabled())
}

func TestLoad_StoreBackendCRDRequiresNamespace(t *testing.T) {
	_, err := LoadFrom(env(map[string]string{"STORE_BACKEND": "crd"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "POD_NAMESPACE")

	c, err := LoadFrom(env(map[string]string{"STORE_BACKEND": "crd", "POD_NAMESPACE": "osb"}))
	require.NoError(t, err)
	assert.Equal(t, "osb", c.PodNamespace)
	assert.Equal(t, BackendCRD, c.StoreBackend)
}

func TestLoad_StoreBackendK8sIstAliasFuerCRD(t *testing.T) {
	// Der ConfigMap-Store hiess "k8s". Deployments, die den Wert gesetzt
	// haben, duerfen nicht still auf In-Memory zurueckfallen - das waere
	// stiller Datenverlust beim Upgrade.
	c, err := LoadFrom(env(map[string]string{"STORE_BACKEND": "k8s", "POD_NAMESPACE": "osb"}))
	require.NoError(t, err)
	assert.Equal(t, BackendCRD, c.StoreBackend)
	assert.Contains(t, joined(c.Warnings), "STORE_BACKEND=k8s")
}

func TestLoad_UnbekanntesStoreBackendIstEinFehler(t *testing.T) {
	// Vorher fiel jeder unbekannte Wert - auch ein Tippfehler - still auf
	// In-Memory zurueck. Der Broker lief dann scheinbar normal und verlor
	// beim naechsten Neustart alle Instanzen.
	_, err := LoadFrom(env(map[string]string{"STORE_BACKEND": "configmap"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "STORE_BACKEND")
}

func TestLoad_StoreBackendMemoryBleibtErlaubt(t *testing.T) {
	c, err := LoadFrom(env(map[string]string{"STORE_BACKEND": "memory"}))
	require.NoError(t, err)
	assert.Equal(t, BackendMemory, c.StoreBackend)

	// Leer bleibt der Default fuer lokale Laeufe und Tests.
	c, err = LoadFrom(env(nil))
	require.NoError(t, err)
	assert.Equal(t, BackendMemory, c.StoreBackend)
	assert.Contains(t, joined(c.Warnings), "in-memory")
}

func TestLoad_TLSRequiresCertAndKey(t *testing.T) {
	_, err := LoadFrom(env(map[string]string{"TLS_ENABLED": "true"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TLS_CERT_FILE")

	_, err = LoadFrom(env(map[string]string{"TLS_ENABLED": "true", "TLS_CERT_FILE": "/c"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TLS_KEY_FILE")
}

func TestLoad_MTLSRequiresCAFileAndTLS(t *testing.T) {
	base := map[string]string{"TLS_ENABLED": "true", "TLS_CERT_FILE": "/c", "TLS_KEY_FILE": "/k"}

	kv := map[string]string{"MTLS_ENABLED": "true"}
	for k, v := range base {
		kv[k] = v
	}
	_, err := LoadFrom(env(kv))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MTLS_CLIENT_CA_FILE")

	// mTLS without TLS is meaningless - there is no handshake to carry it.
	_, err = LoadFrom(env(map[string]string{"MTLS_ENABLED": "true", "MTLS_CLIENT_CA_FILE": "/ca"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TLS_ENABLED")
}

func TestLoad_MinVersion(t *testing.T) {
	tlsOn := func(extra map[string]string) map[string]string {
		kv := map[string]string{"TLS_ENABLED": "true", "TLS_CERT_FILE": "/c", "TLS_KEY_FILE": "/k"}
		for k, v := range extra {
			kv[k] = v
		}
		return kv
	}

	c, err := LoadFrom(env(tlsOn(nil)))
	require.NoError(t, err)
	assert.Equal(t, uint16(tls.VersionTLS12), c.TLS.MinVersion, "default is TLS 1.2")

	c, err = LoadFrom(env(tlsOn(map[string]string{"TLS_MIN_VERSION": "1.3"})))
	require.NoError(t, err)
	assert.Equal(t, uint16(tls.VersionTLS13), c.TLS.MinVersion)

	_, err = LoadFrom(env(tlsOn(map[string]string{"TLS_MIN_VERSION": "1.1"})))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TLS_MIN_VERSION")
}

func TestLoad_AuthMethodsExplicit(t *testing.T) {
	kv := map[string]string{
		"TLS_ENABLED": "true", "TLS_CERT_FILE": "/c", "TLS_KEY_FILE": "/k",
		"MTLS_ENABLED": "true", "MTLS_CLIENT_CA_FILE": "/ca", "MTLS_ALLOWED_CNS": "osb-gate",
		"BROKER_AUTH_USER": "u", "BROKER_AUTH_PASSWORD": "p",
		"AUTH_METHODS": "mtls,basic",
	}
	c, err := LoadFrom(env(kv))
	require.NoError(t, err)
	// Order is normalised so the WWW-Authenticate challenge order is stable.
	assert.Equal(t, []string{"basic", "mtls"}, c.Auth.Methods)
}

func TestLoad_AuthMethodsRejectsUnknownAndUnconfigured(t *testing.T) {
	_, err := LoadFrom(env(map[string]string{
		"BROKER_AUTH_USER": "u", "AUTH_METHODS": "basic,oauth2",
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "oauth2")

	_, err = LoadFrom(env(map[string]string{"AUTH_METHODS": "mtls"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestLoad_CSVParsing(t *testing.T) {
	c, err := LoadFrom(env(map[string]string{
		"TLS_ENABLED": "true", "TLS_CERT_FILE": "/c", "TLS_KEY_FILE": "/k",
		"MTLS_ENABLED": "true", "MTLS_CLIENT_CA_FILE": "/ca",
		"MTLS_ALLOWED_CNS":       " osb-gate , korifi ,, ",
		"MTLS_ALLOWED_DNS_NAMES": "broker.example.com",
		"MTLS_ALLOWED_URIS":      "spiffe://osb/checker",
	}))
	require.NoError(t, err)
	assert.Equal(t, []string{"osb-gate", "korifi"}, c.Auth.MTLS.AllowedCNs)
	assert.Equal(t, []string{"broker.example.com"}, c.Auth.MTLS.AllowedDNSNames)
	assert.Equal(t, []string{"spiffe://osb/checker"}, c.Auth.MTLS.AllowedURIs)
}

func TestLoad_InvalidDurationIsAnError(t *testing.T) {
	_, err := LoadFrom(env(map[string]string{"SERVER_READ_TIMEOUT": "soon"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SERVER_READ_TIMEOUT")
}

func TestLoad_InvalidBoolIsAnError(t *testing.T) {
	_, err := LoadFrom(env(map[string]string{"TLS_ENABLED": "maybe"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TLS_ENABLED")
}

func TestLoad_ClientAuthDerivation(t *testing.T) {
	tlsBase := map[string]string{"TLS_ENABLED": "true", "TLS_CERT_FILE": "/c", "TLS_KEY_FILE": "/k"}
	mtlsBase := map[string]string{"MTLS_ENABLED": "true", "MTLS_CLIENT_CA_FILE": "/ca", "MTLS_ALLOWED_CNS": "x"}

	merge := func(ms ...map[string]string) map[string]string {
		out := map[string]string{}
		for _, m := range ms {
			for k, v := range m {
				out[k] = v
			}
		}
		return out
	}

	cases := []struct {
		name string
		kv   map[string]string
		want tls.ClientAuthType
	}{
		{"tls without mtls", tlsBase, tls.NoClientCert},
		{"mtls optional", merge(tlsBase, mtlsBase), tls.VerifyClientCertIfGiven},
		{"mtls required and sole method", merge(tlsBase, mtlsBase, map[string]string{"MTLS_REQUIRE": "true"}), tls.RequireAndVerifyClientCert},
		{"mtls required but basic also active", merge(tlsBase, mtlsBase, map[string]string{
			"MTLS_REQUIRE": "true", "BROKER_AUTH_USER": "u", "BROKER_AUTH_PASSWORD": "p",
		}), tls.VerifyClientCertIfGiven},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := LoadFrom(env(tc.kv))
			require.NoError(t, err)
			assert.Equal(t, tc.want, c.TLS.ClientAuth)
		})
	}
}

func TestLoad_Warnings(t *testing.T) {
	// No auth configured at all: the broker is open, and must say so.
	c, err := LoadFrom(env(nil))
	require.NoError(t, err)
	assert.Contains(t, joined(c.Warnings), "authentication is disabled")

	// mTLS without an allowlist accepts every cert the CA ever signed.
	c, err = LoadFrom(env(map[string]string{
		"TLS_ENABLED": "true", "TLS_CERT_FILE": "/c", "TLS_KEY_FILE": "/k",
		"MTLS_ENABLED": "true", "MTLS_CLIENT_CA_FILE": "/ca",
	}))
	require.NoError(t, err)
	assert.Contains(t, joined(c.Warnings), "allowlist")

	// MTLS_REQUIRE silently downgraded because another method is active.
	c, err = LoadFrom(env(map[string]string{
		"TLS_ENABLED": "true", "TLS_CERT_FILE": "/c", "TLS_KEY_FILE": "/k",
		"MTLS_ENABLED": "true", "MTLS_CLIENT_CA_FILE": "/ca", "MTLS_ALLOWED_CNS": "x",
		"MTLS_REQUIRE": "true", "BROKER_AUTH_USER": "u", "BROKER_AUTH_PASSWORD": "p",
	}))
	require.NoError(t, err)
	assert.Contains(t, joined(c.Warnings), "MTLS_REQUIRE")
}

func joined(ws []string) string {
	out := ""
	for _, w := range ws {
		out += w + "\n"
	}
	return out
}

// LOG_REQUESTS war im Helm-Chart als `config.logRequests` dokumentiert, von
// keinem Template gerendert und vom Broker nie gelesen: ein Schalter, den
// jemand umlegt und der nichts tut.
func TestConfig_LogRequestsIstStandardmaessigAn(t *testing.T) {
	c, err := LoadFrom(env(nil))
	require.NoError(t, err)
	assert.True(t, c.LogRequests,
		"ohne Angabe wird protokolliert - ein Broker, der stumm laeuft, ist im Fehlerfall nicht nachvollziehbar")
}

func TestConfig_LogRequestsLaesstSichAbschalten(t *testing.T) {
	c, err := LoadFrom(env(map[string]string{"LOG_REQUESTS": "false"}))
	require.NoError(t, err)
	assert.False(t, c.LogRequests)
}
