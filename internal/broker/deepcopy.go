package broker

// Tiefe Kopien fuer die StateStore-Datensaetze.
//
// Ein persistenter Store serialisiert und ist dadurch automatisch vom
// Aufrufer entkoppelt. Der In-Memory-Store kopierte die Struktur nur flach
// (`cp := *i`), teilte also Parameters, Credentials, AppliedObjects und
// AppliedRefs weiterhin mit dem Aufrufer: eine Aenderung nach dem Put - oder
// am gelesenen Ergebnis - schlug still auf den Speicher durch. Getestet wurde
// das nie, weil es beide Stores unterschiedlich betraf; der gemeinsame
// Vertrag in statestore_contract_test.go verlangt es jetzt von beiden.
//
// Bewusst kein Umweg ueber JSON: das wuerde Zahlen in Parameters zu float64
// machen und damit den Typ aendern, den der Aufrufer hineingegeben hat.

func deepCopyValue(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		return deepCopyMap(t)
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, e := range t {
			out[i] = deepCopyValue(e)
		}
		return out
	default:
		// Skalare sind unveraenderlich; alles andere kommt in OSB-Nutzdaten
		// nicht vor, weil sie aus JSON stammen.
		return v
	}
}

func deepCopyMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = deepCopyValue(v)
	}
	return out
}

func deepCopyStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// DeepCopy gibt eine vollstaendig entkoppelte Kopie der Instanz zurueck.
func (i *Instance) DeepCopy() *Instance {
	if i == nil {
		return nil
	}
	out := *i
	out.Parameters = deepCopyMap(i.Parameters)
	out.AppliedObjects = deepCopyStrings(i.AppliedObjects)
	if i.AppliedRefs != nil {
		out.AppliedRefs = make([]AppliedObjectRef, len(i.AppliedRefs))
		copy(out.AppliedRefs, i.AppliedRefs)
	}
	return &out
}

// DeepCopy gibt eine vollstaendig entkoppelte Kopie des Bindings zurueck.
func (b *Binding) DeepCopy() *Binding {
	if b == nil {
		return nil
	}
	out := *b
	out.Parameters = deepCopyMap(b.Parameters)
	out.Credentials = deepCopyMap(b.Credentials)
	if b.VolumeMounts != nil {
		out.VolumeMounts = make([]interface{}, len(b.VolumeMounts))
		for i, v := range b.VolumeMounts {
			out.VolumeMounts[i] = deepCopyValue(v)
		}
	}
	return &out
}
