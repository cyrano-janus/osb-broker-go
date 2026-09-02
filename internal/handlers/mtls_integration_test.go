package handlers

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/osb-broker/internal/auth"
	"github.com/example/osb-broker/internal/broker"
	"github.com/example/osb-broker/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mtlsCA is a throwaway CA that signs the client certificates below.
type mtlsCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pool *x509.CertPool
}

func newMTLSCA(t *testing.T) *mtlsCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "osb-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &mtlsCA{cert: cert, key: key, pool: pool}
}

func (ca *mtlsCA) clientCert(t *testing.T, cn string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)

	pair, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	)
	require.NoError(t, err)
	return pair
}

// newMTLSServer starts the real router behind TLS with mTLS optional, the
// mode the broker ships with: a presented certificate is fully verified,
// its absence leaves Basic Auth as the way in.
func newMTLSServer(t *testing.T, ca *mtlsCA) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)

	h := New(broker.New(store.NewInMemoryStore(), nil))
	h.SetAuthenticator(auth.NewChain(
		auth.NewBasic("broker-user", "broker-secret", "osb-broker"),
		auth.NewMTLS([]string{"osb-checker"}, nil, nil),
	))

	srv := httptest.NewUnstartedServer(h.SetupRouter())
	srv.TLS = &tls.Config{
		MinVersion: tls.VersionTLS12,
		ClientAuth: tls.VerifyClientCertIfGiven,
		ClientCAs:  ca.pool,
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

// tlsClient trusts the test server and optionally presents a client cert.
func tlsClient(srv *httptest.Server, clientCert *tls.Certificate) *http.Client {
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	cfg := &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	if clientCert != nil {
		cfg.Certificates = []tls.Certificate{*clientCert}
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}}
}

func get(t *testing.T, c *http.Client, url string, basicUser, basicPass string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("GET", url, nil)
	require.NoError(t, err)
	req.Header.Set("X-Broker-API-Version", "2.17")
	if basicUser != "" {
		req.SetBasicAuth(basicUser, basicPass)
	}
	resp, err := c.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestMTLS_AllowlistedClientCertificateIsAuthenticated(t *testing.T) {
	ca := newMTLSCA(t)
	srv := newMTLSServer(t, ca)
	cert := ca.clientCert(t, "osb-checker")

	resp := get(t, tlsClient(srv, &cert), srv.URL+"/v2/catalog", "", "")
	assert.Equal(t, http.StatusOK, resp.StatusCode, "a listed client certificate authenticates without any credentials")
}

func TestMTLS_CASignedButUnlistedCertificateIsRejected(t *testing.T) {
	ca := newMTLSCA(t)
	srv := newMTLSServer(t, ca)
	cert := ca.clientCert(t, "some-other-client")

	resp := get(t, tlsClient(srv, &cert), srv.URL+"/v2/catalog", "", "")
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode,
		"being signed by the broker's CA must not be enough on its own")
}

func TestMTLS_NoCertificateAndNoCredentialsIsUnauthorized(t *testing.T) {
	ca := newMTLSCA(t)
	srv := newMTLSServer(t, ca)

	resp := get(t, tlsClient(srv, nil), srv.URL+"/v2/catalog", "", "")
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	// mTLS contributes no challenge, so the header carries only Basic - the
	// exact value the conformance suite and Cloud Foundry expect.
	assert.Equal(t, []string{`Basic realm="osb-broker"`}, resp.Header.Values("WWW-Authenticate"))
}

func TestMTLS_BasicAuthStillWorksWithoutCertificate(t *testing.T) {
	ca := newMTLSCA(t)
	srv := newMTLSServer(t, ca)

	resp := get(t, tlsClient(srv, nil), srv.URL+"/v2/catalog", "broker-user", "broker-secret")
	assert.Equal(t, http.StatusOK, resp.StatusCode,
		"Cloud Foundry sends Basic Auth and presents no client certificate")
}

func TestMTLS_HealthzStaysOpenWithoutCertificate(t *testing.T) {
	// The kubelet presents neither a client certificate nor credentials.
	ca := newMTLSCA(t)
	srv := newMTLSServer(t, ca)

	resp := get(t, tlsClient(srv, nil), srv.URL+"/healthz", "", "")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
