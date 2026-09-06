package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Der Broker aendert eine Instanz nur auf Anfrage.
//
// `RECONCILE_INTERVAL` war ein Zeitgeber, der bestehende Instanzen ohne einen
// Request nachzog - Arbeit eines Controllers, nicht eines Brokers, und dem
// Besitzer der Instanz unsichtbar. Ersetzt hat ihn `maintenanceInfo`: der
// Katalog nennt den Stand, die Plattform zeigt die Abweichung, der Besitzer
// loest aus.
//
// Diese Pruefung haelt den Weg zu. Wer den Zeitgeber wiederbelebt, muss sie
// zuerst loeschen - und trifft die Entscheidung damit bewusst.
func TestKeinZeitgeberSchreibtOhneRequest(t *testing.T) {
	wurzel, err := filepath.Abs("../..")
	require.NoError(t, err)

	var treffer []string
	require.NoError(t, filepath.Walk(wurzel, func(pfad string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			if info != nil && info.IsDir() && (info.Name() == ".git" || info.Name() == "vendor") {
				return filepath.SkipDir
			}
			return err
		}
		if !strings.HasSuffix(pfad, ".go") || strings.HasSuffix(pfad, "no_timer_test.go") {
			return nil
		}
		inhalt, err := os.ReadFile(pfad)
		if err != nil {
			return err
		}
		if strings.Contains(string(inhalt), "RECONCILE_INTERVAL") {
			rel, _ := filepath.Rel(wurzel, pfad)
			treffer = append(treffer, rel)
		}
		return nil
	}))

	assert.Empty(t, treffer,
		"RECONCILE_INTERVAL ist abgeschafft - der Broker aendert eine Instanz nur auf Anfrage. "+
			"Der Weg dafuer ist maintenanceInfo je Plan. Fundstellen: %v", treffer)
}
