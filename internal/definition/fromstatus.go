package definition

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/tidwall/gjson"
)

// StatusQuelle holt einen Wert aus dem Status eines Kubernetes-Objekts und
// stellt ihn dem Bind-Mapping als `.fromStatus.<Name>` bereit.
//
// **Warum es das braucht.** Der Operator schreibt in sein Secret, was INNERHALB
// des Clusters gilt - CloudNativePG etwa den blanken Servicenamen. Eine
// Cloud-Foundry-Anwendung laeuft davon abgeschottet und erreicht das nie
// (FINDINGS #32). Die Adresse, die zaehlt, steht dann im Status eines anderen
// Objekts: der externen Adresse eines Service vom Typ LoadBalancer.
//
// Der Broker kann diese Adresse nicht raten und soll sie auch nicht selbst
// herstellen - das waere Arbeit des Operators (ADR 0011). Er liest sie dort,
// wo sie steht, und reicht sie durch.
type StatusQuelle struct {
	// Name ist der Schluessel unter `.fromStatus` im Mapping.
	Name string `json:"name"`
	// APIVersion und Kind benennen das Objekt. Es muss NICHT das provisionierte
	// CR sein - meist ist es ein zweites Dokument aus demselben Template.
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	// ObjectName ist ein Go-Template ueber .instanceID und .safeName. Leer
	// bedeutet: das provisionierte CR selbst.
	ObjectName string `json:"objectName,omitempty"`
	// JSONPath ist ein gjson-Pfad ueber das GANZE Objekt - dieselbe Schreibweise
	// wie bei readiness.statusJSONPath, damit niemand zwei Dialekte lernen muss.
	JSONPath string `json:"jsonPath"`
}

func (q StatusQuelle) validate(i int) error {
	switch {
	case q.Name == "":
		return fmt.Errorf("spec.bind.fromStatus[%d].name is required", i)
	case q.APIVersion == "":
		return fmt.Errorf("spec.bind.fromStatus[%d] (%s): apiVersion is required", i, q.Name)
	case q.Kind == "":
		return fmt.Errorf("spec.bind.fromStatus[%d] (%s): kind is required", i, q.Name)
	case q.JSONPath == "":
		return fmt.Errorf("spec.bind.fromStatus[%d] (%s): jsonPath is required", i, q.Name)
	}
	return nil
}

// aufloesenStatusQuellen liest alle Quellen und gibt sie als Zeichenketten
// zurueck.
//
// **Drei Fehlerfaelle, und sie sind ausdruecklich verschieden.** Wer sie
// zusammenwirft, schickt den Betreiber an die falsche Stelle: einmal fehlt ein
// Objekt, einmal ist der Pfad falsch, und einmal ist nur noch nichts da.
func (e *Engine) aufloesenStatusQuellen(ctx context.Context, sd *ServiceDefinition,
	namespace, instanceID string) (map[string]string, error) {

	quellen := sd.Spec.Bind.FromStatus
	if len(quellen) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(quellen))
	for _, q := range quellen {
		name := SanitizeInstanceName(instanceID)
		if q.ObjectName != "" {
			gerendert, err := renderTemplate(q.ObjectName, TemplateData{
				InstanceID: instanceID,
				SafeName:   SanitizeInstanceName(instanceID),
			})
			if err != nil {
				return nil, fmt.Errorf("bind fromStatus %q: objectName: %w", q.Name, err)
			}
			name = gerendert
		}

		obj, err := e.op.GetCR(ctx, q.APIVersion, q.Kind, namespace, name)
		if err != nil {
			return nil, fmt.Errorf(
				"bind fromStatus %q: %s %q (%s) in namespace %q nicht lesbar: %w",
				q.Name, q.Kind, name, q.APIVersion, namespace, err)
		}
		roh, err := obj.MarshalJSON()
		if err != nil {
			return nil, fmt.Errorf("bind fromStatus %q: %s %q: %w", q.Name, q.Kind, name, err)
		}

		pfad := strings.TrimPrefix(q.JSONPath, ".")
		treffer := gjson.Get(string(roh), pfad)
		wert := strings.TrimSpace(treffer.String())

		if !treffer.Exists() || wert == "" {
			// **Ein fehlender Wert und ein falscher Pfad sehen gleich aus** -
			// beide Male findet gjson nichts. Unterschieden wird am tiefsten
			// Pfad, der wirklich existiert: steht die Struktur schon da und
			// fehlt nur das Blatt, wartet der Operator noch. Fehlt schon der
			// zweite Abschnitt, zeigt der Pfad ins Leere.
			//
			// Der Unterschied ist keine Feinheit: einmal soll der Betreiber
			// warten, einmal seine Definition anfassen.
			tiefster := tiefsterVorhandenerPfad(string(roh), pfad)
			if strings.Count(tiefster, ".") >= 1 {
				return nil, fmt.Errorf(
					"bind fromStatus %q: %s %q fuehrt %q, aber %q ist noch nicht vergeben - "+
						"spaeter erneut binden",
					q.Name, q.Kind, name, tiefster, q.JSONPath)
			}
			return nil, fmt.Errorf(
				"bind fromStatus %q: der Pfad %q findet in %s %q nichts. Vorhanden ist: %s",
				q.Name, q.JSONPath, q.Kind, name, vorhandeneFelder(string(roh)))
		}
		out[q.Name] = wert
	}
	return out, nil
}

// vorhandeneFelder listet auf, was im Status wirklich steht.
//
// Dieselbe Hilfe wie bei der Readiness-Diagnose: ein "Pfad findet nichts" ohne
// die Angabe, was stattdessen da ist, kostet einen Cluster-Zugriff von Hand.
func vorhandeneFelder(doc string) string {
	status := gjson.Get(doc, "status")
	if !status.Exists() {
		return "kein status am Objekt"
	}
	var felder []string
	status.ForEach(func(k, _ gjson.Result) bool {
		felder = append(felder, "status."+k.String())
		return true
	})
	if len(felder) == 0 {
		return "ein leerer status"
	}
	sort.Strings(felder)
	return strings.Join(felder, ", ")
}

// validateFromStatus prueft die Quellen beim LADEN.
//
// Besonders die erste Regel: **fromStatus ohne mapping ist wirkungslos.** Die
// Werte haetten keinen Weg ins Ergebnis, und ein Betreiber saehe stundenlang
// eine Definition, die aussieht, als taete sie etwas.
func (b *Bind) validateFromStatus() error {
	if len(b.FromStatus) == 0 {
		return nil
	}
	if len(b.Mapping) == 0 {
		return fmt.Errorf(
			"spec.bind.fromStatus ohne spec.bind.mapping ist wirkungslos: " +
				"die gelesenen Werte haetten keinen Weg in die Credentials")
	}
	gesehen := map[string]bool{}
	for i, q := range b.FromStatus {
		if err := q.validate(i); err != nil {
			return err
		}
		if gesehen[q.Name] {
			return fmt.Errorf("spec.bind.fromStatus[%d]: doppelter Name %q", i, q.Name)
		}
		gesehen[q.Name] = true
	}
	return nil
}

// tiefsterVorhandenerPfad gibt den laengsten Praefix des Pfades zurueck, den es
// im Dokument wirklich gibt.
func tiefsterVorhandenerPfad(doc, pfad string) string {
	teile := strings.Split(pfad, ".")
	tiefster := ""
	for i := range teile {
		kandidat := strings.Join(teile[:i+1], ".")
		if !gjson.Get(doc, kandidat).Exists() {
			break
		}
		tiefster = kandidat
	}
	return tiefster
}
