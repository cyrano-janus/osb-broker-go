package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// GetCatalog handles GET /v2/catalog
func (h *Handlers) GetCatalog(c *gin.Context) {
	catalog, err := h.broker.GetCatalog()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":       "InternalServerError",
			"description": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, catalog)
}