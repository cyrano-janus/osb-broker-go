package server

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/cyrano-janus/osb-broker-go/internal/config"
)

// CertReloader serves the broker's TLS material and picks up rotations
// without a restart.
//
// cert-manager renews certificates in place, and Kubernetes projects the
// Secret through an atomically swapped "..data" symlink. An inotify watch on
// the leaf path stops firing after the first such swap, so this polls and
// compares content digests instead. Restarting the pod on every renewal
// would be the alternative, and it would drop in-flight OSB calls.
type CertReloader struct {
	certFile string
	keyFile  string
	caFile   string

	mu      sync.RWMutex
	cert    *tls.Certificate
	pool    *x509.CertPool
	base    *tls.Config
	certSum [sha256.Size]byte
	keySum  [sha256.Size]byte
	caSum   [sha256.Size]byte
}

// NewCertReloader loads the certificate, key and optional client CA bundle.
// It fails when the material cannot be read or parsed: a broker that cannot
// serve TLS must not start and pretend otherwise.
func NewCertReloader(certFile, keyFile, clientCAFile string) (*CertReloader, error) {
	r := &CertReloader{
		certFile: certFile,
		keyFile:  keyFile,
		caFile:   clientCAFile,
		base:     &tls.Config{MinVersion: tls.VersionTLS12},
	}
	if _, err := r.Reload(); err != nil {
		return nil, err
	}
	return r, nil
}

// Reload re-reads the files and swaps the material in when it changed.
//
// On any error the previously loaded material is kept: a truncated or
// briefly missing file mid-rotation must never break the listener.
func (r *CertReloader) Reload() (bool, error) {
	certPEM, err := os.ReadFile(r.certFile)
	if err != nil {
		return false, fmt.Errorf("read certificate: %w", err)
	}
	keyPEM, err := os.ReadFile(r.keyFile)
	if err != nil {
		return false, fmt.Errorf("read key: %w", err)
	}
	var caPEM []byte
	if r.caFile != "" {
		if caPEM, err = os.ReadFile(r.caFile); err != nil {
			return false, fmt.Errorf("read client CA: %w", err)
		}
	}

	certSum := sha256.Sum256(certPEM)
	keySum := sha256.Sum256(keyPEM)
	caSum := sha256.Sum256(caPEM)

	r.mu.RLock()
	unchanged := r.cert != nil && certSum == r.certSum && keySum == r.keySum && caSum == r.caSum
	r.mu.RUnlock()
	if unchanged {
		return false, nil
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return false, fmt.Errorf("parse keypair: %w", err)
	}
	// Parse the leaf eagerly: it makes a corrupt certificate fail here
	// rather than at handshake time, and lets callers inspect expiry.
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return false, fmt.Errorf("parse leaf certificate: %w", err)
	}
	cert.Leaf = leaf

	var pool *x509.CertPool
	if len(caPEM) > 0 {
		pool = x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return false, errors.New("client CA bundle contains no usable certificate")
		}
	}

	r.mu.Lock()
	r.cert, r.pool = &cert, pool
	r.certSum, r.keySum, r.caSum = certSum, keySum, caSum
	r.mu.Unlock()
	return true, nil
}

// Start polls for rotated material until ctx is cancelled. An interval of 0
// disables polling.
func (r *CertReloader) Start(ctx context.Context, every time.Duration) {
	if every <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				changed, err := r.Reload()
				if err != nil {
					// Keep serving the previous material; the operator needs
					// to see this, but it is not fatal.
					log.Printf("WARNING: TLS reload failed, keeping previous certificate: %v", err)
					continue
				}
				if changed {
					log.Printf("TLS certificate reloaded")
				}
			}
		}
	}()
}

// GetCertificate serves the current certificate to a handshake.
func (r *CertReloader) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.cert == nil {
		return nil, errors.New("no TLS certificate loaded")
	}
	return r.cert, nil
}

// GetConfigForClient returns a fresh config per handshake carrying the
// current certificate and client CA pool.
//
// ClientCAs is read from the base config when the handshake starts, so
// mutating it in place would be both racy and invisible. Cloning per
// handshake is what makes the client CA bundle reloadable at all.
func (r *CertReloader) GetConfigForClient(*tls.ClientHelloInfo) (*tls.Config, error) {
	r.mu.RLock()
	cert, pool, base := r.cert, r.pool, r.base
	r.mu.RUnlock()

	if cert == nil {
		return nil, errors.New("no TLS certificate loaded")
	}
	c := base.Clone()
	c.Certificates = []tls.Certificate{*cert}
	c.ClientCAs = pool
	return c, nil
}

// BuildTLSConfig assembles the listener's TLS configuration.
//
// Cipher suites are deliberately not pinned: Go's TLS 1.2 defaults are
// already AEAD-only and are maintained upstream, whereas a hand-written list
// goes stale. MinVersion is the only knob exposed.
func BuildTLSConfig(cfg config.TLSConfig, r *CertReloader) *tls.Config {
	base := &tls.Config{
		MinVersion: cfg.MinVersion,
		ClientAuth: cfg.ClientAuth,
		NextProtos: []string{"h2", "http/1.1"},
	}

	r.mu.Lock()
	r.base = base
	r.mu.Unlock()

	return &tls.Config{
		MinVersion:         cfg.MinVersion,
		ClientAuth:         cfg.ClientAuth,
		NextProtos:         base.NextProtos,
		GetCertificate:     r.GetCertificate,
		GetConfigForClient: r.GetConfigForClient,
	}
}
