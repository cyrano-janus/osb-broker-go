package auth

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stub is a scripted Authenticator for chain tests.
type stub struct {
	name      string
	challenge string
	id        *Identity
	err       error
	calls     *int
}

func (s stub) Name() string      { return s.name }
func (s stub) Challenge() string { return s.challenge }
func (s stub) Authenticate(*http.Request) (*Identity, error) {
	if s.calls != nil {
		*s.calls++
	}
	return s.id, s.err
}

func okStub(name string) stub {
	return stub{name: name, challenge: name + ` realm="x"`, id: &Identity{Method: name}}
}

func missingStub(name string) stub {
	return stub{name: name, challenge: name + ` realm="x"`, err: ErrNoCredentials}
}

func invalidStub(name string) stub {
	return stub{name: name, challenge: name + ` realm="x"`, err: fmt.Errorf("%w: %s", ErrInvalidCredentials, name)}
}

func req(t *testing.T) *http.Request {
	t.Helper()
	r, err := http.NewRequest("GET", "/v2/catalog", nil)
	require.NoError(t, err)
	return r
}

func TestChain_EmptyIsDisabled(t *testing.T) {
	assert.False(t, NewChain().Enabled())
	// A nil Authenticator (an unconfigured method) must not count.
	assert.False(t, NewChain(nil).Enabled())
	assert.False(t, NewChain(NewBasic("", "", "osb-broker")).Enabled())

	assert.True(t, NewChain(okStub("basic")).Enabled())
}

func TestChain_FirstSuccessWinsAndShortCircuits(t *testing.T) {
	second := 0
	later := okStub("mtls")
	later.calls = &second

	c := NewChain(okStub("basic"), later)
	id, err := c.Authenticate(req(t))

	require.NoError(t, err)
	assert.Equal(t, "basic", id.Method)
	assert.Zero(t, second, "later authenticators must not run after a success")
}

func TestChain_SkipsMissingAndSucceedsOnLater(t *testing.T) {
	c := NewChain(missingStub("basic"), okStub("mtls"))

	id, err := c.Authenticate(req(t))
	require.NoError(t, err)
	assert.Equal(t, "mtls", id.Method)
}

func TestChain_AllMissingAggregatesToNoCredentials(t *testing.T) {
	c := NewChain(missingStub("basic"), missingStub("mtls"))

	id, err := c.Authenticate(req(t))
	assert.Nil(t, id)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoCredentials))
}

func TestChain_OneInvalidAggregatesToInvalid(t *testing.T) {
	// Credentials were presented and were wrong; that is not the same as
	// presenting nothing, even though both end in 401.
	c := NewChain(invalidStub("basic"), missingStub("mtls"))

	id, err := c.Authenticate(req(t))
	assert.Nil(t, id)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidCredentials))
	assert.False(t, errors.Is(err, ErrNoCredentials))
}

func TestChain_EmptyChainReportsNoCredentials(t *testing.T) {
	id, err := NewChain().Authenticate(req(t))
	assert.Nil(t, id)
	assert.True(t, errors.Is(err, ErrNoCredentials))
}

func TestChain_ChallengesKeepOrderAndDropEmpty(t *testing.T) {
	// mTLS is a transport-level scheme with nothing to advertise; its empty
	// challenge must not produce a blank WWW-Authenticate header.
	silent := okStub("mtls")
	silent.challenge = ""

	c := NewChain(okStub("basic"), silent)
	assert.Equal(t, []string{`basic realm="x"`}, c.Challenges())

	assert.Empty(t, NewChain(silent).Challenges())
}
