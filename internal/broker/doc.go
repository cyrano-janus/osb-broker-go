// Package broker traegt zwei voneinander unabhaengige Dinge, und das ist beim
// ersten Lesen die Hauptquelle der Verwirrung.
//
// Erstens den Zustandsspeicher: die Schnittstelle StateStore und mit
// CRDStateStore ihre aktuelle Implementierung - je Datensatz ein
// OSBServiceInstance beziehungsweise OSBServiceBinding, Credentials in einem
// eigenen Secret daneben. Begruendung in
// docs/de/adr/0001-kubernetes-as-state-store.md.
//
// Zweitens, in broker.go, einen zweiten vollstaendigen OSB-Broker mit eigenem
// Katalog aus internal/store. Er bedient als Fallback alles, was
// internal/handlers keiner ServiceDefinition zuordnen kann. Was daran haengt,
// steht in docs/de/adr/0003-replace-http-layer.md.
package broker
