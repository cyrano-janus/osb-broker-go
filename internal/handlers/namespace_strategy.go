package handlers

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/cyrano-janus/osb-broker-go/internal/broker"
)

// DefaultInstanceNamespace ist der Namespace, in dem die Instanzen liegen,
// solange der Betreiber nichts anderes sagt.
//
// Ein fester Name ist die einzige Vorgabe, die auf JEDER Plattform traegt: er
// setzt nichts darueber voraus, was die Plattform an Kubernetes-Objekten
// anlegt, und er braucht kein Recht, das der Broker nicht ohnehin hat.
const DefaultInstanceNamespace = "osb-instances"

// NamespaceStrategy bestimmt, in welchen Namespace die Ressourcen einer
// Instanz gehoeren (ADR 0010).
//
// Vorher leitete der Broker das aus der Space-GUID ab - eine Annahme ueber die
// Plattform, die nur aufging, solange sie je Space einen Namespace anlegte.
// Cloud Foundry tut das nicht: Spaces sind Datensaetze im Cloud Controller,
// keine Kubernetes-Objekte. Deshalb sagt es jetzt der Betreiber.
type NamespaceStrategy struct {
	tmpl *template.Template
	// Create erlaubt dem Broker, einen fehlenden Namespace anzulegen.
	//
	// Ausdruecklich einzuschalten, weil er ihn nie wieder entfernt: OSB kennt
	// das Loeschen einer INSTANZ, nicht das Ende eines Mandanten. Wer das
	// setzt, nimmt in Kauf, dass die Namespaces sich ansammeln.
	Create bool
}

// NewNamespaceStrategy uebersetzt die Konfiguration in eine Strategie.
//
// Ein leeres Template bedeutet die Vorgabe. Fehler fallen HIER auf, beim
// Laden - ein Broker, der mit einem Tippfehler im Template startet und erst
// bei der ersten Bestellung scheitert, hat den Fehler nur verschoben.
func NewNamespaceStrategy(tmplText string, create bool) (*NamespaceStrategy, error) {
	if strings.TrimSpace(tmplText) == "" {
		tmplText = DefaultInstanceNamespace
	}
	// missingkey=error macht aus einem Tippfehler einen Fehler statt eines
	// stillen "<no value>" - sonst hiesse der Namespace woertlich "osb-".
	t, err := template.New("instanceNamespace").Option("missingkey=error").Parse(tmplText)
	if err != nil {
		return nil, fmt.Errorf("INSTANCE_NAMESPACE_TEMPLATE is not a valid template: %w", err)
	}
	s := &NamespaceStrategy{tmpl: t, Create: create}

	// Probelauf gegen einen vollstaendigen Kontext: er faengt jeden Feldnamen
	// ab, den es nicht gibt. Der Wert selbst ist dabei egal - geprueft wird,
	// ob das Template ueberhaupt ausfuehrbar ist.
	if _, err := s.render(kontextFelder(broker.Context{
		Platform: "probe", OrganizationGUID: "probe", OrganizationName: "probe",
		SpaceGUID: "probe", SpaceName: "probe",
	})); err != nil {
		return nil, fmt.Errorf("INSTANCE_NAMESPACE_TEMPLATE cannot be evaluated: %w", err)
	}
	return s, nil
}

// kontextFelder ist die Sicht, die das Template auf den Provision-Kontext hat.
//
// Bewusst nur diese fuenf: es sind die Felder, die Cloud Foundry in jedem
// Provision mitschickt (context_hash im Cloud Controller). Die Annotationen
// der Org und des Space stehen ABSICHTLICH nicht hier - sie setzt der Mandant
// selbst, und eine Platzierungsentscheidung gehoert dem Betreiber (ADR 0010).
func kontextFelder(ctx broker.Context) map[string]string {
	return map[string]string{
		"platform":  ctx.Platform,
		"orgGUID":   ctx.OrganizationGUID,
		"orgName":   ctx.OrganizationName,
		"spaceGUID": ctx.SpaceGUID,
		"spaceName": ctx.SpaceName,
	}
}

func (s *NamespaceStrategy) render(felder map[string]string) (string, error) {
	var buf bytes.Buffer
	if err := s.tmpl.Execute(&buf, felder); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

// Resolve liefert den Namespace fuer einen Provision-Kontext.
//
// Ein unbrauchbares Ergebnis wird ABGELEHNT, nicht zurechtgebogen. Ein
// stillschweigend verstuemmelter Name fuehrt zwei Mandanten in denselben
// Namespace, und das faellt niemandem auf.
func (s *NamespaceStrategy) Resolve(ctx broker.Context) (string, error) {
	if s == nil {
		return DefaultInstanceNamespace, nil
	}
	name, err := s.render(kontextFelder(ctx))
	if err != nil {
		return "", fmt.Errorf("cannot determine the target namespace: %w", err)
	}
	if name == "" {
		return "", fmt.Errorf(
			"the target namespace template produced an empty name - instances would land in the broker's own namespace")
	}
	if problems := validation.IsDNS1123Label(name); len(problems) > 0 {
		return "", fmt.Errorf(
			"the target namespace template produced %q, which is not a valid namespace name: %s",
			name, strings.Join(problems, "; "))
	}
	return name, nil
}
