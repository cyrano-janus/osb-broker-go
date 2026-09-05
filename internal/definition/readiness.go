package definition

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// DefaultReadinessTimeout gilt, wenn eine Definition kein Zeitlimit nennt.
// Der Wert ist keine Erfindung: er steht seit jeher als `default` im
// Definitionsschema, wurde vom Go-Typ aber nie angewendet.
const DefaultReadinessTimeout = 600 * time.Second

// ReadinessTimeout liefert das Zeitlimit, innerhalb dessen der Operator die
// Readiness-Bedingung erfuellt haben muss.
//
// Fehlt die Angabe, gilt DefaultReadinessTimeout - "kein Wert" darf nicht
// "unbegrenzt" heissen, sonst haengt jede Definition, die das Feld vergisst.
// Wer wirklich kein Limit will, sagt das mit einem negativen Wert.
func ReadinessTimeout(sd *ServiceDefinition) time.Duration {
	switch n := sd.Spec.Readiness.TimeoutSeconds; {
	case n < 0:
		return 0
	case n == 0:
		return DefaultReadinessTimeout
	default:
		return time.Duration(n) * time.Second
	}
}

// readinessDeadlineExceeded meldet, ob der Operator laenger gebraucht hat als
// erlaubt, und wie lange gewartet wurde.
//
// Gemessen wird ab `metadata.creationTimestamp` des CR: das ist der Zeitpunkt,
// ab dem der Operator die Aufgabe hatte, er kommt vom API-Server und er
// ueberlebt einen Neustart des Brokers - anders als jede Uhr im Prozess. Fehlt
// der Zeitstempel, gibt es keine Grundlage fuer eine Frist; dann wird
// weitergewartet, statt eine Instanz faelschlich als gescheitert zu melden.
func readinessDeadlineExceeded(sd *ServiceDefinition, cr *unstructured.Unstructured, now time.Time) (bool, time.Duration) {
	limit := ReadinessTimeout(sd)
	created := cr.GetCreationTimestamp().Time
	if created.IsZero() {
		return false, 0
	}
	waited := now.Sub(created)
	if limit <= 0 {
		return false, waited
	}
	return waited > limit, waited
}

// EvaluateReadiness applies the definition's statusJSONPath to the CR and
// compares against expectedValue. Returns true when the instance is ready,
// plus a reason that says why it is not.
//
// JSONPath note: definitions use gjson syntax, e.g.
//
//	status.conditions.#(type=="Ready").status
//
// (no leading dot; array filters via #(...)), which is simpler and safer
// than full JSONPath for the narrow use case of reading a status value.
//
// Der Grund ist kein Beiwerk. Ein Pfad, der ins Leere zeigt, ist von "noch
// nicht bereit" nicht zu unterscheiden - der Broker wartet dann bis in das
// Zeitlimit der Plattform, ohne dass jemand erfaehrt, warum. Deshalb nennt der
// Grund, wenn der Operator einen Status geschrieben hat und der Pfad trotzdem
// nichts findet, die Conditions, die wirklich da sind.
func EvaluateReadiness(sd *ServiceDefinition, cr *unstructured.Unstructured) (bool, string, error) {
	raw, err := cr.MarshalJSON()
	if err != nil {
		return false, "", fmt.Errorf("marshal CR for readiness: %w", err)
	}
	path := strings.TrimPrefix(sd.Spec.Readiness.StatusJSONPath, ".")
	doc := string(raw)

	expected := sd.Spec.Readiness.ExpectedValue
	if expected == "" {
		expected = "True"
	}

	res := gjson.Get(doc, path)
	if !res.Exists() {
		if !gjson.Get(doc, "status").Exists() {
			return false, "der Operator hat noch keinen Status geschrieben", nil
		}
		reason := fmt.Sprintf("der Pfad %q findet im Status nichts, obwohl der Operator einen Status fuehrt", path)
		if present := conditionTypes(doc); len(present) > 0 {
			reason += " - vorhandene Conditions: " + strings.Join(present, ", ")
		}
		return false, reason, nil
	}

	if strings.EqualFold(res.String(), expected) {
		return true, "", nil
	}
	return false, fmt.Sprintf("%s steht auf %q, erwartet %q", path, res.String(), expected), nil
}

// conditionTypes liest die Condition-Typen aus dem Status, damit der Grund
// benennen kann, was der Operator statt des gesuchten Namens veroeffentlicht.
func conditionTypes(doc string) []string {
	seen := map[string]bool{}
	gjson.Get(doc, "status.conditions.#.type").ForEach(func(_, v gjson.Result) bool {
		if t := v.String(); t != "" {
			seen[t] = true
		}
		return true
	})
	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
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
