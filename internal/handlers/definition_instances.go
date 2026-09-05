package handlers

import (
	"context"
	"fmt"

	"github.com/example/osb-broker/internal/broker"
	"github.com/example/osb-broker/internal/definition"
)

// definitionFor loest die ServiceDefinition zu einer service_id auf.
//
// Es gibt keine Rueckfallebene. Kennt die Engine den Service nicht, ist das
// ErrServiceUnknown und damit 400 - frueher fiel der Request an dieser Stelle
// stumm auf einen zweiten Broker mit eigenem Katalog.
func (h *Handlers) definitionFor(serviceID string) (*definition.ServiceDefinition, error) {
	if serviceID == "" {
		return nil, fmt.Errorf("%w: service_id is required", definition.ErrServiceUnknown)
	}
	if h.engine == nil || h.engine.Engine == nil {
		return nil, fmt.Errorf("%w: no service definitions are loaded", definition.ErrServiceUnknown)
	}
	return h.engine.Engine.DefinitionByServiceID(serviceID)
}

// provisionOperation ist die Kennung, die der Broker mit dem 202 zurueckgibt
// und die die Plattform bei last_operation wieder mitschickt.
//
// Ein Wert genuegt: der Zustand wird bei jeder Abfrage frisch aus dem CR
// gelesen, es gibt also keinen Vorgang, den der Broker sich merken muesste.
const provisionOperation = "provision"

// instanceKnown reports whether the broker's state store knows the instance.
func (h *Handlers) instanceKnown(ctx context.Context, instanceID string) bool {
	inst, err := h.broker.StoredInstance(ctx, instanceID)
	return err == nil && inst != nil
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
	return namespaceOf(inst)
}

// namespaceOf ist derselbe Dreischritt fuer einen bereits geladenen Datensatz.
// Wer die Instanz ohnehin in der Hand hat, soll sie nicht ein zweites Mal
// aus dem Zustandsspeicher holen muessen.
func namespaceOf(inst *broker.Instance) string {
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
