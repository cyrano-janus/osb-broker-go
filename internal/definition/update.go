package definition

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// UpdateInstance applies a plan or parameter change to an existing instance's
// CR: re-render the provision template with the new configuration and apply
// all documents (create-or-update). Returns done=true when the update was
// applied synchronously; async flows are not used by this broker.
//
// planID darf leer sein - im PATCH ist plan_id optional. parameters traegt nur
// die geaenderten Schluessel; sie werden ueber die gespeicherten gelegt.
func (e *Engine) UpdateInstance(ctx context.Context, serviceID, instanceID, namespace, planID string, parameters map[string]interface{}) (bool, error) {
	sd, err := e.DefinitionByServiceID(serviceID)
	if err != nil {
		return false, err
	}
	return e.updateDefinition(ctx, sd, instanceID, namespace, planID, parameters)
}

func (e *Engine) updateDefinition(ctx context.Context, sd *ServiceDefinition, instanceID, namespace, planID string, parameters map[string]interface{}) (bool, error) {
	rec, recErr := e.regGet(ctx, instanceID)
	haveRec := recErr == nil && rec != nil

	// plan_id ist im PATCH optional (OSB 2.17, Updating a Service Instance).
	// Fehlt es, gilt der Plan, unter dem die Instanz angelegt wurde - sonst
	// scheitert jedes reine Parameter-Update an ErrPlanUnknown.
	if planID == "" && haveRec {
		planID = rec.PlanID
	}
	plan, err := sd.PlanByID(planID)
	if err != nil {
		return false, err
	}

	// Verschmelzen statt Ersetzen: gesendete Schluessel ueberschreiben die
	// gespeicherten, nicht genannte bleiben stehen. OSB 2.17 legt das nicht
	// fest; die Wahl haelt GET /v2/service_instances und die tatsaechliche
	// Konfiguration deckungsgleich, auch wenn die Plattform nur das Geaenderte
	// schickt.
	merged := parameters
	if haveRec {
		merged = overlay(rec.Parameters, parameters)
	}
	// Geprueft wird der verschmolzene Satz, nicht nur das Neue: bei einem
	// Planwechsel muss die gesamte Konfiguration im Zielplan erlaubt sein.
	if err := ValidatePlanParams(plan, merged); err != nil {
		return false, err
	}

	rendered, err := RenderProvision(sd, instanceID, plan.Params, merged)
	if err != nil {
		return false, err
	}

	// No-op detection: if every rendered document already matches its live
	// object (content + labels), do not touch anything — a no-op write still
	// bumps resourceVersion and wakes up operator reconciles.
	upToDate, err := e.op.ManifestsUpToDate(ctx,
		sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, namespace, rendered)
	if err != nil {
		return false, fmt.Errorf("update %s %q: %w", sd.Spec.Provision.Kind, instanceID, err)
	}
	// Apply all documents (multi-doc aware). For single-doc this is identical
	// to the previous ApplyCR path.
	var applied []ObjectRef
	if !upToDate {
		applied, err = e.op.ApplyManifestRefs(ctx,
			sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, namespace, rendered)
		if err != nil {
			return false, fmt.Errorf("update %s %q: %w", sd.Spec.Provision.Kind, instanceID, err)
		}
	}

	// Den Datensatz auch dann nachfuehren, wenn das Manifest gleich geblieben
	// ist: ein Planwechsel ohne Wirkung auf das Manifest und ein Parameter,
	// den das Template nicht liest, aendern den Zustand der Instanz, nicht
	// aber ihre Objekte. Ohne diesen Zweig meldete
	// GET /v2/service_instances anschliessend den alten Plan.
	if e.reg != nil && haveRec {
		rec.PlanID = planID
		rec.Parameters = merged
		if applied != nil {
			rec.AppliedObjects = refNames(applied)
			rec.AppliedRefs = applied
		}
		if err := e.reg.PutInstance(ctx, rec); err != nil {
			return true, fmt.Errorf("record instance: %w", err)
		}
	}

	return true, nil
}

// crMatchesRendered compares a live object against the rendered desired state
// on the fields the broker owns: metadata.labels plus every top-level content
// field of the desired document. apiVersion/kind are already fixed by the
// lookup, metadata is server-owned bookkeeping and status belongs to the
// operator — all four are ignored.
func crMatchesRendered(live, desired *unstructured.Unstructured) bool {
	if !mapsEqual(live.GetLabels(), desired.GetLabels()) {
		return false
	}
	for key, want := range desired.Object {
		switch key {
		case "apiVersion", "kind", "metadata", "status":
			continue
		}
		if !valuesEqual(live.Object[key], want) {
			return false
		}
	}
	return true
}

// mapsEqual compares two string maps, treating nil and empty as equal.
func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		if bv, ok := b[k]; !ok || av != bv {
			return false
		}
	}
	return true
}

// valuesEqual deep-compares two unstructured values. Both sides are pushed
// through JSON first: YAML decoding and the API server hand back the same
// number as int64 or float64 depending on the path, and only normalization
// makes `instances: 3` compare equal to itself.
func valuesEqual(a, b interface{}) bool {
	na, errA := normalizeJSON(a)
	nb, errB := normalizeJSON(b)
	if errA != nil || errB != nil {
		return false // cannot prove equality → treat as changed
	}
	return reflect.DeepEqual(na, nb)
}

func normalizeJSON(v interface{}) (interface{}, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}
