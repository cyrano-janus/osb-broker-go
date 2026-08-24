package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// osbError maps a broker error string to the OSB-correct HTTP status and
// error name. Central mapping per Phase 1.4 so every handler reports
// consistent codes:
//
//   - "instance not found" on DELETE        -> 410 Gone   (OSB spec)
//   - "binding not found" on DELETE         -> 410 Gone   (OSB spec)
//   - "not found" / unknown service or plan -> 400 Bad Request
//   - "already exists with different ..."   -> 409 Conflict
//   - "has existing bindings"               -> 409 Conflict (must unbind first)
func osbErrorStatus(err error) (status int, name string) {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "has existing bindings"):
		return http.StatusConflict, "ConcurrencyErrorException"
	case strings.Contains(msg, "already exists with different"):
		return http.StatusConflict, "RequiresAppInstanceAlreadyExists" // conflict family
	case strings.Contains(msg, "not found"):
		return http.StatusBadRequest, "BadRequest"
	default:
		return http.StatusInternalServerError, "InternalServerError"
	}
}

// respondOSBError writes the unified error body and honors the OSB rule
// that DELETE requests to non-existent resources must return 410 Gone.
func respondOSBError(c *gin.Context, err error) {
	status, name := osbErrorStatus(err)
	if c.Request.Method == http.MethodDelete && strings.Contains(err.Error(), "not found") {
		status = http.StatusGone
		name = "Gone"
	}
	c.JSON(status, gin.H{
		"error":       name,
		"description": err.Error(),
	})
}

var _ = errors.New // keep import if future sentinel errors land here
