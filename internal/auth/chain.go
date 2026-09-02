package auth

import "net/http"

// Chain tries several authenticators in order; the first success wins.
//
// Order matters twice: it decides which Identity a request that carries two
// kinds of credentials gets, and it fixes the order of the WWW-Authenticate
// challenges on a 401. config.resolveMethods normalises it to (basic, mtls)
// so a basic-only deployment still emits exactly the header it emitted
// before Phase 4.5.
type Chain struct {
	auths []Authenticator
}

// NewChain builds a chain, dropping unconfigured (nil) authenticators.
func NewChain(as ...Authenticator) *Chain {
	c := &Chain{}
	for _, a := range as {
		if a == nil {
			continue
		}
		c.auths = append(c.auths, a)
	}
	return c
}

// Enabled reports whether any authenticator is configured. A disabled chain
// leaves every endpoint open, which is the documented behaviour when no
// credentials are configured at all.
func (c *Chain) Enabled() bool { return c != nil && len(c.auths) > 0 }

// Challenges returns the WWW-Authenticate values to send on a 401, in
// registration order, omitting authenticators that have nothing to advertise.
func (c *Chain) Challenges() []string {
	if c == nil {
		return nil
	}
	var out []string
	for _, a := range c.auths {
		if ch := a.Challenge(); ch != "" {
			out = append(out, ch)
		}
	}
	return out
}

// Authenticate runs the chain and returns the first accepted identity.
//
// When every authenticator reported ErrNoCredentials the request simply
// carried nothing; if any of them rejected presented credentials the result
// is ErrInvalidCredentials. Callers must not tell the two apart in the
// response - that would be an oracle for probing which methods are enabled.
func (c *Chain) Authenticate(r *http.Request) (*Identity, error) {
	if c == nil {
		return nil, ErrNoCredentials
	}
	sawInvalid := false
	for _, a := range c.auths {
		id, err := a.Authenticate(r)
		if err == nil {
			return id, nil
		}
		if !isNoCredentials(err) {
			sawInvalid = true
		}
	}
	if sawInvalid {
		return nil, ErrInvalidCredentials
	}
	return nil, ErrNoCredentials
}
