// Package broker haelt den Zustand des Brokers - welche Instanzen und
// Bindings er kennt - und sonst nichts.
//
// Die Schnittstelle StateStore beschreibt die Buchfuehrung, CRDStateStore ist
// ihre Implementierung: je Datensatz ein OSBServiceInstance beziehungsweise
// OSBServiceBinding, Credentials in einem eigenen Secret daneben.
// Begruendung in docs/de/adr/0001-kubernetes-as-state-store.md.
//
// broker.go war frueher ein zweiter, vollstaendiger OSB-Broker mit eigenem
// Katalog, der als Rueckfallebene alles bediente, was internal/handlers keiner
// ServiceDefinition zuordnen konnte. Diese zweite Implementierung ist
// entfallen; was blieb, ist der Zugang zum Zustandsspeicher. Siehe
// docs/de/adr/0003-replace-http-layer.md.
package broker
