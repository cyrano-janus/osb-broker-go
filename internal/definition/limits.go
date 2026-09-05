package definition

import (
	"fmt"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
)

// Kontingente. Ein Plan beschreibt Groessen ueber `params`; ohne Grenzen
// erzwingt er sie nicht. Steht ein Schluessel in `allowedParameters`, ist
// jeder Wert dafuer erlaubt - der Plan "small" mit 1Gi laesst sich dann mit
// 10Ti provisionieren.
//
// Verglichen wird ueber resource.Quantity. Das deckt beide Faelle mit
// derselben Schreibweise ab: eine blosse Zahl ("3") ebenso wie eine
// Kubernetes-Mengenangabe ("10Gi"). Ein Wert, der sich nicht lesen laesst,
// wird abgelehnt und nicht durchgewinkt - sonst umginge jede unlesbare Angabe
// die Grenze.

// checkLimits prueft die Benutzerparameter gegen die Grenzen des Plans.
func checkLimits(plan *Plan, parameters map[string]interface{}) error {
	for key, raw := range parameters {
		limit, ok := plan.ParameterLimits[key]
		if !ok || !limit.HasBounds() {
			continue
		}
		if err := limit.check(key, plan.Name, raw); err != nil {
			return err
		}
	}
	return nil
}

func (l ParameterLimit) check(key, planName string, raw interface{}) error {
	value, err := scalarString(raw)
	if err != nil {
		return fmt.Errorf("%w: parameter %q in plan %q: %v", ErrParameterNotAllowed, key, planName, err)
	}

	if len(l.OneOf) > 0 {
		for _, allowed := range l.OneOf {
			if value == allowed {
				return nil
			}
		}
		return fmt.Errorf("%w: parameter %q in plan %q must be one of [%s], got %q",
			ErrParameterNotAllowed, key, planName, strings.Join(l.OneOf, ", "), value)
	}

	got, err := resource.ParseQuantity(value)
	if err != nil {
		return fmt.Errorf("%w: parameter %q in plan %q: %q is not a comparable value",
			ErrParameterNotAllowed, key, planName, value)
	}
	if l.Max != "" {
		max := resource.MustParse(l.Max) // beim Laden geprueft, siehe validateLimits
		if got.Cmp(max) > 0 {
			return fmt.Errorf("%w: parameter %q in plan %q exceeds the limit of %s (got %s)",
				ErrParameterNotAllowed, key, planName, l.Max, value)
		}
	}
	if l.Min != "" {
		min := resource.MustParse(l.Min)
		if got.Cmp(min) < 0 {
			return fmt.Errorf("%w: parameter %q in plan %q is below the minimum of %s (got %s)",
				ErrParameterNotAllowed, key, planName, l.Min, value)
		}
	}
	return nil
}

// scalarString bringt einen JSON-Wert auf eine vergleichbare Zeichenkette.
//
// Zahlen kommen aus JSON als float64, aus YAML als int. Ohne Normalisierung
// waere 2 nicht mit "2" vergleichbar und dieselbe Grenze griffe je nach
// Herkunft des Wertes anders.
func scalarString(raw interface{}) (string, error) {
	switch v := raw.(type) {
	case string:
		return v, nil
	case int:
		return strconv.Itoa(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case bool:
		return strconv.FormatBool(v), nil
	default:
		return "", fmt.Errorf("value of type %T cannot be compared against a limit", raw)
	}
}

// validateLimits prueft die Grenzen selbst, beim Laden der Definition.
//
// Zwei Fehler fallen hier auf statt beim ersten Provision: eine Grenze auf
// einem Schluessel, den niemand setzen darf (sie kann nie greifen und
// taeuscht Schutz vor), und eine Grenze, die sich nicht lesen laesst.
func (p *Plan) validateLimits() error {
	if len(p.ParameterLimits) == 0 {
		return nil
	}
	allowed := make(map[string]bool, len(p.AllowedParameters))
	for _, k := range p.AllowedParameters {
		allowed[k] = true
	}

	for key, limit := range p.ParameterLimits {
		if !allowed[key] {
			return fmt.Errorf("plan %q: parameterLimits names %q, which is not in allowedParameters - the limit could never apply",
				p.Name, key)
		}
		for _, bound := range []struct{ name, value string }{{"max", limit.Max}, {"min", limit.Min}} {
			if bound.value == "" {
				continue
			}
			if _, err := resource.ParseQuantity(bound.value); err != nil {
				return fmt.Errorf("plan %q: parameterLimits.%s.%s = %q is not a readable quantity",
					p.Name, key, bound.name, bound.value)
			}
		}
		if len(limit.OneOf) > 0 && (limit.Max != "" || limit.Min != "") {
			return fmt.Errorf("plan %q: parameterLimits.%s combines oneOf with max/min - use one or the other",
				p.Name, key)
		}
	}
	return nil
}

// --- OSB-Plan-Schemas ---------------------------------------------------
//
// OSB 2.17 erlaubt einem Plan, im Katalog zu beschreiben, welche Parameter er
// annimmt. Eine Plattform kann damit ablehnen, bevor der Broker gefragt wird,
// und eine Oberflaeche daraus ein Formular bauen.
//
// Abgeleitet wird das aus dem, was der Broker ohnehin durchsetzt. Zwei Quellen
// fuer dieselbe Aussage liefen auseinander; hier ist es eine.

// PlanSchemas ist der `schemas`-Block eines Plans im Katalog.
type PlanSchemas struct {
	ServiceInstance InstanceSchemas `json:"service_instance"`
}

// InstanceSchemas traegt die Schemas fuer Provision und Update.
type InstanceSchemas struct {
	Create SchemaHolder `json:"create"`
	Update SchemaHolder `json:"update"`
}

// SchemaHolder ist die von OSB vorgesehene Verschachtelung um das eigentliche
// JSON Schema.
type SchemaHolder struct {
	Parameters map[string]interface{} `json:"parameters"`
}

// ParameterSchema baut das JSON Schema der Benutzerparameter dieses Plans.
//
// `additionalProperties: false` ist die Entsprechung der Allowlist: was der
// Broker mit 400 ablehnt, darf das Schema nicht erlauben.
func (p *Plan) ParameterSchema() map[string]interface{} {
	props := make(map[string]interface{}, len(p.AllowedParameters))
	for _, key := range p.AllowedParameters {
		props[key] = p.propertySchema(key)
	}
	return map[string]interface{}{
		"$schema":              "http://json-schema.org/draft-04/schema#",
		"type":                 "object",
		"additionalProperties": false,
		"properties":           props,
	}
}

// propertySchema beschreibt einen einzelnen Parameter.
func (p *Plan) propertySchema(key string) map[string]interface{} {
	out := map[string]interface{}{}
	limit, ok := p.ParameterLimits[key]
	if !ok || !limit.HasBounds() {
		// Ohne Grenze bleibt der Typ offen: der Plan sagt, dass es den
		// Schluessel gibt, nicht welche Form sein Wert hat.
		return out
	}

	if len(limit.OneOf) > 0 {
		enum := make([]interface{}, 0, len(limit.OneOf))
		for _, v := range limit.OneOf {
			enum = append(enum, v)
		}
		out["enum"] = enum
		return out
	}

	// Eine blosse Zahl laesst sich als maximum/minimum ausdruecken. Eine
	// Mengenangabe wie 10Gi ist in JSON Schema keine Zahl - sie als maximum
	// zu fuehren waere schlicht falsch. Sichtbar bleiben muss sie trotzdem,
	// sonst sieht die Plattform eine Grenze nicht, die der Broker durchsetzt.
	var notes []string
	for _, b := range []struct {
		key, field, value, word string
	}{
		{"maximum", "max", limit.Max, "at most"},
		{"minimum", "min", limit.Min, "at least"},
	} {
		if b.value == "" {
			continue
		}
		if n, err := strconv.ParseFloat(b.value, 64); err == nil {
			out[b.key] = n
			continue
		}
		notes = append(notes, fmt.Sprintf("%s %s", b.word, b.value))
	}
	if len(notes) > 0 {
		out["description"] = strings.Join(notes, ", ")
	}
	return out
}
