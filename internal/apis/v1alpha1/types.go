// Package v1alpha1 enthaelt die Custom Resources, in denen der Broker seinen
// Zustand haelt (Phase 5).
//
// Vorher lag der gesamte Zustand - alle Instanzen und alle Bindings - als ein
// JSON-Dokument unter einem einzigen Key in einer ConfigMap. Das hatte drei
// Folgen, die alle an derselben Ursache haengen: das 1-MiB-Limit einer
// ConfigMap wurde ab rund 500 Instanzen erreicht, jeder einzelne Schreibzugriff
// schrieb das gesamte Dokument neu, und zwei gleichzeitige Schreiber
// ueberschrieben sich gegenseitig, weil es keine Sperre pro Datensatz gab.
//
// Ein Objekt je Datensatz loest alle drei: kein Gesamtlimit, ein Schreibzugriff
// kostet die Groesse eines Datensatzes, und Kubernetes' resourceVersion gibt
// optimistische Sperren pro Objekt - womit auch mehr als eine Broker-Replica
// moeglich wird.
//
// Bewusst KEIN Status-Subresource, obwohl die Roadmap es vorschlug: eine
// Trennung von Wunsch und Beobachtung ergibt nur Sinn, wenn ein Controller die
// Objekte abgleicht. Hier schreibt sie ausschliesslich der Broker selbst, und
// ein Subresource wuerde jeden Put in zwei API-Aufrufe zerlegen, ohne dass
// jemand etwas davon haette.
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

const (
	// GroupName ist dieselbe Gruppe, unter der auch die ServiceDefinition
	// beschrieben wird - ein Projekt, eine API-Gruppe.
	GroupName = "broker.osb.io"
	Version   = "v1alpha1"

	// LabelInstance traegt den abgeleiteten Objektnamen der Instanz, zu der
	// ein Binding gehoert. Der Objektname statt der rohen OSB-ID, weil
	// Label-Werte hoechstens 63 Zeichen lang und auf ein enges Alphabet
	// beschraenkt sind - eine beliebige OSB-ID erfuellt das nicht.
	LabelInstance = GroupName + "/instance"
)

var (
	// GroupVersion identifiziert die API-Gruppe dieser Typen.
	GroupVersion = schema.GroupVersion{Group: GroupName, Version: Version}

	// SchemeBuilder registriert die Typen an einem Scheme.
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme fuegt die Typen einem Scheme hinzu.
	AddToScheme = SchemeBuilder.AddToScheme
)

func init() {
	SchemeBuilder.Register(
		&OSBServiceInstance{}, &OSBServiceInstanceList{},
		&OSBServiceBinding{}, &OSBServiceBindingList{},
	)
}

// OSBContext ist der Plattform-Kontext aus dem OSB-Request.
//
// Eigene Struktur statt broker.Context: API-Typen werden nach etcd
// serialisiert und muessen fuer sich stehen. Ein Import aus dem
// broker-Paket wuerde die Persistenzform an interne Typen koppeln - und
// umgekehrt einen Importzyklus erzeugen, sobald der Store beides braucht.
type OSBContext struct {
	Platform            string `json:"platform,omitempty"`
	OrganizationGUID    string `json:"organizationGuid,omitempty"`
	SpaceGUID           string `json:"spaceGuid,omitempty"`
	ClusterID           string `json:"clusterId,omitempty"`
	Namespace           string `json:"namespace,omitempty"`
	OriginatingIdentity string `json:"originatingIdentity,omitempty"`
}

// AppliedObjectRef benennt ein Kubernetes-Objekt, das fuer eine Instanz
// angelegt wurde. Traegt den Namespace mit, damit ein spaeterer Wechsel von
// "alles in default" auf Namespaces je Space (FINDINGS #3/#16) keine
// Schema-Aenderung braucht.
type AppliedObjectRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
}

// OSBServiceInstanceSpec beschreibt eine Service-Instanz.
type OSBServiceInstanceSpec struct {
	// ID ist die OSB-Instanz-ID, wie die Plattform sie geschickt hat. Der
	// Objektname ist davon abgeleitet und nicht immer gleich, weil OSB-IDs
	// beliebige Strings sein duerfen; die Wahrheit steht hier.
	ID           string     `json:"id"`
	ServiceID    string     `json:"serviceId"`
	PlanID       string     `json:"planId"`
	Context      OSBContext `json:"context,omitempty"`
	DashboardURL string     `json:"dashboardUrl,omitempty"`
	// Ready meldet, ob das Provisioning abgeschlossen ist.
	Ready bool `json:"ready,omitempty"`
	// Parameters sind die frei geformten Benutzerparameter aus dem
	// Provision-Request.
	Parameters *runtime.RawExtension `json:"parameters,omitempty"`
	// AppliedObjects und AppliedRefs halten fest, was fuer diese Instanz
	// angelegt wurde, damit Deprovision auch nach einem Neustart alles
	// wieder abraeumt (Multi-Doc, Phase 4.6).
	AppliedObjects []string           `json:"appliedObjects,omitempty"`
	AppliedRefs    []AppliedObjectRef `json:"appliedRefs,omitempty"`
}

// OSBServiceInstance ist der persistierte Zustand einer Service-Instanz.
type OSBServiceInstance struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec OSBServiceInstanceSpec `json:"spec,omitempty"`
}

// OSBServiceInstanceList ist eine Liste von Instanzen.
type OSBServiceInstanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OSBServiceInstance `json:"items"`
}

// OSBServiceBindingSpec beschreibt ein Service-Binding.
type OSBServiceBindingSpec struct {
	ID         string     `json:"id"`
	InstanceID string     `json:"instanceId"`
	ServiceID  string     `json:"serviceId"`
	PlanID     string     `json:"planId"`
	AppGUID    string     `json:"appGuid,omitempty"`
	Context    OSBContext `json:"context,omitempty"`
	Ready      bool       `json:"ready,omitempty"`

	Parameters      *runtime.RawExtension `json:"parameters,omitempty"`
	VolumeMounts    *runtime.RawExtension `json:"volumeMounts,omitempty"`
	SyslogDrainURL  string                `json:"syslogDrainUrl,omitempty"`
	RouteServiceURL string                `json:"routeServiceUrl,omitempty"`

	// CredentialsSecret nennt das Secret, in dem die Credentials liegen.
	//
	// Die Credentials stehen absichtlich NICHT in dieser Ressource: in der
	// ConfigMap lagen sie im Klartext (FINDINGS #19), und ein CR haette das
	// nur mit anderem Objektnamen wiederholt. Ein Secret laesst sich
	// getrennt per RBAC schuetzen und at rest verschluesseln.
	CredentialsSecret string `json:"credentialsSecret,omitempty"`
}

// OSBServiceBinding ist der persistierte Zustand eines Service-Bindings.
type OSBServiceBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec OSBServiceBindingSpec `json:"spec,omitempty"`
}

// OSBServiceBindingList ist eine Liste von Bindings.
type OSBServiceBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OSBServiceBinding `json:"items"`
}
