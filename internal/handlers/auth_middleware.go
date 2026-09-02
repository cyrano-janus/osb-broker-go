package handlers

import (
	"net/http"

	"github.com/example/osb-broker/internal/auth"
	"github.com/gin-gonic/gin"
)

// authIdentityKey is the gin context key holding the *auth.Identity of an
// authenticated request.
const authIdentityKey = "auth_identity"

// SetAuthenticator wires the authentication chain (Phase 4.5). A nil or
// empty chain disables authentication, which is the documented behaviour
// when no credentials are configured.
func (h *Handlers) SetAuthenticator(chain *auth.Chain) {
	h.authChain = chain
}

// SetBasicAuthCredentials configures Basic Auth as the only authentication
// method. Empty user AND password disable authentication.
//
// Kept as the Phase 1.2 entry point: main() and several tests wire auth
// through it, and it now builds a one-element chain.
func (h *Handlers) SetBasicAuthCredentials(user, pass string) {
	h.SetAuthenticator(auth.NewChain(auth.NewBasic(user, pass, defaultRealm)))
}

// defaultRealm is the realm used when auth is wired through
// SetBasicAuthCredentials rather than from config.
const defaultRealm = "osb-broker"

// AuthIdentity returns the identity that authenticated this request, or nil
// when authentication is disabled.
func AuthIdentity(c *gin.Context) *auth.Identity {
	v, ok := c.Get(authIdentityKey)
	if !ok {
		return nil
	}
	id, _ := v.(*auth.Identity)
	return id
}

// authMiddleware enforces the configured authentication methods on all OSB
// endpoints. Any one method succeeding is enough.
//
// The 401 body is identical for every failure mode. Reporting which method
// failed, or why, would tell an unauthenticated caller which methods are
// enabled.
func (h *Handlers) authMiddleware(c *gin.Context) {
	if !h.authChain.Enabled() {
		c.Next()
		return
	}

	id, err := h.authChain.Authenticate(c.Request)
	if err != nil {
		// Add, not Set: several enabled methods mean several challenges,
		// each on its own header line per RFC 7235.
		for _, challenge := range h.authChain.Challenges() {
			c.Writer.Header().Add("WWW-Authenticate", challenge)
		}
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"error":       "Unauthorized",
			"description": "Invalid or missing credentials",
		})
		return
	}

	c.Set(authIdentityKey, id)
	c.Next()
}
