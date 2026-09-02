package auth

import (
	"crypto/x509"
	"fmt"
	"net/http"
)

// mtls authenticates a client by its TLS certificate.
//
// Verification of the chain is done by crypto/tls during the handshake; this
// authenticator only decides whether the verified identity is one the broker
// accepts. That split matters: a Common Name is attacker-chosen text that
// any CA in the pool can sign, so the CA bundle must be narrow and the
// allowlist is what turns authentication into authorisation.
type mtls struct {
	cns      map[string]struct{}
	dnsNames map[string]struct{}
	uris     map[string]struct{}
}

// NewMTLS returns a client-certificate authenticator. All three allowlists
// empty accepts any certificate the configured CA signed - legal, and
// warned about at configuration load.
func NewMTLS(cns, dnsNames, uris []string) Authenticator {
	return &mtls{
		cns:      set(cns),
		dnsNames: set(dnsNames),
		uris:     set(uris),
	}
}

func set(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	m := make(map[string]struct{}, len(values))
	for _, v := range values {
		m[v] = struct{}{}
	}
	return m
}

func (m *mtls) Name() string { return "mtls" }

// Challenge is empty: mTLS happens below HTTP, so there is nothing to
// advertise in WWW-Authenticate.
func (m *mtls) Challenge() string { return "" }

func (m *mtls) Authenticate(r *http.Request) (*Identity, error) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return nil, ErrNoCredentials
	}
	// VerifiedChains is empty when crypto/tls did not verify the chain -
	// which is the case under RequireAnyClientCert. Reading a Subject from
	// an unverified certificate would accept anything self-signed.
	if len(r.TLS.VerifiedChains) == 0 || len(r.TLS.VerifiedChains[0]) == 0 {
		return nil, fmt.Errorf("%w: client certificate chain not verified", ErrInvalidCredentials)
	}
	leaf := r.TLS.VerifiedChains[0][0]

	subject, ok := m.match(leaf)
	if !ok {
		return nil, fmt.Errorf("%w: client certificate not on the allowlist", ErrInvalidCredentials)
	}
	return &Identity{Method: m.Name(), Subject: subject}, nil
}

// match reports whether the certificate carries an allowed identity and
// returns the value that matched.
//
// Each allowlist is checked against its own field only: a Common Name equal
// to an allowed DNS name must not satisfy a DNS-SAN allowlist, or the
// distinction between the two would be decorative.
func (m *mtls) match(leaf *x509.Certificate) (string, bool) {
	if m.cns == nil && m.dnsNames == nil && m.uris == nil {
		return identityOf(leaf), true
	}
	if _, ok := m.cns[leaf.Subject.CommonName]; ok && leaf.Subject.CommonName != "" {
		return leaf.Subject.CommonName, true
	}
	for _, name := range leaf.DNSNames {
		if _, ok := m.dnsNames[name]; ok {
			return name, true
		}
	}
	for _, uri := range leaf.URIs {
		if _, ok := m.uris[uri.String()]; ok {
			return uri.String(), true
		}
	}
	return "", false
}

// identityOf picks the most specific name available for logging, preferring
// SANs over the Common Name.
func identityOf(leaf *x509.Certificate) string {
	if len(leaf.URIs) > 0 {
		return leaf.URIs[0].String()
	}
	if len(leaf.DNSNames) > 0 {
		return leaf.DNSNames[0]
	}
	return leaf.Subject.CommonName
}
