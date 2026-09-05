package auth

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mtlsRequest builds a request carrying the given peer certificate. verified
// controls whether crypto/tls chain verification succeeded.
func mtlsRequest(t *testing.T, leaf *x509.Certificate, verified bool) *http.Request {
	t.Helper()
	r, err := http.NewRequest("GET", "/v2/catalog", nil)
	require.NoError(t, err)
	if leaf == nil {
		return r
	}
	state := &tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf}}
	if verified {
		state.VerifiedChains = [][]*x509.Certificate{{leaf}}
	}
	r.TLS = state
	return r
}

func TestMTLS_NoCertificateIsNoCredentials(t *testing.T) {
	a := NewMTLS([]string{"osb-gate"}, nil, nil)

	// Plain HTTP request: no TLS state at all.
	id, err := a.Authenticate(mtlsRequest(t, nil, false))
	assert.Nil(t, id)
	assert.True(t, errors.Is(err, ErrNoCredentials))

	// TLS, but the client presented no certificate - Basic Auth may still
	// carry this request, so the chain must continue.
	r, err2 := http.NewRequest("GET", "/v2/catalog", nil)
	require.NoError(t, err2)
	r.TLS = &tls.ConnectionState{}
	id, err = a.Authenticate(r)
	assert.Nil(t, id)
	assert.True(t, errors.Is(err, ErrNoCredentials))
}

func TestMTLS_UnverifiedChainIsRejected(t *testing.T) {
	ca := newTestCA(t)
	leaf := ca.issueClient(t, "osb-gate", nil, nil)
	a := NewMTLS([]string{"osb-gate"}, nil, nil)

	// A certificate whose chain crypto/tls did not verify must never be
	// trusted, even if the subject matches. This guards against a
	// RequireAnyClientCert misconfiguration, which performs no verification.
	id, err := a.Authenticate(mtlsRequest(t, leaf, false))
	assert.Nil(t, id)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidCredentials))
	assert.False(t, errors.Is(err, ErrNoCredentials))
}

func TestMTLS_CommonNameAllowlist(t *testing.T) {
	ca := newTestCA(t)
	a := NewMTLS([]string{"osb-gate"}, nil, nil)

	id, err := a.Authenticate(mtlsRequest(t, ca.issueClient(t, "osb-gate", nil, nil), true))
	require.NoError(t, err)
	assert.Equal(t, "mtls", id.Method)
	assert.Equal(t, "osb-gate", id.Subject)

	// Signed by the trusted CA, but not on the allowlist. "The CA signed it"
	// is authentication, not authorisation.
	id, err = a.Authenticate(mtlsRequest(t, ca.issueClient(t, "someone-else", nil, nil), true))
	assert.Nil(t, id)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidCredentials))
}

func TestMTLS_DNSAndURISANAllowlist(t *testing.T) {
	ca := newTestCA(t)

	dnsAuth := NewMTLS(nil, []string{"broker.example.com"}, nil)
	id, err := dnsAuth.Authenticate(mtlsRequest(t, ca.issueClient(t, "irrelevant-cn", []string{"broker.example.com"}, nil), true))
	require.NoError(t, err)
	assert.Equal(t, "broker.example.com", id.Subject)

	uriAuth := NewMTLS(nil, nil, []string{"spiffe://osb/checker"})
	id, err = uriAuth.Authenticate(mtlsRequest(t, ca.issueClient(t, "irrelevant-cn", nil, []string{"spiffe://osb/checker"}), true))
	require.NoError(t, err)
	assert.Equal(t, "spiffe://osb/checker", id.Subject)

	// A CN that happens to equal an allowed DNS name must not satisfy a
	// DNS-SAN allowlist.
	id, err = dnsAuth.Authenticate(mtlsRequest(t, ca.issueClient(t, "broker.example.com", nil, nil), true))
	assert.Nil(t, id)
	assert.True(t, errors.Is(err, ErrInvalidCredentials))
}

func TestMTLS_EmptyAllowlistAcceptsAnyVerifiedCertificate(t *testing.T) {
	// Documented behaviour, and config.Load warns about it: with no
	// allowlist, trust rests entirely on the CA bundle being narrow.
	ca := newTestCA(t)
	a := NewMTLS(nil, nil, nil)

	id, err := a.Authenticate(mtlsRequest(t, ca.issueClient(t, "anybody", nil, nil), true))
	require.NoError(t, err)
	assert.Equal(t, "anybody", id.Subject)
}

func TestMTLS_NameAndChallenge(t *testing.T) {
	a := NewMTLS(nil, nil, nil)
	assert.Equal(t, "mtls", a.Name())
	// mTLS is a transport-level scheme; there is no HTTP challenge to send,
	// and an empty value must not become a blank WWW-Authenticate header.
	assert.Equal(t, "", a.Challenge())
}
