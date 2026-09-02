package server

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/osb-broker/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newReloader(t *testing.T, dir string) *CertReloader {
	t.Helper()
	r, err := NewCertReloader(filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key"), "")
	require.NoError(t, err)
	return r
}

func servedSerial(t *testing.T, r *CertReloader) string {
	t.Helper()
	cert, err := r.GetCertificate(&tls.ClientHelloInfo{ServerName: "localhost"})
	require.NoError(t, err)
	require.NotNil(t, cert.Leaf, "leaf must be parsed so callers can inspect the certificate")
	return cert.Leaf.SerialNumber.String()
}

func TestNewCertReloader_MissingFileFailsFast(t *testing.T) {
	// A broker that cannot read its certificate must not come up serving
	// nothing; it must fail at startup where the operator sees it.
	_, err := NewCertReloader("/nonexistent/tls.crt", "/nonexistent/tls.key", "")
	require.Error(t, err)
}

func TestCertReloader_ServesLoadedCertificate(t *testing.T) {
	dir := t.TempDir()
	serial := writeCert(t, dir)

	r := newReloader(t, dir)
	assert.Equal(t, serial.String(), servedSerial(t, r))
}

func TestCertReloader_PicksUpRotatedCertificate(t *testing.T) {
	dir := t.TempDir()
	writeCert(t, dir)
	r := newReloader(t, dir)
	before := servedSerial(t, r)

	newSerial := writeCert(t, dir)
	changed, err := r.Reload()
	require.NoError(t, err)
	assert.True(t, changed)

	after := servedSerial(t, r)
	assert.NotEqual(t, before, after)
	assert.Equal(t, newSerial.String(), after)
}

func TestCertReloader_UnchangedFilesAreNotReparsed(t *testing.T) {
	dir := t.TempDir()
	writeCert(t, dir)
	r := newReloader(t, dir)

	changed, err := r.Reload()
	require.NoError(t, err)
	assert.False(t, changed, "polling must be cheap when nothing rotated")
}

func TestCertReloader_CorruptFileKeepsPreviousCertificate(t *testing.T) {
	dir := t.TempDir()
	writeCert(t, dir)
	r := newReloader(t, dir)
	before := servedSerial(t, r)

	// A half-written or truncated file during rotation must never take the
	// listener down.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tls.crt"), []byte("not a certificate"), 0o600))

	changed, err := r.Reload()
	require.Error(t, err)
	assert.False(t, changed)
	assert.Equal(t, before, servedSerial(t, r), "the last good certificate must survive a failed reload")
}

func TestCertReloader_DetectsKubernetesSymlinkSwap(t *testing.T) {
	// Kubernetes projects Secret volumes through an atomically swapped
	// ..data symlink, so the leaf paths never change - only their contents.
	dir := t.TempDir()

	writeVersion := func(name string) {
		sub := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(sub, 0o700))
		writeCert(t, sub)
	}
	link := func(target string) {
		tmp := filepath.Join(dir, ".data-tmp")
		require.NoError(t, os.Symlink(target, tmp))
		require.NoError(t, os.Rename(tmp, filepath.Join(dir, "..data")))
	}

	writeVersion("..data-1")
	link("..data-1")
	require.NoError(t, os.Symlink(filepath.Join(dir, "..data", "tls.crt"), filepath.Join(dir, "tls.crt")))
	require.NoError(t, os.Symlink(filepath.Join(dir, "..data", "tls.key"), filepath.Join(dir, "tls.key")))

	r := newReloader(t, dir)
	before := servedSerial(t, r)

	writeVersion("..data-2")
	link("..data-2")

	changed, err := r.Reload()
	require.NoError(t, err)
	assert.True(t, changed, "a ..data symlink swap must be detected")
	assert.NotEqual(t, before, servedSerial(t, r))
}

func TestCertReloader_ReloadsClientCAPool(t *testing.T) {
	dir := t.TempDir()
	writeCert(t, dir)
	caPEM, _, _ := generateCert(t)
	caPath := filepath.Join(dir, "ca.crt")
	require.NoError(t, os.WriteFile(caPath, caPEM, 0o600))

	r, err := NewCertReloader(filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key"), caPath)
	require.NoError(t, err)

	cfg, err := r.GetConfigForClient(&tls.ClientHelloInfo{})
	require.NoError(t, err)
	require.NotNil(t, cfg.ClientCAs, "the client CA pool must ride on the per-handshake config")
	assert.Len(t, cfg.Certificates, 1)

	// Rotating the CA bundle must be picked up too - that is the reason for
	// GetConfigForClient rather than GetCertificate alone.
	newCA, _, _ := generateCert(t)
	require.NoError(t, os.WriteFile(caPath, append(caPEM, newCA...), 0o600))
	changed, err := r.Reload()
	require.NoError(t, err)
	assert.True(t, changed)
}

func TestCertReloader_RejectsUnparsableCABundle(t *testing.T) {
	dir := t.TempDir()
	writeCert(t, dir)
	caPath := filepath.Join(dir, "ca.crt")
	require.NoError(t, os.WriteFile(caPath, []byte("garbage"), 0o600))

	_, err := NewCertReloader(filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key"), caPath)
	require.Error(t, err)
}

func TestBuildTLSConfig(t *testing.T) {
	dir := t.TempDir()
	writeCert(t, dir)
	r := newReloader(t, dir)

	cfg := BuildTLSConfig(config.TLSConfig{
		MinVersion: tls.VersionTLS13,
		ClientAuth: tls.VerifyClientCertIfGiven,
	}, r)

	assert.Equal(t, uint16(tls.VersionTLS13), cfg.MinVersion)
	assert.NotNil(t, cfg.GetCertificate)
	assert.NotNil(t, cfg.GetConfigForClient)
	// Cipher suites are deliberately left to Go's maintained defaults.
	assert.Nil(t, cfg.CipherSuites)

	perHandshake, err := cfg.GetConfigForClient(&tls.ClientHelloInfo{})
	require.NoError(t, err)
	assert.Equal(t, tls.VerifyClientCertIfGiven, perHandshake.ClientAuth)
	assert.Equal(t, uint16(tls.VersionTLS13), perHandshake.MinVersion)
}
