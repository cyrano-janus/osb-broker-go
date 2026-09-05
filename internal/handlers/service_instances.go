package handlers

import (
	"fmt"
	"net/http"

	"github.com/cyrano-janus/osb-broker-go/internal/broker"
	"github.com/cyrano-janus/osb-broker-go/internal/definition"
	"github.com/gin-gonic/gin"
)

// Ein Pfad. Jeder Request loest seine ServiceDefinition auf; gibt es keine,
// ist das ein Fehler des Aufrufers und keine stille Rueckfallebene.
//
// Vorher verzweigte jeder Handler einzeln ueber resolveDefinition. Lieferte es
// nil - auch im Fehlerfall -, lief der Request in einen zweiten Broker mit
// eigenem Katalog und eigenen Maps. Ein Tippfehler in der service_id sah damit
// aus wie ein anderer Service, und die Antwort kam von einer Attrappe.

// ProvisionServiceInstance handles PUT /v2/service_instances/:instance_id
func (h *Handlers) ProvisionServiceInstance(c *gin.Context) {
	instanceID := c.Param("instance_id")

	var req broker.ProvisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "Invalid request body: "+err.Error())
		return
	}
	if req.ServiceID == "" {
		badRequest(c, "service_id is required")
		return
	}
	if req.PlanID == "" {
		badRequest(c, "plan_id is required")
		return
	}

	sd, err := h.definitionFor(req.ServiceID)
	if err != nil {
		respondOSBError(c, err)
		return
	}
	if _, err := sd.PlanByID(req.PlanID); err != nil {
		respondOSBError(c, err)
		return
	}

	// OSB 2.17: ein wiederholtes Provision derselben Instanz mit denselben
	// Parametern ist 200, nicht 201 - die Plattform wiederholt Requests, und
	// ein zweites 201 liest sie als "neu angelegt". Weichen Service, Plan
	// oder Parameter ab, ist es 409.
	if known, err := h.broker.StoredInstance(c.Request.Context(), instanceID); err == nil && known != nil {
		if known.ServiceID != req.ServiceID || known.PlanID != req.PlanID ||
			!definition.ParamsEqual(known.Parameters, req.Parameters) {
			c.JSON(http.StatusConflict, gin.H{
				"error":       "Conflict",
				"description": "instance already exists with different service_id, plan_id or parameters",
			})
			return
		}
		c.JSON(http.StatusOK, broker.ProvisionResponse{DashboardURL: dashboardURL(instanceID)})
		return
	}

	// OSB 2.17: kann der Broker nur asynchron provisionieren und hat der
	// Aufrufer das nicht erlaubt, ist die Antwort 422 AsyncRequired - nicht
	// ein 201, das Fertigstellung behauptet. accepts_incomplete ist ein
	// QUERY-Parameter, kein Body-Feld.
	//
	// Dieser Broker IST asynchron: er legt ein CR an, fertig ist der Dienst
	// erst, wenn der Operator ihn hergestellt hat. Bei CloudNativePG sind das
	// Minuten.
	if c.Query("accepts_incomplete") != "true" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":       "AsyncRequired",
			"description": "This service plan requires client support for asynchronous service operations.",
		})
		return
	}

	// allowedParameters gilt beim Provision genauso wie beim Update. Vorher
	// wurde die Liste nur im PATCH geprueft: das Provision nahm jeden
	// Parameter an und verwarf ihn kommentarlos.
	if err := sd.ValidatePlanParameters(req.PlanID, req.Parameters); err != nil {
		respondOSBError(c, err)
		return
	}

	// Beide erlaubten Quellen auswerten: Korifi schickt space_guid
	// ausschliesslich als Top-Level-Feld (FINDINGS #3).
	namespace := targetNamespace(req.ResolvedContext())
	if err := h.engine.Engine.ProvisionInstance(c.Request.Context(), req.ServiceID, instanceID, namespace, req.PlanID, req.Parameters); err != nil {
		respondOSBError(c, err)
		return
	}
	h.observeProvision(req.ServiceID, req.PlanID)

	c.JSON(http.StatusAccepted, broker.ProvisionResponse{
		DashboardURL: dashboardURL(instanceID),
		Operation:    provisionOperation,
	})
}

// DeprovisionServiceInstance handles DELETE /v2/service_instances/:instance_id
func (h *Handlers) DeprovisionServiceInstance(c *gin.Context) {
	instanceID := c.Param("instance_id")
	serviceID := c.Query("service_id")

	// service_id ist bei DELETE laut Spezifikation Pflicht, aber die Instanz
	// kennt ihren Service selbst. Fehlt die Angabe, ist das kein Grund, den
	// Dienst stehen zu lassen.
	if serviceID == "" {
		if inst, err := h.broker.StoredInstance(c.Request.Context(), instanceID); err == nil && inst != nil {
			serviceID = inst.ServiceID
		}
	}

	// OSB 2.17: das Loeschen einer unbekannten Instanz ist 410 Gone. Genau
	// daran erkennt die Plattform, dass ein Deprovision durch ist.
	inst, err := h.broker.StoredInstance(c.Request.Context(), instanceID)
	if err != nil || inst == nil {
		c.JSON(http.StatusGone, gin.H{"error": "Gone", "description": "instance not found"})
		return
	}

	sd, err := h.definitionFor(serviceID)
	if err != nil {
		respondOSBError(c, err)
		return
	}

	// Eine Instanz mit bestehenden Bindings darf nicht verschwinden - sonst
	// zeigt die Binding einer Anwendung auf einen Dienst, den es nicht mehr
	// gibt. Diese Pruefung konnte fuer Definitions-Services frueher nie
	// zuschlagen, weil deren Bindings gar nicht gespeichert wurden.
	if bindings, err := h.broker.BindingsOfInstance(c.Request.Context(), instanceID); err == nil {
		for _, b := range bindings {
			if b.Ready {
				c.JSON(http.StatusConflict, gin.H{
					"error":       "Conflict",
					"description": "instance has existing bindings",
				})
				return
			}
		}
	}

	namespace := h.instanceNamespace(c.Request.Context(), instanceID)
	if err := h.engine.Engine.DeprovisionInstance(c.Request.Context(), sd, namespace, instanceID); err != nil {
		h.observeDeprovision(serviceID, "error")
		respondOSBError(c, err)
		return
	}
	h.observeDeprovision(serviceID, "ok")
	c.JSON(http.StatusOK, broker.DeprovisionResponse{})
}

// UpdateServiceInstance handles PATCH /v2/service_instances/:instance_id
func (h *Handlers) UpdateServiceInstance(c *gin.Context) {
	instanceID := c.Param("instance_id")

	var req broker.UpdateInstanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "Invalid request body: "+err.Error())
		return
	}

	// OSB 2.17: ein Update auf eine unbekannte Instanz ist 404. Ohne diese
	// Pruefung rendert die Engine das Manifest und legt die Instanz an - ein
	// Update, das provisioniert, und zwar im Rueckfall-Namespace statt im
	// Space, weil ein PATCH keinen context traegt.
	//
	// Ein Lesevorgang genuegt: derselbe Datensatz liefert Service, Plan und
	// Namespace, von denen der PATCH keinen mitschickt.
	inst, err := h.broker.StoredInstance(c.Request.Context(), instanceID)
	if err != nil || inst == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "NotFound", "description": "instance not found"})
		return
	}

	serviceID := req.ServiceID
	if serviceID == "" {
		serviceID = inst.ServiceID
	}
	sd, err := h.definitionFor(serviceID)
	if err != nil {
		respondOSBError(c, err)
		return
	}

	// plan_id ist im PATCH optional. Fehlt es, gilt der Plan, unter dem die
	// Instanz angelegt wurde - sonst scheitert ein reines Parameter-Update an
	// einem unbekannten Plan "".
	planID := req.PlanID
	if planID == "" {
		planID = inst.PlanID
	}

	// Ein Planwechsel, den der Katalog nicht zusagt, darf nicht stillschweigend
	// stattfinden.
	//
	// Der Broker kann ihn technisch: das Manifest wird mit den Werten des
	// neuen Plans neu gerendert. Ob der Operator mitgeht, weiss nur die
	// Definition - CNPG laesst Speicher wachsen und nicht schrumpfen. Und der
	// Wechsel aendert mehr als Groessen: er kann eine Instanz auf einen Plan
	// mit retainOnDeprovision schieben, womit ein spaeteres Deprovision die
	// Daten stehen laesst. Wer den Wechsel nicht zusagt, muss ihn ablehnen.
	//
	// OSB 2.17 sieht dafuer 422 vor: "MUST be returned if the requested change
	// is not supported". Derselbe Plan ist kein Wechsel.
	if planID != inst.PlanID && !planChangeAllowed(sd) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error": "PlanChangeNotSupported",
			"description": fmt.Sprintf(
				"service %q does not support changing the plan (plan_updateable is false)", sd.Spec.Offering.Name),
		})
		return
	}

	// Der PATCH-Request traegt keinen Space; der Namespace kommt aus dem
	// gespeicherten Datensatz (FINDINGS #16).
	namespace := namespaceOf(inst)
	if err := ValidatePlanParamsForService(h, serviceID, planID, req.Parameters); err != nil {
		respondOSBError(c, err)
		return
	}
	if _, err := h.engine.Engine.UpdateInstance(c.Request.Context(), serviceID, instanceID, namespace, planID, req.Parameters); err != nil {
		respondOSBError(c, err)
		return
	}
	c.JSON(http.StatusOK, broker.UpdateInstanceResponse{Operation: "update"})
}

// planChangeAllowed meldet, ob die Definition den Planwechsel zusagt. Ohne
// Angabe gilt er als nicht zugesagt: was der Operator nicht nachweislich kann,
// darf der Broker nicht tun.
func planChangeAllowed(sd *definition.ServiceDefinition) bool {
	return sd != nil && sd.Spec.Offering.PlanUpdateable != nil && *sd.Spec.Offering.PlanUpdateable
}

// GetServiceInstance handles GET /v2/service_instances/:instance_id
func (h *Handlers) GetServiceInstance(c *gin.Context) {
	response, err := h.broker.GetInstance(c.Request.Context(), c.Param("instance_id"))
	if err != nil {
		respondOSBError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

// GetLastOperation handles GET /v2/service_instances/:instance_id/last_operation
func (h *Handlers) GetLastOperation(c *gin.Context) {
	instanceID := c.Param("instance_id")
	operation := c.Query("operation")

	// service_id ist laut Spezifikation empfohlen, nicht Pflicht. Fehlt es,
	// kommt der Service aus dem gespeicherten Datensatz.
	serviceID := c.Query("service_id")
	if serviceID == "" {
		if inst, err := h.broker.StoredInstance(c.Request.Context(), instanceID); err == nil && inst != nil {
			serviceID = inst.ServiceID
		}
	}

	// OSB 2.17: kennt der Broker die Instanz nicht, ist die Antwort 410 Gone.
	// Genau daran erkennt die Plattform, dass ein Deprovision durch ist -
	// Korifi liest 410 als Abschluss.
	if !h.instanceKnown(c.Request.Context(), instanceID) {
		c.JSON(http.StatusGone, gin.H{"error": "Gone", "description": "instance not found"})
		return
	}

	sd, err := h.definitionFor(serviceID)
	if err != nil {
		respondOSBError(c, err)
		return
	}

	// Ohne den gespeicherten Namespace suchte last_operation im
	// Rueckfall-Namespace und fand nichts (FINDINGS #16).
	namespace := h.instanceNamespace(c.Request.Context(), instanceID)

	state, reason, err := h.engine.Engine.LastOperation(c.Request.Context(), sd, namespace, instanceID)
	if err != nil {
		// Der Datensatz existiert, das Objekt nicht: der Vorgang ist
		// gescheitert, nicht "noch unterwegs". Ohne diesen Zweig pollte die
		// Plattform bis in ihr eigenes Zeitlimit.
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
		Description: reason,
		Operation:   operation,
	})
}

// dashboardURL ist der Platzhalter, den der Broker mit jeder Instanz
// zurueckgibt. Ein eigenes Dashboard gibt es nicht.
func dashboardURL(instanceID string) string {
	return "https://dashboard.example.com/instances/" + instanceID
}

func badRequest(c *gin.Context, description string) {
	c.JSON(http.StatusBadRequest, gin.H{"error": "BadRequest", "description": description})
}
