package definition

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"text/template"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Labels am projizierten Secret, damit Workloads es finden koennen.
const (
	managedByValue  = "osb-broker-go"
	LabelInstanceID = "osb.broker.osb.io/instance-id"
	LabelBindingID  = "osb.broker.osb.io/binding-id"
)

// Umsetzung der CNCF Service Binding Specification (Phase 6).
//
// Die Spezifikation dreht die bisherige Richtung um. Bisher musste die
// Definition das Namensschema des Operators nachbauen - eine Konvention, die
// bei jedem neuen Operator neu zu erraten ist und an der Valkey, Redpanda und
// NATS gescheitert sind. Ein Provisioned Service sagt stattdessen selbst, wo
// seine Credentials liegen: im Feld .status.binding.name seiner eigenen
// Ressource.

// bindingSecretPath ist der von der Spezifikation festgelegte Ort. Bewusst
// nicht konfigurierbar: waere er es, waere es wieder eine Konvention und kein
// Standard. Operatoren, die ihn nicht fuellen, bedient credentialsFromSecret.
var bindingSecretPath = []string{"status", "binding", "name"}

// resolveSecretName ermittelt, aus welchem Secret die Credentials kommen.
func (e *Engine) resolveSecretName(ctx context.Context, sd *ServiceDefinition, namespace, instanceID string) (string, error) {
	if !sd.Spec.Bind.ProvisionedService {
		return RenderSecretName(sd, instanceID)
	}

	name, err := e.provisionedServiceSecret(ctx, sd, namespace, instanceID)
	if err != nil {
		return "", err
	}
	if name != "" {
		return name, nil
	}

	// Rueckfallebene: der Operator fuellt das Feld (noch) nicht.
	if sd.Spec.Bind.CredentialsFromSecret != "" {
		return RenderSecretName(sd, instanceID)
	}
	return "", fmt.Errorf(
		"%w: %s does not expose .status.binding.name for instance %q; set spec.bind.credentialsFromSecret as a fallback",
		ErrNotFound, sd.Spec.Provision.Kind, instanceID)
}

// provisionedServiceSecret liest .status.binding.name aus dem CR. Ein leeres
// Ergebnis ist kein Fehler - der Aufrufer entscheidet ueber die Rueckfallebene.
func (e *Engine) provisionedServiceSecret(ctx context.Context, sd *ServiceDefinition, namespace, instanceID string) (string, error) {
	cr, err := e.op.GetCR(ctx, sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind,
		namespace, SanitizeInstanceName(instanceID))
	if err != nil {
		return "", err
	}
	name, found, err := unstructured.NestedString(cr.Object, bindingSecretPath...)
	if err != nil {
		// Feld vorhanden, aber kein String - eine Fehlkonfiguration des
		// Operators, die man nicht als "nicht vorhanden" durchwinken darf.
		return "", fmt.Errorf("%s %q: .status.binding.name is not a string: %w",
			sd.Spec.Provision.Kind, instanceID, err)
	}
	if !found {
		return "", nil
	}
	return name, nil
}

// shapeCredentials formt die Rohdaten des Operator-Secrets in die Zielform.
//
// Ohne mapping bleibt es beim bisherigen Verhalten: alle Keys, optional
// gefiltert durch credentialKeys. Mit mapping besteht das Ergebnis genau aus
// den gemappten Keys - ein Adapter, der daneben noch alle Originalschluessel
// durchreicht, macht die definierte Zielform zunichte.
//
// type und provider kommen in beiden Faellen hinzu, weil die Spezifikation
// type auf jedem Binding verlangt.
func shapeCredentials(b *Bind, secretData map[string][]byte) (map[string]interface{}, error) {
	raw := make(map[string]interface{}, len(secretData))
	for k, v := range secretData {
		raw[k] = string(v)
	}

	var out map[string]interface{}
	if len(b.Mapping) == 0 {
		out = ExtractCredentials(secretData, b.CredentialKeys)
	} else {
		var err error
		if out, err = applyMapping(b.Mapping, raw); err != nil {
			return nil, err
		}
	}

	// Nach dem Mapping gesetzt, damit ein Mapping-Eintrag namens "type" nicht
	// still ueberschrieben wird - die Definition hat das letzte Wort.
	if b.Type != "" {
		out["type"] = b.Type
	}
	if b.Provider != "" {
		out["provider"] = b.Provider
	}
	return out, nil
}

// applyMapping wertet die Mapping-Eintraege aus.
func applyMapping(mapping []CredentialMapping, raw map[string]interface{}) (map[string]interface{}, error) {
	out := make(map[string]interface{}, len(mapping))
	data := map[string]interface{}{"credentials": raw}

	for _, m := range mapping {
		if m.From != "" {
			v, ok := raw[m.From]
			if !ok {
				// Hart abbrechen statt still auszulassen: ein fehlender Key
				// heisst, dass die Definition nicht zum Operator passt. Ein
				// halb gefuelltes Binding faellt sonst erst in der App auf.
				return nil, fmt.Errorf("bind mapping %q: secret has no key %q", m.Name, m.From)
			}
			out[m.Name] = v
			continue
		}

		// Templates sind bereits beim Parsen der Definition geprueft; hier
		// kann nur noch ein fehlender Key auffallen (missingkey=error).
		tmpl, err := template.New(m.Name).Option("missingkey=error").Parse(m.Value)
		if err != nil {
			return nil, fmt.Errorf("bind mapping %q: %w", m.Name, err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return nil, fmt.Errorf("bind mapping %q: %w", m.Name, err)
		}
		out[m.Name] = buf.String()
	}
	return out, nil
}

// --- 6.4: spec-konformes Secret fuer Konsumenten ausserhalb von Cloud Foundry ---

// bindingSecretSuffix haengt an den abgeleiteten Namen des projizierten
// Secrets. Der Name ist rein aus der Binding-ID abgeleitet und braucht keinen
// gespeicherten Zustand - so laesst er sich beim Unbind wieder bilden.
const bindingSecretSuffix = "-binding"

// secretTypePrefix ist der von der Spezifikation vorgesehene Praefix fuer den
// Kubernetes-Secret-Type.
const secretTypePrefix = "servicebinding.io/"

// projectedSecretName leitet den Namen des projizierten Secrets ab.
func projectedSecretName(bindingID string) string {
	// SanitizeInstanceName kuerzt auf ein gueltiges DNS-1123-Label und haengt
	// bei Ueberlaenge einen Hash an; das Suffix muss in die 63 Zeichen passen.
	return SanitizeInstanceName(bindingID + bindingSecretSuffix)
}

// ProjectBindingSecret schreibt die Credentials zusaetzlich als
// spec-konformes Secret in den Ziel-Namespace.
//
// Damit wird ein Binding auch fuer Konsumenten nutzbar, die nicht ueber Cloud
// Foundry kommen: ein Workload referenziert das Secret ueber eine
// ServiceBinding-Ressource, statt die Credentials aus einer OSB-Antwort zu
// bekommen, die er nie sieht.
//
// Ohne spec.bind.projectSecret passiert nichts; der Rueckgabewert ist dann
// leer.
func (e *Engine) ProjectBindingSecret(ctx context.Context, sd *ServiceDefinition, namespace, instanceID, bindingID string, creds map[string]interface{}) (string, error) {
	if !sd.Spec.Bind.ProjectSecret {
		return "", nil
	}

	data := make(map[string][]byte, len(creds))
	for k, v := range creds {
		data[k] = []byte(fmt.Sprintf("%v", v))
	}

	labels := map[string]string{
		"app.kubernetes.io/managed-by": managedByValue,
		LabelInstanceID:                SanitizeInstanceName(instanceID),
		LabelBindingID:                 SanitizeInstanceName(bindingID),
	}
	// ExtraLabels war bisher deklariert, aber nirgends gelesen. Hier hat es
	// einen Sinn: Workloads suchen ihr Binding-Secret ueber Labels.
	for k, v := range sd.Spec.Bind.ExtraLabels {
		labels[k] = v
	}

	name := projectedSecretName(bindingID)
	// Das provisionierte CR als Owner: wird die Instanz geloescht, raeumt
	// Kubernetes das Secret mit ab - auch dann, wenn kein Unbind mehr kommt.
	owner, err := e.op.GetCR(ctx, sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind,
		namespace, SanitizeInstanceName(instanceID))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return "", err
	}

	if err := e.op.WriteSecret(ctx, namespace, name,
		corev1.SecretType(secretTypePrefix+sd.Spec.Bind.Type), data, labels, owner); err != nil {
		return "", err
	}
	return name, nil
}

// DeleteBindingSecret entfernt das projizierte Secret. Ohne das bliebe bei
// jedem Unbind ein Secret mit echten Credentials im Namespace stehen.
//
// Idempotent: ein zweites Unbind darf nicht fehlschlagen.
func (e *Engine) DeleteBindingSecret(ctx context.Context, sd *ServiceDefinition, namespace, bindingID string) error {
	if !sd.Spec.Bind.ProjectSecret {
		return nil
	}
	return e.op.DeleteSecret(ctx, namespace, projectedSecretName(bindingID))
}
