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
			"id":              e.ID,
			"name":            e.Name,
			"bindable":        e.Bindable,
			"plan_updateable": true,
			"plans":           plans,
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

// GetCatalog handles GET /v2/catalog.
//
// Der Katalog ist genau das, was die Engine aus den ServiceDefinitions kennt.
// Frueher wurde ihm ein statischer Demo-Katalog aus internal/store
// vorangestellt - service-1 und service-2 standen damit in jedem
// Produktivkatalog, und es gab keinen Schalter dagegen.
func (h *Handlers) GetCatalog(c *gin.Context) {
	services := h.definitionServices()
	if services == nil {
		services = []map[string]interface{}{}
	}
	c.JSON(http.StatusOK, gin.H{"services": services})
}
