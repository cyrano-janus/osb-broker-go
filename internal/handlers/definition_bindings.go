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

	// OSB 2.17: ein wiederholtes Bind derselben Binding-ID mit denselben
	// Parametern ist 200, nicht 201. Ohne Datensatz konnte der Broker den
	// Unterschied gar nicht kennen - er antwortete jedes Mal 201, und
	// GET binding fand nie etwas.
	if known, err := h.broker.StoredBinding(c.Request.Context(), bindingID); err == nil && known != nil {
		if known.ServiceID != req.ServiceID || known.InstanceID != instanceID {
			c.JSON(http.StatusConflict, gin.H{
				"error":       "Conflict",
				"description": "binding already exists with different service_id or instance",
			})
			return
		}
		c.JSON(http.StatusOK, broker.BindResponse{Credentials: known.Credentials})
		return
	}

	// Der Bind-Request traegt keine Space-GUID. Der Namespace gehoert zur
	// Instanz, nicht zum Bind - also aus dem Datensatz.
	namespace := h.instanceNamespace(c.Request.Context(), instanceID)
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

	// Den Datensatz anlegen, sonst weiss der Broker nach der Antwort nichts
	// mehr von diesem Binding: GET binding liefe ins Leere, ein Deprovision
	// koennte bestehende Bindings nicht erkennen, und eine Wiederholung waere
	// nicht von einem neuen Bind zu unterscheiden.
	if err := h.broker.RecordBinding(c.Request.Context(), &broker.Binding{
		ID:          bindingID,
		InstanceID:  instanceID,
		ServiceID:   req.ServiceID,
		PlanID:      req.PlanID,
		AppGUID:     req.AppGUID,
		Credentials: creds,
		Ready:       true,
	}); err != nil {
		respondOSBError(c, err)
		return
	}

	h.observeBind(req.ServiceID)

	c.JSON(http.StatusCreated, broker.BindResponse{Credentials: creds})
}

func isNotFoundErr(err error) bool {
	return errors.Is(err, definition.ErrNotFound)
}
