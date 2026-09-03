// Package handlers ist die HTTP-Schicht: gin-Router, die OSB-Endpunkte,
// Auth-Middleware, strukturiertes Logging und die Metriken.
//
// Die Reihenfolge der Middleware in SetupRouter ist tragend. /healthz, die
// Docs-Endpunkte und /metrics werden VOR der Auth registriert und sind damit
// frei; die Metrics-Middleware haengt ebenfalls davor, damit ein 401 gezaehlt
// wird und nicht unsichtbar bleibt.
//
// Der Verzweigungspunkt des gesamten Pakets ist resolveDefinition: liefert es
// eine ServiceDefinition, laeuft der Request durch internal/definition;
// liefert es nil - auch im Fehlerfall -, faellt er stumm auf den zweiten,
// vollstaendigen Broker in internal/broker/broker.go zurueck. Dieser Doppelpfad
// ist die zentrale Altlast des Repos und in docs/de/architecture.md sowie
// docs/de/adr/0003-replace-http-layer.md beschrieben.
package handlers
