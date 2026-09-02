package handlers

import (
	"errors"
	"net/http"

	"github.com/example/osb-broker/internal/broker"
	"github.com/example/osb-broker/internal/definition"
	"github.com/gin-gonic/gin"
)

// BindDefinition creates a binding by reading the operator's credentials
// secret for the instance (name rendered from the definition).
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
			// OSB spec: bind to a non-existent instance -> 404.
			c.JSON(http.StatusNotFound, gin.H{
				"error":       "NotFound",
				"description": "instance not found (credentials secret missing): " + err.Error(),
			})
			return
		}
		respondOSBError(c, err)
		return
	}
	// Phase 6.4: dasselbe Binding zusaetzlich als spec-konformes Secret
	// ablegen, damit Konsumenten ausserhalb von Cloud Foundry es nutzen
	// koennen - die sehen die OSB-Antwort nie. No-op ohne projectSecret.
	if _, err := h.engine.Engine.ProjectBindingSecret(
		c.Request.Context(), sd, namespace, instanceID, bindingID, creds); err != nil {
		respondOSBError(c, err)
		return
	}

	h.observeBind(req.ServiceID)

	c.JSON(http.StatusCreated, broker.BindResponse{Credentials: creds})
}

func isNotFoundErr(err error) bool {
	return errors.Is(err, definition.ErrNotFound)
}
