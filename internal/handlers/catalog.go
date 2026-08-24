package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// definitionCatalog converts the engine's entries into the OSB services
// response shape.
func (h *Handlers) definitionServices() []map[string]interface{} {
	if h.engine == nil || h.engine.Engine == nil {
		return nil
	}
	entries := h.engine.Engine.Catalog()
	out := make([]map[string]interface{}, 0, len(entries))
	for _, e := range entries {
		plans := make([]map[string]interface{}, 0, len(e.Plans))
		for _, p := range e.Plans {
			plan := map[string]interface{}{
				"id":   p.ID,
				"name": p.Name,
				"free": true,
			}
			if p.Description != "" {
				plan["description"] = p.Description
			}
			plans = append(plans, plan)
		}
		svc := map[string]interface{}{
			"id":        e.ID,
			"name":      e.Name,
			"bindable":  e.Bindable,
			"plans":     plans,
		}
		if e.Description != "" {
			svc["description"] = e.Description
		}
		if len(e.Tags) > 0 {
			svc["tags"] = e.Tags
		}
		out = append(out, svc)
	}
	return out
}

// mergeCatalog appends definition-based services to the broker catalog.
func (h *Handlers) mergedCatalog() (map[string]interface{}, error) {
	base, err := h.broker.GetCatalog()
	if err != nil {
		return nil, err
	}
	result := map[string]interface{}{"services": base.Services}
	defs := h.definitionServices()
	if len(defs) > 0 {
		all := make([]interface{}, 0, len(base.Services)+len(defs))
		for _, s := range base.Services {
			all = append(all, s)
		}
		for _, d := range defs {
			all = append(all, d)
		}
		result["services"] = all
	}
	return result, nil
}

// GetCatalog handles GET /v2/catalog — merges static and definition-based
// services.
func (h *Handlers) GetCatalog(c *gin.Context) {
	catalog, err := h.mergedCatalog()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":       "InternalServerError",
			"description": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, catalog)
}
