// Package migrate uebertraegt den Zustand aus der abgeloesten State-ConfigMap
// in die CRDs aus Phase 5.
//
// Der Umstieg ist ein harter Schnitt: der Broker liest die ConfigMap nicht
// mehr. Ohne Migration waeren bestehende Instanzen fuer ihn unsichtbar - die
// Cloud-Foundry-Seite wuesste noch von ihnen, der Broker nicht, und die
// angelegten Operator-Ressourcen (etwa laufende Datenbanken) blieben als
// Waisen zurueck.
package migrate

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/example/osb-broker/internal/broker"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DefaultConfigMapName ist der Name, unter dem der alte Store seinen Zustand
// ablegte.
const DefaultConfigMapName = "osb-broker-state"

// stateKey war der einzige Key in dieser ConfigMap - der gesamte Zustand lag
// als ein JSON-Dokument darin.
const stateKey = "state.json"

// Report fasst zusammen, was uebertragen wurde.
type Report struct {
	Instances int
	Bindings  int
	// SourceMissing meldet, dass es nichts zu tun gab: eine Neuinstallation
	// hat keine alte ConfigMap.
	SourceMissing bool
	DryRun        bool
}

// legacyInstance und legacyBinding bilden das alte Format exakt ab.
//
// Die Feldnamen sind PascalCase, weil die damaligen Structs KEINE json-Tags
// trugen und encoding/json dann die Go-Namen schreibt. Das eingebettete
// Context hatte dagegen Tags und steht deshalb in snake_case. Diese Mischung
// ist der Grund, warum die Typen hier noch einmal stehen statt die heutigen
// wiederzuverwenden: gegen die heutigen gelesen kaeme lautlos ein leerer
// Datensatz heraus.
type legacyContext struct {
	Platform            string `json:"platform"`
	OrganizationGUID    string `json:"organization_guid"`
	SpaceGUID           string `json:"space_guid"`
	ClusterID           string `json:"cluster_id"`
	Namespace           string `json:"namespace"`
	OriginatingIdentity string `json:"originating_identity"`
}

type legacyObjectRef struct {
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
}

type legacyInstance struct {
	ID             string
	ServiceID      string
	PlanID         string
	Context        legacyContext
	Parameters     map[string]interface{}
	DashboardURL   string
	Ready          bool
	AppliedObjects []string
	AppliedRefs    []legacyObjectRef
}

type legacyBinding struct {
	ID              string
	InstanceID      string
	ServiceID       string
	PlanID          string
	AppGUID         string
	Context         legacyContext
	Parameters      map[string]interface{}
	Credentials     map[string]interface{}
	SyslogDrainURL  string
	RouteServiceURL string
	VolumeMounts    []interface{}
	Ready           bool
}

type legacyState struct {
	Instances map[string]*legacyInstance `json:"instances"`
	Bindings  map[string]*legacyBinding  `json:"bindings"`
}

// Run liest die alte ConfigMap und schreibt ihren Inhalt als CRs.
//
// Der Lauf ist idempotent - ein zweiter Aufruf ueberschreibt dieselben
// Objekte - und laesst die ConfigMap stehen: solange die Migration nicht
// geprueft ist, ist sie die einzige Rueckfallebene.
func Run(ctx context.Context, c client.Client, namespace, configMapName string, dryRun bool) (Report, error) {
	report := Report{DryRun: dryRun}

	var cm corev1.ConfigMap
	err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: configMapName}, &cm)
	if apierrors.IsNotFound(err) {
		report.SourceMissing = true
		return report, nil
	}
	if err != nil {
		return report, fmt.Errorf("read %s/%s: %w", namespace, configMapName, err)
	}

	raw, ok := cm.Data[stateKey]
	if !ok || raw == "" {
		report.SourceMissing = true
		return report, nil
	}

	var state legacyState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return report, fmt.Errorf("parse %s in %s/%s: %w", stateKey, namespace, configMapName, err)
	}

	store := broker.NewCRDStateStore(c, namespace)

	for id, li := range state.Instances {
		if li == nil {
			continue
		}
		inst := toInstance(id, li)
		report.Instances++
		if dryRun {
			continue
		}
		if err := store.PutInstance(ctx, inst); err != nil {
			return report, fmt.Errorf("write instance %q: %w", inst.ID, err)
		}
	}

	for id, lb := range state.Bindings {
		if lb == nil {
			continue
		}
		bind := toBinding(id, lb)
		report.Bindings++
		if dryRun {
			continue
		}
		if err := store.PutBinding(ctx, bind); err != nil {
			return report, fmt.Errorf("write binding %q: %w", bind.ID, err)
		}
	}

	return report, nil
}

// toInstance wandelt einen alten Datensatz um. Der Map-Key gilt als ID, falls
// das Feld leer ist - der Store war darin nicht streng.
func toInstance(key string, li *legacyInstance) *broker.Instance {
	id := li.ID
	if id == "" {
		id = key
	}
	out := &broker.Instance{
		ID: id, ServiceID: li.ServiceID, PlanID: li.PlanID,
		Context:        toContext(li.Context),
		Parameters:     li.Parameters,
		DashboardURL:   li.DashboardURL,
		Ready:          li.Ready,
		AppliedObjects: li.AppliedObjects,
	}
	for _, r := range li.AppliedRefs {
		out.AppliedRefs = append(out.AppliedRefs, broker.AppliedObjectRef{
			APIVersion: r.APIVersion, Kind: r.Kind, Namespace: r.Namespace, Name: r.Name})
	}
	return out
}

func toBinding(key string, lb *legacyBinding) *broker.Binding {
	id := lb.ID
	if id == "" {
		id = key
	}
	return &broker.Binding{
		ID: id, InstanceID: lb.InstanceID, ServiceID: lb.ServiceID, PlanID: lb.PlanID,
		AppGUID:         lb.AppGUID,
		Context:         toContext(lb.Context),
		Parameters:      lb.Parameters,
		Credentials:     lb.Credentials,
		SyslogDrainURL:  lb.SyslogDrainURL,
		RouteServiceURL: lb.RouteServiceURL,
		VolumeMounts:    lb.VolumeMounts,
		Ready:           lb.Ready,
	}
}

func toContext(c legacyContext) broker.Context {
	return broker.Context{
		Platform:            c.Platform,
		OrganizationGUID:    c.OrganizationGUID,
		SpaceGUID:           c.SpaceGUID,
		ClusterID:           c.ClusterID,
		Namespace:           c.Namespace,
		OriginatingIdentity: c.OriginatingIdentity,
	}
}
