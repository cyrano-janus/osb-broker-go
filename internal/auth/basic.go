package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
)

// basic implements HTTP Basic Auth (RFC 7617).
//
// Credentials are kept as SHA-256 digests rather than raw bytes so the
// constant-time comparison runs over two fixed-length values. Comparing the
// raw strings - as the pre-4.5 middleware did - leaks the configured
// credential length through the compare's running time.
type basic struct {
	userHash [sha256.Size]byte
	passHash [sha256.Size]byte
	realm    string
}

// NewBasic returns a Basic Auth authenticator, or nil when neither
// credential is configured.
//
// The nil return is an Authenticator, not a *basic: a typed nil pointer
// stored in a non-nil interface would pass NewChain's nil check and then
// reject every request.
func NewBasic(user, pass, realm string) Authenticator {
	if user == "" && pass == "" {
		return nil
	}
	return &basic{
		userHash: sha256.Sum256([]byte(user)),
		passHash: sha256.Sum256([]byte(pass)),
		realm:    realm,
	}
}

func (b *basic) Name() string { return "basic" }

func (b *basic) Challenge() string { return fmt.Sprintf("Basic realm=%q", b.realm) }

func (b *basic) Authenticate(r *http.Request) (*Identity, error) {
	// http.Request.BasicAuth handles the case-insensitive scheme prefix,
	// the base64 decode and the first-colon split.
	user, pass, ok := r.BasicAuth()
	if !ok {
		return nil, ErrNoCredentials
	}

	userHash := sha256.Sum256([]byte(user))
	passHash := sha256.Sum256([]byte(pass))
	// Both comparisons always run; & rather than && so the result does not
	// depend on which half failed.
	userOK := subtle.ConstantTimeCompare(userHash[:], b.userHash[:])
	passOK := subtle.ConstantTimeCompare(passHash[:], b.passHash[:])
	if userOK&passOK != 1 {
		return nil, fmt.Errorf("%w: basic auth", ErrInvalidCredentials)
	}

	return &Identity{Method: b.Name(), Subject: user}, nil
}

// isNoCredentials reports whether err means "nothing presented".
func isNoCredentials(err error) bool { return errors.Is(err, ErrNoCredentials) }
