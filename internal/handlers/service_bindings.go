package handlers

import (
	"net/http"

	"github.com/cyrano-janus/osb-broker-go/internal/broker"
	"github.com/gin-gonic/gin"
)

// BindServiceInstance handles PUT /v2/service_instances/:instance_id/service_bindings/:binding_id
func (h *Handlers) BindServiceInstance(c *gin.Context) {
	instanceID := c.Param("instance_id")
	bindingID := c.Param("binding_id")

	var req broker.BindRequest
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

	// OSB 2.17: ein wiederholtes Bind derselben Binding-ID mit denselben
	// Parametern ist 200, nicht 201.
	if known, err := h.broker.StoredBinding(c.Request.Context(), bindingID); err == nil && known != nil {
		if known.ServiceID != req.ServiceID || known.InstanceID != instanceID {
			c.JSON(http.StatusConflict, gin.H{
				"error":       "Conflict",
				"description": "binding already exists with different service_id or instance",
			})
			return
		}
		c.JSON(http.StatusOK, broker.BindResponse{Credentials: known.Credentials})
		return
	}

	// Der Bind-Request traegt keine Space-GUID. Der Namespace gehoert zur
	// Instanz, nicht zum Bind - also aus dem Datensatz.
	namespace := h.instanceNamespace(c.Request.Context(), instanceID)
	creds, _, err := h.engine.Engine.BindCredentials(c.Request.Context(), sd, namespace, instanceID)
	if err != nil {
		respondOSBError(c, err)
		return
	}

	// Dasselbe Binding zusaetzlich als spec-konformes Secret ablegen, damit
	// Konsumenten ausserhalb von Cloud Foundry es nutzen koennen - die sehen
	// die OSB-Antwort nie. No-op ohne projectSecret.
	if _, err := h.engine.Engine.ProjectBindingSecret(
		c.Request.Context(), sd, namespace, instanceID, bindingID, creds); err != nil {
		respondOSBError(c, err)
		return
	}

	// Den Datensatz anlegen, sonst weiss der Broker nach der Antwort nichts
	// mehr von diesem Binding: GET binding liefe ins Leere, ein Deprovision
	// koennte bestehende Bindings nicht erkennen, und eine Wiederholung waere
	// nicht von einem neuen Bind zu unterscheiden.
	if err := h.broker.RecordBinding(c.Request.Context(), &broker.Binding{
		ID:          bindingID,
		InstanceID:  instanceID,
		ServiceID:   req.ServiceID,
		PlanID:      req.PlanID,
		AppGUID:     req.AppGUID,
		Credentials: creds,
		Ready:       true,
	}); err != nil {
		respondOSBError(c, err)
		return
	}

	h.observeBind(req.ServiceID)
	c.JSON(http.StatusCreated, broker.BindResponse{Credentials: creds})
}

// UnbindServiceInstance handles DELETE /v2/service_instances/:instance_id/service_bindings/:binding_id
func (h *Handlers) UnbindServiceInstance(c *gin.Context) {
	instanceID := c.Param("instance_id")
	bindingID := c.Param("binding_id")

	// OSB 2.17: das Unbind einer unbekannten Binding ist 410 Gone. Ohne
	// Datensatz war das nicht zu unterscheiden - jedes Unbind antwortete 200,
	// auch fuer eine Binding, die es nie gab.
	known, err := h.broker.StoredBinding(c.Request.Context(), bindingID)
	if err != nil || known == nil {
		c.JSON(http.StatusGone, gin.H{"error": "Gone", "description": "binding not found"})
		return
	}

	serviceID := c.Query("service_id")
	if serviceID == "" {
		serviceID = known.ServiceID
	}
	sd, err := h.definitionFor(serviceID)
	if err != nil {
		respondOSBError(c, err)
		return
	}

	// Das Credentials-Secret des Operators bleibt stehen, es gehoert ihm. Ein
	// von uns projiziertes Secret muss dagegen weg - sonst bliebe bei jedem
	// Unbind eines mit echten Zugangsdaten im Namespace liegen.
	namespace := h.instanceNamespace(c.Request.Context(), instanceID)
	if err := h.engine.Engine.DeleteBindingSecret(c.Request.Context(), sd, namespace, bindingID); err != nil {
		respondOSBError(c, err)
		return
	}

	// Datensatz mit abraeumen, sonst laege nach dem Unbind ein Eintrag mit
	// echten Zugangsdaten weiter im Zustandsspeicher - und ein erneutes Bind
	// derselben ID bekaeme die alten Credentials zurueck.
	if err := h.broker.ForgetBinding(c.Request.Context(), bindingID); err != nil {
		respondOSBError(c, err)
		return
	}

	h.observeUnbind(serviceID)
	c.JSON(http.StatusOK, broker.UnbindResponse{})
}

// GetBinding handles GET /v2/service_instances/:instance_id/service_bindings/:binding_id
func (h *Handlers) GetBinding(c *gin.Context) {
	response, err := h.broker.GetBinding(c.Request.Context(),
		c.Param("instance_id"), c.Param("binding_id"))
	if err != nil {
		respondOSBError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

// GetLastBindingOperation handles
// GET /v2/service_instances/:instance_id/service_bindings/:binding_id/last_operation
//
// Bind ist in diesem Broker synchron: die Antwort auf PUT traegt die
// Zugangsdaten, es gibt keinen Vorgang, der danach noch laeuft. Die ehrliche
// Antwort ist deshalb "succeeded" fuer eine Binding, die es gibt, und 410 Gone
// fuer eine, die es nicht gibt.
//
// Vorher stand hier eine Konstante: jede Abfrage bekam "succeeded", auch fuer
// eine Binding-ID, die nie existiert hat. Sobald ein Service Zugangsdaten
// asynchron ausstellt, wird aus dieser Stelle wieder eine echte Abfrage - der
// Unterschied ist, dass sie dann nicht mehr luegen kann, ohne dass es auffaellt.
func (h *Handlers) GetLastBindingOperation(c *gin.Context) {
	instanceID := c.Param("instance_id")
	bindingID := c.Param("binding_id")

	known, err := h.broker.StoredBinding(c.Request.Context(), bindingID)
	if err != nil || known == nil {
		c.JSON(http.StatusGone, gin.H{"error": "Gone", "description": "binding not found"})
		return
	}
	if known.InstanceID != instanceID {
		c.JSON(http.StatusGone, gin.H{
			"error":       "Gone",
			"description": "binding does not belong to instance",
		})
		return
	}

	c.JSON(http.StatusOK, broker.LastOperationResponse{
		State:       string(broker.OperationStateSucceeded),
		Description: "die Zugangsdaten stehen seit der Antwort auf das Bind bereit",
		Operation:   c.Query("operation"),
	})
}
