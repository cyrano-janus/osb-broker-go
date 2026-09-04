// Package handlers ist die HTTP-Schicht: gin-Router, die OSB-Endpunkte,
// Auth-Middleware, strukturiertes Logging und die Metriken.
//
// Die Reihenfolge der Middleware in SetupRouter ist tragend. /healthz, die
// Docs-Endpunkte und /metrics werden VOR der Auth registriert und sind damit
// frei; die Metrics-Middleware haengt ebenfalls davor, damit ein 401 gezaehlt
// wird und nicht unsichtbar bleibt.
//
// Jeder Request loest ueber definitionFor seine ServiceDefinition auf. Kennt
// die Engine den Service nicht, ist das ErrServiceUnknown und damit 400 - es
// gibt keine Rueckfallebene. Frueher fiel ein Request an dieser Stelle stumm
// auf einen zweiten, vollstaendigen Broker mit eigenem Katalog zurueck; was
// daran hing, steht in docs/de/adr/0003-replace-http-layer.md.
//
// Fehler werden ueber ihren Wert auf Statuscodes abgebildet, nicht ueber ihren
// Text - siehe errors.go und internal/definition/errors.go.
package handlers
