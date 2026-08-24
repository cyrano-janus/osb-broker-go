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
	name := prefix + strings.Trim(string(b), "-")
	if name == prefix {
		name = prefix + "instance"
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
	// Plan holds the plan parameters.
	Plan map[string]interface{}
	// Parameters holds user-supplied request parameters.
	Parameters map[string]interface{}
}

// aliasLower provides the lowercase template aliases (.instanceID etc.).
func (d TemplateData) instanceID() string { return d.InstanceID }
func (d TemplateData) safeName() string   { return d.SafeName }

// lowerCase returns an alias map exposing the same data with lowercase
// keys (.instanceID, .bindingID, .plan, .parameters) for YAML-flavoured
// template readability.
func (t TemplateData) lowerCase() map[string]interface{} {
	return map[string]interface{}{
		"instanceID":  t.InstanceID,
		"safeName":    t.SafeName,
		"bindingID":   t.BindingID,
		"plan":        t.Plan,
		"parameters":  t.Parameters,
	}
}

// RenderProvision renders the provision CR manifest for an instance.
func RenderProvision(sd *ServiceDefinition, instanceID string, planParams map[string]interface{}) (string, error) {
	return renderTemplate(sd.Spec.Provision.Template, TemplateData{
		InstanceID: instanceID,
		// SafeName is the K8s-validated object name derived from the
		// (possibly unsanitary) OSB instance_id. Templates that create
		// objects should use {{ .safeName }}; {{ .instanceID }} stays
		// available for labels/annotations where arbitrary values are OK.
		SafeName: SanitizeInstanceName(instanceID),
		Plan:     planParams,
	})
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
	// Execute twice: once against the lowercase alias map (YAML style),
	// falling back to the struct (Go style). A missing lowercase key with
	// missingkey=error panics the map lookup as "map has no entry", which
	// we translate into a retry against the struct.
	if err := t.Execute(&buf, data.lowerCase()); err == nil {
		return buf.String(), nil
	}
	buf.Reset()
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("template execute: %w", err)
	}
	return buf.String(), nil
}
