package definition

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// UpdateInstance applies a plan change to an existing instance's CR:
// re-render the provision template with the new plan's params and apply
// all documents (create-or-update). Returns done=true when the update was
// applied synchronously; async flows are not used by this broker.
func (e *Engine) UpdateInstance(ctx context.Context, serviceID, instanceID, namespace, planID string) (bool, error) {
	sd, err := e.DefinitionByServiceID(serviceID)
	if err != nil {
		return false, err
	}
	return e.updateDefinition(ctx, sd, instanceID, namespace, planID)
}

func (e *Engine) updateDefinition(ctx context.Context, sd *ServiceDefinition, instanceID, namespace, planID string) (bool, error) {
	plan, err := sd.PlanByID(planID)
	if err != nil {
		return false, err
	}
	rendered, err := RenderProvision(sd, instanceID, plan.Params)
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
	if upToDate {
		return true, nil
	}

	// Apply all documents (multi-doc aware). For single-doc this is identical
	// to the previous ApplyCR path.
	applied, err := e.op.ApplyManifestRefs(ctx,
		sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, namespace, rendered)
	if err != nil {
		return false, fmt.Errorf("update %s %q: %w", sd.Spec.Provision.Kind, instanceID, err)
	}

	// Update the instance record with the new list of applied objects.
	if e.reg != nil {
		if rec, err := e.regGet(ctx, instanceID); err == nil {
			rec.AppliedObjects = refNames(applied)
			rec.AppliedRefs = applied
			if err := e.reg.PutInstance(ctx, rec); err != nil {
				return true, fmt.Errorf("record instance: %w", err)
			}
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
