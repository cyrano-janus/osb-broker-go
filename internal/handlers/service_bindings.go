package handlers

import (

	"net/http"

	"github.com/example/osb-broker/internal/broker"
	"github.com/gin-gonic/gin"
)

// BindServiceInstance handles PUT /v2/service_instances/:instance_id/service_bindings/:binding_id
func (h *Handlers) BindServiceInstance(c *gin.Context) {
	instanceID := c.Param("instance_id")
	bindingID := c.Param("binding_id")

	var req broker.BindRequest
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

	// Phase 2: definition-based services bind via operator secret.
	if sd, _ := h.resolveDefinition(req.ServiceID); sd != nil {
		h.bindDefinition(c, instanceID, bindingID, req)
		return
	}

	response, err := h.broker.Bind(c.Request.Context(), instanceID, bindingID, &req)
	if err != nil {
		respondOSBError(c, err)
		return
	}

	// OSB spec: re-bind of an existing binding with identical parameters
	// returns 200 (fetch semantics), first creation returns 201.
	if h.broker.LastBindWasIdempotent {
		c.JSON(http.StatusOK, response)
		return
	}
	c.JSON(http.StatusCreated, response)
}

// UnbindServiceInstance handles DELETE /v2/service_instances/:instance_id/service_bindings/:binding_id
func (h *Handlers) UnbindServiceInstance(c *gin.Context) {
	instanceID := c.Param("instance_id")
	bindingID := c.Param("binding_id")

	serviceID := c.Query("service_id")
	planID := c.Query("plan_id")

	req := &broker.UnbindRequest{
		ServiceID: serviceID,
		PlanID:    planID,
	}

	// Phase 2: definition-based services unbind via engine (secret is not
	// deleted — it belongs to the operator).
	if sd, _ := h.resolveDefinition(req.ServiceID); sd != nil {
		c.JSON(http.StatusOK, broker.UnbindResponse{})
		return
	}

	response, err := h.broker.Unbind(c.Request.Context(), instanceID, bindingID, req)
	if err != nil {
		respondOSBError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// GetBinding handles GET /v2/service_instances/:instance_id/service_bindings/:binding_id
func (h *Handlers) GetBinding(c *gin.Context) {
	instanceID := c.Param("instance_id")
	bindingID := c.Param("binding_id")

	response, err := h.broker.GetBinding(c.Request.Context(), instanceID, bindingID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":       "NotFound",
			"description": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, response)
}

// GetLastBindingOperation handles GET /v2/service_instances/:instance_id/service_bindings/:binding_id/last_operation
func (h *Handlers) GetLastBindingOperation(c *gin.Context) {
	instanceID := c.Param("instance_id")
	bindingID := c.Param("binding_id")
	operation := c.Query("operation")

	response, err := h.broker.GetLastBindingOperation(instanceID, bindingID, operation)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":       "NotFound",
			"description": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, response)
}