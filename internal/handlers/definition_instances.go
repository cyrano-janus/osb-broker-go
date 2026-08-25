package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/example/osb-broker/internal/broker"
	"github.com/example/osb-broker/internal/definition"
	"github.com/gin-gonic/gin"
)

// isDefinitionService resolves the definition for an OSB service_id.
// Returns (definition, nil) when the service is definition-based, else
// (nil, nil) so callers can fall back to legacy handling.
func (h *Handlers) resolveDefinition(serviceID string) (*definition.ServiceDefinition, error) {
	if h.engine == nil || h.engine.Engine == nil {
		return nil, nil
	}
	sd, err := h.engine.Engine.DefinitionByServiceID(serviceID)
	if err != nil {
		return nil, nil // not a definition service -> legacy path
	}
	return sd, nil
}

// ProvisionDefinitionInstance handles definition-based provisioning.
func (h *Handlers) provisionDefinitionWithRequest(c *gin.Context, instanceID string, req broker.ProvisionRequest) {
	if req.ServiceID == "" || req.PlanID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "BadRequest", "description": "service_id and plan_id are required"})
		return
	}

	sd, err := h.resolveDefinition(req.ServiceID)
	if err != nil || sd == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "BadRequest", "description": "unknown definition service"})
		return
	}

	namespace := targetNamespace(req.Context)
	if err := h.engine.Engine.ProvisionInstance(c.Request.Context(), req.ServiceID, instanceID, namespace, req.PlanID, req.Parameters); err != nil {
		respondOSBError(c, err)
		return
	}

	dashboard := "https://dashboard.example.com/instances/" + instanceID
	c.JSON(http.StatusCreated, broker.ProvisionResponse{DashboardURL: dashboard})
}

// DeprovisionDefinitionInstance removes the CR for the instance.
func (h *Handlers) deprovisionDefinition(c *gin.Context, instanceID, serviceID string) {
	h.deprovisionWithEngine(c, instanceID, serviceID)
}

func (h *Handlers) deprovisionWithEngine(c *gin.Context, instanceID, serviceID string) {
	sd, err := h.resolveDefinition(serviceID)
	if err != nil || sd == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "BadRequest", "description": "unknown definition service"})
		return
	}
	namespace := targetNamespaceFromQuery(c)

	// OSB 2.17: deleting a non-existent instance is 410 Gone. The CR delete
	// alone would succeed idempotently — so check existence first: the
	// instance must be known to the broker's state store.
	if !h.instanceKnown(instanceID) {
		c.JSON(http.StatusGone, gin.H{
			"error":       "Gone",
			"description": "instance not found",
		})
		return
	}

	if err := h.engine.Engine.DeprovisionInstance(c.Request.Context(), sd, namespace, instanceID); err != nil {
		if errors.Is(err, definition.ErrNotFound) {
			c.JSON(http.StatusGone, gin.H{"error": "Gone", "description": "instance not found"})
			return
		}
		respondOSBError(c, err)
		return
	}
	c.JSON(http.StatusOK, broker.DeprovisionResponse{})
}

// instanceKnown reports whether the broker's state store knows the instance.
func (h *Handlers) instanceKnown(instanceID string) bool {
	_, err := h.broker.GetInstance(context.Background(), instanceID)
	return err == nil
}

// ValidatePlanParamsForService resolves the plan and validates user-supplied
// parameters against the plan's allowedParameters whitelist.
func ValidatePlanParamsForService(h *Handlers, serviceID, planID string, parameters map[string]interface{}) error {
	sd, err := h.engine.Engine.DefinitionByServiceID(serviceID)
	if err != nil {
		return err
	}
	return sd.ValidatePlanParameters(planID, parameters)
}

func targetNamespace(ctx broker.Context) string {
	if ctx.SpaceGUID != "" {
		return ctx.SpaceGUID
	}
	return "default"
}

func targetNamespaceFromQuery(c *gin.Context) string {
	return "default"
}
