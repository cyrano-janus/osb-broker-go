package broker

import (
	"context"
	"fmt"
)

// Broker ist der Zugang zum Zustandsspeicher - und nur das.
//
// Frueher stand hier eine zweite, vollstaendige Broker-Implementierung:
// eigener Katalog aus internal/store, eigene Instanz- und Binding-Maps,
// eigene Provision-, Bind- und last_operation-Logik. Jeder Handler verzweigte
// einzeln zwischen ihr und der Engine, und wenn die Aufloesung der Definition
// fehlschlug - auch aus einem anderen Grund als "kenne ich nicht" -, fiel der
// Request stumm hierher. Zwei Folgen, die lange niemand sah: der Demo-Katalog
// erschien in jedem Produktivkatalog, und die eigene Konformitaetssuite prueft
// den Demo-Service statt der Engine.
//
// Es gibt jetzt einen Pfad. Was hier bleibt, ist die Buchfuehrung: welche
// Instanzen und Bindings der Broker kennt. Die Dienste selbst stellt die
// Engine her.
type Broker struct {
	state StateStore
}

// New erzeugt den Zustandszugang. Ohne StateStore wird der In-Memory-Speicher
// genommen, damit Tests ohne Cluster laufen.
func New(state StateStore) *Broker {
	if state == nil {
		state = NewInMemoryStateStore()
	}
	return &Broker{state: state}
}

// StoredInstance liefert den gespeicherten Instanz-Datensatz.
func (b *Broker) StoredInstance(ctx context.Context, instanceID string) (*Instance, error) {
	return b.state.GetInstance(ctx, instanceID)
}

// RecordInstance schreibt einen Instanz-Datensatz.
func (b *Broker) RecordInstance(ctx context.Context, i *Instance) error {
	return b.state.PutInstance(ctx, i)
}

// ForgetInstance entfernt einen Instanz-Datensatz.
func (b *Broker) ForgetInstance(ctx context.Context, instanceID string) error {
	return b.state.DeleteInstance(ctx, instanceID)
}

// GetInstance liefert die OSB-Antwort auf GET /v2/service_instances/:id.
func (b *Broker) GetInstance(ctx context.Context, instanceID string) (*GetInstanceResponse, error) {
	instance, err := b.state.GetInstance(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("%w: instance %q", ErrNotFound, instanceID)
	}
	return &GetInstanceResponse{
		ServiceID:    instance.ServiceID,
		PlanID:       instance.PlanID,
		DashboardURL: instance.DashboardURL,
		Parameters:   instance.Parameters,
	}, nil
}

// StoredBinding liefert den gespeicherten Binding-Datensatz.
func (b *Broker) StoredBinding(ctx context.Context, bindingID string) (*Binding, error) {
	return b.state.GetBinding(ctx, bindingID)
}

// RecordBinding schreibt einen Binding-Datensatz in den Zustandsspeicher.
func (b *Broker) RecordBinding(ctx context.Context, bd *Binding) error {
	return b.state.PutBinding(ctx, bd)
}

// ForgetBinding entfernt einen Binding-Datensatz aus dem Zustandsspeicher.
func (b *Broker) ForgetBinding(ctx context.Context, bindingID string) error {
	return b.state.DeleteBinding(ctx, bindingID)
}

// BindingsOfInstance liefert alle Bindings einer Instanz. Gebraucht, damit ein
// Deprovision eine Instanz mit bestehenden Bindings ablehnen kann.
func (b *Broker) BindingsOfInstance(ctx context.Context, instanceID string) ([]*Binding, error) {
	return b.state.ListBindingsByInstance(ctx, instanceID)
}

// GetBinding liefert die OSB-Antwort auf GET …/service_bindings/:id.
func (b *Broker) GetBinding(ctx context.Context, instanceID, bindingID string) (*GetBindingResponse, error) {
	binding, err := b.state.GetBinding(ctx, bindingID)
	if err != nil {
		return nil, fmt.Errorf("%w: binding %q", ErrNotFound, bindingID)
	}
	if binding.InstanceID != instanceID {
		return nil, fmt.Errorf("%w: binding %q does not belong to instance %q", ErrNotFound, bindingID, instanceID)
	}
	return &GetBindingResponse{
		Credentials:     binding.Credentials,
		SyslogDrainURL:  binding.SyslogDrainURL,
		RouteServiceURL: binding.RouteServiceURL,
		VolumeMounts:    binding.VolumeMounts,
	}, nil
}
