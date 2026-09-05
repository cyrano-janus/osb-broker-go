package definition

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"text/template"
)

// SanitizeInstanceName makes an arbitrary OSB instance_id safe as a
// Kubernetes object name (DNS label): keeps [a-z0-9-], lowercases the rest,
// always prefixes "osb-" (some operators' webhooks reject bare GUID-style
// names even when formally valid — e.g. CloudNativePG 1.24), and hashes
// over-long names to stay <= 63 chars while remaining deterministic.
func SanitizeInstanceName(instanceID string) string {
	const maxLen = 63
	const prefix = "osb-"
	var b []byte
	for _, r := range strings.ToLower(instanceID) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b = append(b, byte(r))
		default:
			b = append(b, '-')
		}
	}
	trimmed := strings.Trim(string(b), "-")
	// Idempotenz: eine bereits präfixierte ID nicht doppelt präfixieren
	// (relevant, wenn Aufrufer versehentlich safeName statt instance_id senden).
	var name string
	if strings.HasPrefix(trimmed, prefix) {
		name = trimmed
	} else if trimmed == "" {
		name = prefix + "instance"
	} else {
		name = prefix + trimmed
	}
	if len(name) > maxLen {
		sum := sha256.Sum256([]byte(instanceID))
		suffix := hex.EncodeToString(sum[:4])
		cut := name[:maxLen-len(suffix)-1]
		name = strings.Trim(cut, "-") + "-" + suffix
		if !strings.HasPrefix(name, prefix) {
			name = prefix + strings.Trim(name[len(prefix):], "-")
		}
	}
	return name
}

// TemplateData is the dot available inside ServiceDefinition templates.
// Both spellings are supported: .InstanceID/.Plan/.Parameters (Go style)
// and .instanceID/.plan/.parameters (as used in roadmap examples).
type TemplateData struct {
	// InstanceID is the OSB instance_id.
	InstanceID string
	// SafeName is the DNS-label-safe object name derived from InstanceID.
	SafeName string
	// BindingID is the OSB binding_id.
	BindingID string
	// Plan holds the effective configuration: the plan's params, overlaid
	// with those user parameters the plan allows.
	Plan map[string]interface{}
	// Parameters holds the user-supplied request parameters alone.
	Parameters map[string]interface{}
}

// dot ist der Punkt im Template. Beide Schreibweisen stehen nebeneinander in
// derselben Map: die YAML-nahe Kleinschreibung (.instanceID, .plan,
// .parameters) und die Go-Schreibweise (.InstanceID, .Plan, .Parameters).
//
// Frueher wurde dafuer zweimal gerendert - erst gegen eine Kleinschreibungs-
// Map, bei Fehlschlag gegen die Struktur. Das verfaelschte jede Fehlermeldung:
// ein fehlender Schluessel unter `.parameters` liess den ersten Lauf
// scheitern, und gemeldet wurde der Fehler des zweiten - "can't evaluate field
// parameters in type definition.TemplateData", also ein Feldname, an dem
// nichts falsch war. Mit einer Map gibt es einen Lauf und eine Meldung, die
// den wirklich fehlenden Schluessel nennt.
func (t TemplateData) dot() map[string]interface{} {
	return map[string]interface{}{
		"instanceID": t.InstanceID, "InstanceID": t.InstanceID,
		"safeName": t.SafeName, "SafeName": t.SafeName,
		"bindingID": t.BindingID, "BindingID": t.BindingID,
		"plan": t.Plan, "Plan": t.Plan,
		"parameters": t.Parameters, "Parameters": t.Parameters,
	}
}

// overlay legt over ueber base: jeder Schluessel aus over ersetzt den
// gleichnamigen aus base, alles Uebrige bleibt stehen. Sind beide leer, ist
// das Ergebnis nil - ein leeres Objekt wuerde sonst als `parameters: {}` im
// Datensatz landen.
//
// Die Kopie ist nicht optional: PlanByID gibt einen Zeiger in die geladene
// Definition zurueck. Ein Schreiben auf plan.Params wuerde den Vorgabewert
// fuer jede weitere Instanz desselben Plans veraendern.
func overlay(base, over map[string]interface{}) map[string]interface{} {
	if len(base) == 0 && len(over) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(base)+len(over))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		out[k] = v
	}
	return out
}

// ParamsEqual meldet, ob zwei Parametersaetze denselben Inhalt tragen. Nil und
// die leere Map gelten als gleich; der Vergleich laeuft ueber dieselbe
// JSON-Normalisierung wie der Abgleich gegen das Cluster, damit eine Zahl aus
// dem Request und dieselbe Zahl aus dem gespeicherten CR sich nicht wegen
// int64 gegen float64 unterscheiden.
func ParamsEqual(a, b map[string]interface{}) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return valuesEqual(a, b)
}

// RenderProvision renders the provision CR manifest for an instance.
// Templates may contain multiple YAML documents separated by `---`
// (multi-doc, 4.6); the rendered output keeps the separator so
// SplitManifests can slice it.
//
// userParams sind die Parameter aus dem Request. Sie ueberschreiben unter
// `.plan` den gleichnamigen Planwert und stehen zusaetzlich unter
// `.parameters` fuer sich. Damit bleibt ein Template wie
// `{{ .plan.storageSize }}` unveraendert und liefert je nach Fall den Plan-
// oder den Benutzerwert; welche Schluessel ueberhaupt gesetzt werden duerfen,
// entscheidet allowedParameters des Plans.
func RenderProvision(sd *ServiceDefinition, instanceID string, planParams, userParams map[string]interface{}) (string, error) {
	return renderTemplate(sd.Spec.Provision.Template, TemplateData{
		InstanceID: instanceID,
		// SafeName is the K8s-validated object name derived from the
		// (possibly unsanitary) OSB instance_id. Templates that create
		// objects should use {{ .safeName }}; {{ .instanceID }} stays
		// available for labels/annotations where arbitrary values are OK.
		SafeName:   SanitizeInstanceName(instanceID),
		Plan:       overlay(planParams, userParams),
		Parameters: userParams,
	})
}

// SplitManifests slices a (possibly multi-document) YAML string into its
// non-empty documents. Leading separators and blank documents are dropped;
// each returned doc is trimmed and carries no leading `---`.
func SplitManifests(in string) []string {
	var docs []string
	for _, part := range strings.Split(in, "\n---") {
		d := strings.TrimSpace(part)
		if d == "" {
			continue
		}
		docs = append(docs, d)
	}
	return docs
}

// RenderSecretName renders the credentials secret name for an instance.
func RenderSecretName(sd *ServiceDefinition, instanceID string) (string, error) {
	return renderTemplate(sd.Spec.Bind.CredentialsFromSecret, TemplateData{
		InstanceID: instanceID,
		SafeName:   SanitizeInstanceName(instanceID),
	})
}

// templateFuncs adds helpers available inside ServiceDefinition templates.
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		// upper converts a string to uppercase (e.g. for Secret keys).
		"upper": strings.ToUpper,
	}
}

func renderTemplate(tmpl string, data TemplateData) (string, error) {
	t, err := template.New("def").Funcs(templateFuncs()).Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("invalid template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data.dot()); err != nil {
		return "", fmt.Errorf("template execute: %w", err)
	}
	return buf.String(), nil
}
