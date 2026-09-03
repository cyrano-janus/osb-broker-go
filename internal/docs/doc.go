// Package docs enthaelt keinen Laufzeitcode. Es traegt den Waechtertest ueber
// den Dokumentationsbaum unter docs/.
//
// Warum ein Go-Test und kein Shell-Skript: dieses Repo koppelt Dokumentation
// und Code bereits an vier Stellen ueber Tests (schema_sync_test.go,
// docs_sync_test.go, crd_schema_test.go, catalog_test.go). `go test ./...` ist
// das einzige Gate, das ohnehin jeder ausfuehrt und das die CI schon faehrt.
// Ein zusaetzliches Skript waere ein zweiter Mechanismus fuer dieselbe Aufgabe.
//
// Die Datei doc.go existiert, damit das Verzeichnis eine nicht-Test-Quelldatei
// hat: `go build ./...` bricht sonst mit "no non-test Go files" ab.
package docs
