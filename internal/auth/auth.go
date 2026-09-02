// Package auth implements the broker's authentication methods behind one
// interface so several can be active at once (Phase 4.5).
//
// The package deliberately speaks net/http rather than gin: the chain is
// framework-agnostic, and the gin adapter lives in internal/handlers.
package auth

import (
	"errors"
	"net/http"
)

// Identity is what a successful authentication yields. It is attached to the
// request context for the access log; nothing in the OSB request handling
// depends on it yet.
type Identity struct {
	// Method is the authenticator that accepted the request ("basic",
	// "mtls").
	Method string
	// Subject identifies the caller: the Basic Auth user, or the matched
	// certificate CN/SAN.
	Subject string
	// Issuer names the credential issuer where one exists (unused until a
	// token-based method lands).
	Issuer string
	// Scopes carries authorisation scopes where the method provides them.
	Scopes []string
}

// Authenticator validates one kind of credential.
//
// The error contract is what makes several methods composable:
//
//	ErrNoCredentials      - the request carries nothing for this method, so
//	                        the chain should try the next one
//	ErrInvalidCredentials - credentials were presented and are wrong
//
// Both end in 401 for the client; the distinction only decides whether the
// chain keeps looking, and never reaches the response body.
type Authenticator interface {
	// Name is the method identifier used in logs and in Identity.Method.
	Name() string
	// Challenge is the WWW-Authenticate value to advertise on a 401, or ""
	// for schemes that have nothing to advertise (mTLS).
	Challenge() string
	// Authenticate validates the request's credentials.
	Authenticate(r *http.Request) (*Identity, error)
}

var (
	// ErrNoCredentials means the request presented nothing this
	// authenticator could act on.
	ErrNoCredentials = errors.New("auth: no credentials presented")
	// ErrInvalidCredentials means credentials were presented and rejected.
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
)
