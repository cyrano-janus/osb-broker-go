package handlers

import (

	"net/http"

	"github.com/example/osb-broker/internal/broker"
	"github.com/gin-gonic/gin"
)

// ProvisionServiceInstance handles PUT /v2/service_instances/:instance_id
func (h *Handlers) ProvisionServiceInstance(c *gin.Context) {
	instanceID := c.Param("instance_id")

	var req broker.ProvisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":       "BadRequest",
			"description": "Invalid request body: " + err.Error(),
		})
		return
	}

	// Validate required fields
	if req.ServiceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":       "BadRequest",
			"description": "service_id is required",
		})
		return
	}

	if req.PlanID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":       "BadRequest",
			"description": "plan_id is required",
		})
		return
	}

	// Check for async support
	if req.AcceptsIncomplete {
		// For this reference implementation, we support async but execute synchronously
	}

	// Phase 2: definition-based services take precedence.
	if sd, _ := h.resolveDefinition(req.ServiceID); sd != nil {
		h.provisionDefinitionWithRequest(c, instanceID, req)
		return
	}

	response, err := h.broker.Provision(c.Request.Context(), instanceID, &req)
	if err != nil {
		respondOSBError(c, err)
		return
	}

	// OSB spec: re-provision with identical parameters returns 200 (fetch
	// semantics), first creation returns 201.
	if h.broker.LastProvisionWasIdempotent {
		c.JSON(http.StatusOK, response)
		return
	}
	c.JSON(http.StatusCreated, response)
}

// DeprovisionServiceInstance handles DELETE /v2/service_instances/:instance_id
func (h *Handlers) DeprovisionServiceInstance(c *gin.Context) {
	instanceID := c.Param("instance_id")

	serviceID := c.Query("service_id")
	planID := c.Query("plan_id")

	// Phase 2: definition-based deprovision removes the CR.
	if sd, _ := h.resolveDefinition(serviceID); sd != nil {
		h.deprovisionWithEngine(c, instanceID, serviceID)
		return
	}

	req := &broker.DeprovisionRequest{
		ServiceID: serviceID,
		PlanID:    planID,
	}

	response, err := h.broker.Deprovision(c.Request.Context(), instanceID, req)
	if err != nil {
		c.JSON(http.StatusGone, gin.H{
			"error":       "Gone",
			"description": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, response)
}

// UpdateServiceInstance handles PATCH /v2/service_instances/:instance_id
func (h *Handlers) UpdateServiceInstance(c *gin.Context) {
	instanceID := c.Param("instance_id")

	var req broker.UpdateInstanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":       "BadRequest",
			"description": "Invalid request body: " + err.Error(),
		})
		return
	}

	// Phase 3: definition-based services update via engine (CR re-render).
	if sd, _ := h.resolveDefinition(req.ServiceID); sd != nil {
		// Der PATCH-Request traegt keinen Space; der Namespace kommt aus dem
		// gespeicherten Datensatz (FINDINGS #16).
		namespace := h.instanceNamespace(c.Request.Context(), instanceID)
		if err := ValidatePlanParamsForService(h, req.ServiceID, req.PlanID, req.Parameters); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "BadRequest", "description": err.Error()})
			return
		}
		if _, err := h.engine.Engine.UpdateInstance(c.Request.Context(), req.ServiceID, instanceID, namespace, req.PlanID); err != nil {
			respondOSBError(c, err)
			return
		}
		c.JSON(http.StatusOK, broker.UpdateInstanceResponse{Operation: "update"})
		return
	}

	response, err := h.broker.UpdateInstance(c.Request.Context(), instanceID, &req)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":       "NotFound",
			"description": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, response)
}

// GetServiceInstance handles GET /v2/service_instances/:instance_id
func (h *Handlers) GetServiceInstance(c *gin.Context) {
	instanceID := c.Param("instance_id")

	response, err := h.broker.GetInstance(c.Request.Context(), instanceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":       "NotFound",
			"description": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, response)
}

// GetLastOperation handles GET /v2/service_instances/:instance_id/last_operation
func (h *Handlers) GetLastOperation(c *gin.Context) {
	instanceID := c.Param("instance_id")
	operation := c.Query("operation")

	// Phase 2: definition-based services derive state from CR readiness.
	if serviceID := c.Query("service_id"); serviceID != "" {
		if sd, _ := h.resolveDefinition(serviceID); sd != nil {
			// Ebenso hier: ohne den gespeicherten Namespace suchte
			// last_operation in "default", fand nichts und fiel auf den
			// Legacy-Pfad zurueck, der hart "succeeded" meldet - Erfolg fuer
			// eine Instanz, die der Broker gar nicht gefunden hat
			// (FINDINGS #16).
			namespace := h.instanceNamespace(c.Request.Context(), instanceID)
			state, err := h.engine.Engine.LastOperation(c.Request.Context(), sd, namespace, instanceID)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{
					"error":       "NotFound",
					"description": err.Error(),
				})
				return
			}
			h.observeLastOperation(state)
			c.JSON(http.StatusOK, broker.LastOperationResponse{
				State:       state,
				Description: "Readiness evaluated from operator CR status",
				Operation:   operation,
			})
			return
		}
	}

	response, err := h.broker.GetLastOperation(instanceID, operation)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":       "NotFound",
			"description": err.Error(),
		})
		return
	}
	h.observeLastOperation(response.State)

	c.JSON(http.StatusOK, response)
}