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

	// accepts_incomplete ist ein QUERY-Parameter, kein Body-Feld. Als Feld
	// modelliert wurde er nie gefuellt, der Async-Apparat lief deshalb leer
	// und der Broker meldete "fertig", sobald das CR angelegt war - bei
	// CloudNativePG Minuten zu frueh.
	acceptsIncomplete := c.Query("accepts_incomplete") == "true"

	// Phase 2: definition-based services take precedence.
	if sd, _ := h.resolveDefinition(req.ServiceID); sd != nil {
		h.provisionDefinitionWithRequest(c, instanceID, req, acceptsIncomplete)
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
		// OSB 2.17: ein Update auf eine unbekannte Instanz ist 404. Ohne
		// diese Pruefung rendert die Engine das Manifest und legt die Instanz
		// an - ein Update, das provisioniert, und zwar im Rueckfall-Namespace
		// `default` statt im Space, weil ein PATCH keinen context traegt.
		if !h.instanceKnown(instanceID) {
			c.JSON(http.StatusNotFound, gin.H{
				"error":       "NotFound",
				"description": "instance not found",
			})
			return
		}

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

	// service_id ist laut Spezifikation empfohlen, nicht Pflicht. Fehlt es,
	// kommt der Service aus dem gespeicherten Datensatz - sonst faellt die
	// Abfrage auf den Fallback-Pfad zurueck, der hart "succeeded" meldet, und
	// die Plattform haelt eine Instanz fuer fertig, die noch entsteht.
	serviceID := c.Query("service_id")
	if serviceID == "" {
		if inst, err := h.broker.StoredInstance(c.Request.Context(), instanceID); err == nil && inst != nil {
			serviceID = inst.ServiceID
		}
	}

	// Phase 2: definition-based services derive state from CR readiness.
	if serviceID != "" {
		if sd, _ := h.resolveDefinition(serviceID); sd != nil {
			// Ebenso hier: ohne den gespeicherten Namespace suchte
			// last_operation in "default", fand nichts und fiel auf den
			// Legacy-Pfad zurueck, der hart "succeeded" meldet - Erfolg fuer
			// eine Instanz, die der Broker gar nicht gefunden hat
			// (FINDINGS #16).
			namespace := h.instanceNamespace(c.Request.Context(), instanceID)

			// OSB 2.17: kennt der Broker die Instanz nicht, ist die Antwort
			// 410 Gone. Genau daran erkennt die Plattform, dass ein
			// Deprovision durch ist - Korifi liest 410 als Abschluss.
			if !h.instanceKnown(instanceID) {
				c.JSON(http.StatusGone, gin.H{
					"error":       "Gone",
					"description": "instance not found",
				})
				return
			}

			state, err := h.engine.Engine.LastOperation(c.Request.Context(), sd, namespace, instanceID)
			if err != nil {
				// Der Datensatz existiert, das Objekt nicht: der Vorgang ist
				// gescheitert, nicht "noch unterwegs". Ohne diesen Zweig
				// pollte die Plattform bis in ihr eigenes Zeitlimit.
				h.observeLastOperation("failed")
				c.JSON(http.StatusOK, broker.LastOperationResponse{
					State:       "failed",
					Description: "the provisioned resource is gone: " + err.Error(),
					Operation:   operation,
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
