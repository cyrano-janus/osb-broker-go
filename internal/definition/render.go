package definition

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// TemplateData is the dot available inside ServiceDefinition templates.
// Both spellings are supported: .InstanceID/.Plan/.Parameters (Go style)
// and .instanceID/.plan/.parameters (as used in roadmap examples).
type TemplateData struct {
	// InstanceID is the OSB instance_id.
	InstanceID string
	// BindingID is the OSB binding_id (bind templates only).
	BindingID string
	// Plan holds the selected plan's params map (address as .Plan.<key>).
	Plan map[string]interface{}
	// Parameters are the free-form parameters sent by the platform.
	Parameters map[string]interface{}
}

// lowerCase returns an alias map exposing the same data with lowercase
// keys (.instanceID, .bindingID, .plan, .parameters) for YAML-flavoured
// template readability.
func (t TemplateData) lowerCase() map[string]interface{} {
	return map[string]interface{}{
		"instanceID":  t.InstanceID,
		"bindingID":   t.BindingID,
		"plan":        t.Plan,
		"parameters":  t.Parameters,
	}
}

// RenderProvision renders the provision CR manifest for an instance.
func RenderProvision(sd *ServiceDefinition, instanceID string, planParams map[string]interface{}) (string, error) {
	return renderTemplate(sd.Spec.Provision.Template, TemplateData{
		InstanceID: instanceID,
		Plan:       planParams,
	})
}

// RenderSecretName renders the credentials secret name for an instance.
func RenderSecretName(sd *ServiceDefinition, instanceID string) (string, error) {
	return renderTemplate(sd.Spec.Bind.CredentialsFromSecret, TemplateData{
		InstanceID: instanceID,
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
