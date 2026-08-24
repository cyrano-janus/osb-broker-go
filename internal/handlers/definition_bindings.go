package handlers

import (
	"errors"
	"net/http"

	"github.com/example/osb-broker/internal/broker"
	"github.com/example/osb-broker/internal/definition"
	"github.com/gin-gonic/gin"
)

// bindDefinition handles binding for definition-based services: credentials
// are read from the operator-generated secret named by the definition.
func (h *Handlers) bindDefinition(c *gin.Context, instanceID, bindingID string, req broker.BindRequest) {
	sd, err := h.resolveDefinition(req.ServiceID)
	if err != nil || sd == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "BadRequest", "description": "unknown definition service"})
		return
	}

	namespace := targetNamespace(req.Context)
	creds, _, err := h.engine.Engine.BindCredentials(c.Request.Context(), sd, namespace, instanceID)
	if err != nil {
		if isNotFoundErr(err) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":       "BadRequest",
				"description": "instance not found (credentials secret missing): " + err.Error(),
			})
			return
		}
		respondOSBError(c, err)
		return
	}

	c.JSON(http.StatusCreated, broker.BindResponse{Credentials: creds})
}

func isNotFoundErr(err error) bool {
	return errors.Is(err, definition.ErrNotFound)
}
