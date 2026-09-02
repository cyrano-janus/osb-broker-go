package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/example/osb-broker/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serveTLS starts the broker's own TLS listener on a random port and returns
// its address plus the pool that trusts it.
func serveTLS(t *testing.T, clientAuth tls.ClientAuthType) (addr string, pool *x509.CertPool) {
	t.Helper()

	dir := t.TempDir()
	writeCert(t, dir)
	r := newReloader(t, dir)

	tlsCfg := BuildTLSConfig(config.TLSConfig{
		MinVersion: tls.VersionTLS12,
		ClientAuth: clientAuth,
	}, r)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv := New(Options{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, "ok")
		}),
		TLS:               tlsCfg,
		ReadHeaderTimeout: time.Second,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, srv, ln, 2*time.Second) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	certPEM, err := os.ReadFile(filepath.Join(dir, "tls.crt"))
	require.NoError(t, err)
	pool = x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(certPEM))

	return ln.Addr().String(), pool
}

func TestServe_TLSListenerServesRequests(t *testing.T) {
	addr, pool := serveTLS(t, tls.NoClientCert)

	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
	}}
	resp, err := client.Get("https://" + addr + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.True(t, resp.TLS.Version >= tls.VersionTLS12)
}

func TestServe_TLSRejectsUntrustedClientView(t *testing.T) {
	addr, _ := serveTLS(t, tls.NoClientCert)

	// A client that does not trust the broker's CA must fail the handshake -
	// proof that the listener presents a real certificate rather than
	// anything a client would accept.
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}}
	_, err := client.Get("https://" + addr + "/healthz")
	require.Error(t, err)
}

func TestServe_TLSAllowsCertlessClientWhenOptional(t *testing.T) {
	// VerifyClientCertIfGiven is what keeps kubelet probes and Prometheus
	// scrapes working while mTLS is enabled for real clients.
	addr, pool := serveTLS(t, tls.VerifyClientCertIfGiven)

	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
	}}
	resp, err := client.Get("https://" + addr + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
