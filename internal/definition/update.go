package definition

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// UpdateInstance applies a plan change to an existing instance's CR:
// re-render the provision template with the new plan's params and ApplyCR
// (create-or-update). Returns done=true when the update was applied
// synchronously; async flows are not used by this broker.
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

	// No-op detection: if the live CR already matches the rendered desired
	// state (spec + labels), do not touch it — a no-op PATCH still bumps
	// resourceVersion and wakes up operator reconciles.
	existing, err := e.op.GetCR(ctx, sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, namespace, instanceID)
	if err == nil && crMatchesRendered(existing, rendered) {
		return true, nil
	}

	if err := e.op.ApplyCR(ctx, sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, namespace, rendered); err != nil {
		return false, fmt.Errorf("update %s %q: %w", sd.Spec.Provision.Kind, instanceID, err)
	}
	return true, nil
}

// crMatchesRendered compares the live CR against the rendered manifest on
// the fields the broker owns: metadata.labels and spec.
func crMatchesRendered(live *unstructured.Unstructured, renderedYAML string) bool {
	desired := &unstructured.Unstructured{}
	if err := yaml.Unmarshal([]byte(renderedYAML), &desired.Object); err != nil {
		return false // cannot prove equality → apply
	}
	liveSpec, okLive, _ := unstructured.NestedMap(live.Object, "spec")
	wantSpec, okWant, _ := unstructured.NestedMap(desired.Object, "spec")
	if okLive != okWant || !mapsEqual(liveSpec, wantSpec) {
		return false
	}
	liveLabels := live.GetLabels()
	wantLabels := desired.GetLabels()
	return mapsEqual(toIfaceMap(liveLabels), toIfaceMap(wantLabels))
}

func toIfaceMap(in map[string]string) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func mapsEqual(a, b map[string]interface{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok {
			return false
		}
		// Numeric normalization: JSON/YAML paths produce float64/int64/int.
		an, aIsNum := toFloat(av)
		bn, bIsNum := toFloat(bv)
		if aIsNum && bIsNum {
			if an != bn {
				return false
			}
			continue
		}
		if fmt.Sprint(av) != fmt.Sprint(bv) {
			return false
		}
	}
	return true
}

func toFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case int64:
		return float64(n), true
	case int:
		return float64(n), true
	case float64:
		return n, true
	default:
		return 0, false
	}
}

// ValidatePlanParams rejects user-supplied parameters that the plan does not
// explicitly allow via allowedParameters. Plan params are operator-managed
// sizing knobs — unknown keys are almost always typos or injection attempts.
// `parameters` may be nil.
func ValidatePlanParams(plan *Plan, parameters map[string]interface{}) error {
	if len(parameters) == 0 {
		return nil
	}
	allowed := make(map[string]bool, len(plan.AllowedParameters))
	for _, k := range plan.AllowedParameters {
		allowed[k] = true
	}
	for key := range parameters {
		if !allowed[key] {
			return fmt.Errorf("%w: parameter %q is not allowed in plan %q", ErrNotFound, key, plan.Name)
		}
	}
	return nil
}
