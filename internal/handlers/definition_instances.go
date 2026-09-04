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
func (h *Handlers) provisionDefinitionWithRequest(c *gin.Context, instanceID string, req broker.ProvisionRequest, acceptsIncomplete bool) {
	if req.ServiceID == "" || req.PlanID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "BadRequest", "description": "service_id and plan_id are required"})
		return
	}

	sd, err := h.resolveDefinition(req.ServiceID)
	if err != nil || sd == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "BadRequest", "description": "unknown definition service"})
		return
	}

	// OSB 2.17: ein wiederholtes Provision derselben Instanz mit denselben
	// Parametern ist 200, nicht 201 - die Plattform wiederholt Requests, und
	// ein zweites 201 liest sie als "neu angelegt". Weichen Service oder Plan
	// ab, ist es 409.
	if known, err := h.broker.StoredInstance(c.Request.Context(), instanceID); err == nil && known != nil {
		if known.ServiceID != req.ServiceID || known.PlanID != req.PlanID {
			c.JSON(http.StatusConflict, gin.H{
				"error":       "Conflict",
				"description": "instance already exists with different service_id or plan_id",
			})
			return
		}
		c.JSON(http.StatusOK, broker.ProvisionResponse{
			DashboardURL: "https://dashboard.example.com/instances/" + instanceID,
		})
		return
	}

	// OSB 2.17: kann der Broker nur asynchron provisionieren und hat der
	// Aufrufer das nicht erlaubt, ist die Antwort 422 AsyncRequired - nicht
	// ein 201, das Fertigstellung behauptet.
	//
	// Der Definitions-Pfad IST asynchron: er legt ein CR an, fertig ist der
	// Dienst erst, wenn der Operator ihn hergestellt hat. Bei CloudNativePG
	// sind das Minuten. Ein synchrones 201 fuehrte genau dazu, dass die
	// Plattform gegen ein Secret bindet, das es noch nicht gibt.
	if !acceptsIncomplete {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":       "AsyncRequired",
			"description": "This service plan requires client support for asynchronous service operations.",
		})
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
		DashboardURL: "https://dashboard.example.com/instances/" + instanceID,
		Operation:    provisionOperation,
	})
}

// provisionOperation ist die Kennung, die der Broker mit dem 202 zurueckgibt
// und die die Plattform bei last_operation wieder mitschickt.
//
// Ein Wert genuegt: der Zustand wird bei jeder Abfrage frisch aus dem CR
// gelesen, es gibt also keinen Vorgang, den der Broker sich merken muesste.
const provisionOperation = "provision"

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
	namespace := h.instanceNamespace(c.Request.Context(), instanceID)

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
			h.observeDeprovision(serviceID, "gone")
			c.JSON(http.StatusGone, gin.H{"error": "Gone", "description": "instance not found"})
			return
		}
		h.observeDeprovision(serviceID, "error")
		respondOSBError(c, err)
		return
	}
	h.observeDeprovision(serviceID, "ok")
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

// defaultNamespace gilt, wo die Plattform keinen Space kennt.
const defaultNamespace = "default"

// targetNamespace bildet den Cloud-Foundry-Space auf einen Namespace ab.
// Korifi legt seine Space-Namespaces genau unter der Space-GUID an.
func targetNamespace(ctx broker.Context) string {
	if ctx.SpaceGUID != "" {
		return ctx.SpaceGUID
	}
	return defaultNamespace
}

// instanceNamespace ermittelt, in welchem Namespace die Ressourcen einer
// bestehenden Instanz liegen.
//
// Aus dem Request ist das grundsaetzlich nicht herleitbar: ein
// OSB-Deprovision, ein last_operation oder ein Bind tragen weder context noch
// space_guid. Frueher stand hier hart "default" - und weil
// OperatorClient.Delete ein IsNotFound ignoriert, meldete ein Deprovision im
// falschen Namespace Erfolg, waehrend die Datenbank weiterlief (FINDINGS #7).
//
// Drei Stufen, absteigend nach Verlaesslichkeit:
//  1. das beim Provision gespeicherte Namespace-Feld,
//  2. der Namespace der angelegten Objekte - fuer Datensaetze, die vor der
//     Einfuehrung des Feldes geschrieben wurden,
//  3. "default", damit sich das Verhalten fuer alles Uebrige nicht aendert.
func (h *Handlers) instanceNamespace(ctx context.Context, instanceID string) string {
	inst, err := h.broker.StoredInstance(ctx, instanceID)
	if err != nil || inst == nil {
		return defaultNamespace
	}
	if inst.Namespace != "" {
		return inst.Namespace
	}
	for _, ref := range inst.AppliedRefs {
		if ref.Namespace != "" {
			return ref.Namespace
		}
	}
	return defaultNamespace
}
