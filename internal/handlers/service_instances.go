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

	response, err := h.broker.Provision(instanceID, &req)
	if err != nil {
		// Check error type for proper status code
		if err.Error() == "service not found" || err.Error() == "plan not found" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":       "BadRequest",
				"description": err.Error(),
			})
			return
		}
		c.JSON(http.StatusConflict, gin.H{
			"error":       "Conflict",
			"description": err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, response)
}

// DeprovisionServiceInstance handles DELETE /v2/service_instances/:instance_id
func (h *Handlers) DeprovisionServiceInstance(c *gin.Context) {
	instanceID := c.Param("instance_id")

	serviceID := c.Query("service_id")
	planID := c.Query("plan_id")

	req := &broker.DeprovisionRequest{
		ServiceID: serviceID,
		PlanID:    planID,
	}

	response, err := h.broker.Deprovision(instanceID, req)
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

	response, err := h.broker.UpdateInstance(instanceID, &req)
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

	response, err := h.broker.GetInstance(instanceID)
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

	response, err := h.broker.GetLastOperation(instanceID, operation)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":       "NotFound",
			"description": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, response)
}