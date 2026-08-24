package definition

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// EvaluateReadiness applies the definition's statusJSONPath to the CR and
// compares against expectedValue. Returns true when the instance is ready.
//
// JSONPath note: definitions use gjson syntax, e.g.
//
//	status.conditions.#(type=="Ready").status
//
// (no leading dot; array filters via #(...)), which is simpler and safer
// than full JSONPath for the narrow use case of reading a status value.
func EvaluateReadiness(sd *ServiceDefinition, cr *unstructured.Unstructured) (bool, error) {
	raw, err := cr.MarshalJSON()
	if err != nil {
		return false, fmt.Errorf("marshal CR for readiness: %w", err)
	}
	path := strings.TrimPrefix(sd.Spec.Readiness.StatusJSONPath, ".")
	res := gjson.Get(string(raw), path)
	if !res.Exists() {
		return false, nil // condition not present yet → not ready
	}
	expected := sd.Spec.Readiness.ExpectedValue
	if expected == "" {
		expected = "True"
	}
	return strings.EqualFold(res.String(), expected), nil
}

// ExtractCredentials converts secret byte-data into the OSB credentials map,
// optionally filtered to the keys listed in the definition. Values become
// UTF-8 strings; binary values are base64-encoded by the Secret API already
// being decoded at read time.
func ExtractCredentials(secretData map[string][]byte, filter []string) map[string]interface{} {
	out := make(map[string]interface{}, len(secretData))
	if len(filter) == 0 {
		for k, v := range secretData {
			out[k] = string(v)
		}
		return out
	}
	for _, k := range filter {
		if v, ok := secretData[k]; ok {
			out[k] = string(v)
		}
	}
	return out
}
