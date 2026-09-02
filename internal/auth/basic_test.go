package auth

import (
	"encoding/base64"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func basicRequest(t *testing.T, header string) *http.Request {
	t.Helper()
	r, err := http.NewRequest("GET", "/v2/catalog", nil)
	require.NoError(t, err)
	if header != "" {
		r.Header.Set("Authorization", header)
	}
	return r
}

func basicHeader(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

func TestNewBasic_UnconfiguredIsNil(t *testing.T) {
	// Both credentials empty means basic auth was not configured. The
	// constructor returns a nil Authenticator so NewChain can drop it - a
	// typed nil pointer behind a non-nil interface would silently reject
	// every request instead.
	assert.Nil(t, NewBasic("", "", "osb-broker"))

	// One of the two is enough to configure it, matching the pre-4.5 check.
	assert.NotNil(t, NewBasic("u", "", "osb-broker"))
	assert.NotNil(t, NewBasic("", "p", "osb-broker"))
}

func TestBasic_ValidCredentials(t *testing.T) {
	a := NewBasic("broker-user", "broker-secret", "osb-broker")

	id, err := a.Authenticate(basicRequest(t, basicHeader("broker-user", "broker-secret")))
	require.NoError(t, err)
	require.NotNil(t, id)
	assert.Equal(t, "basic", id.Method)
	assert.Equal(t, "broker-user", id.Subject)
}

func TestBasic_WrongCredentialsAreInvalidNotMissing(t *testing.T) {
	a := NewBasic("broker-user", "broker-secret", "osb-broker")

	for _, hdr := range []string{
		basicHeader("broker-user", "wrong"),
		basicHeader("wrong", "broker-secret"),
		basicHeader("", ""),
	} {
		id, err := a.Authenticate(basicRequest(t, hdr))
		assert.Nil(t, id)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrInvalidCredentials), "header %q", hdr)
		assert.False(t, errors.Is(err, ErrNoCredentials), "header %q", hdr)
	}
}

func TestBasic_NothingPresentedIsNoCredentials(t *testing.T) {
	a := NewBasic("broker-user", "broker-secret", "osb-broker")

	// No header at all, another scheme, and malformed payloads all mean
	// "this method has nothing to work with" so the chain can try the next.
	headers := []string{
		"",
		"Bearer abc123",
		"Basic !!!not-base64!!!",
		"basic " + base64.StdEncoding.EncodeToString([]byte("no-colon")),
	}
	for _, hdr := range headers {
		id, err := a.Authenticate(basicRequest(t, hdr))
		assert.Nil(t, id)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrNoCredentials), "header %q", hdr)
	}
}

func TestBasic_NameAndChallenge(t *testing.T) {
	a := NewBasic("u", "p", "osb-broker")
	assert.Equal(t, "basic", a.Name())
	// The exact string the pre-4.5 middleware emitted; conformance check
	// auth-enforcement and basicauth_test.go both depend on it.
	assert.Equal(t, `Basic realm="osb-broker"`, a.Challenge())
}
