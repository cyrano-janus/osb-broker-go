package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadWith(t *testing.T, env map[string]string) *Config {
	t.Helper()
	cfg, err := LoadFrom(func(k string) string { return env[k] })
	require.NoError(t, err)
	return cfg
}

// Der Abgleich schreibt in fremde Namespaces. Er muss deshalb ausdrücklich
// eingeschaltet werden — ein Betreiber, der eine ältere Fassung ersetzt, soll
// nicht überrascht werden, dass plötzlich etwas ohne Request schreibt.

func TestAbgleichKonfiguration_IstStandardmaessigAus(t *testing.T) {
	cfg := loadWith(t, nil)
	assert.Zero(t, cfg.Reconcile.Interval, "ohne Angabe darf nichts von selbst schreiben")
}

func TestAbgleichKonfiguration_IntervallWirdUebernommen(t *testing.T) {
	cfg := loadWith(t, map[string]string{"RECONCILE_INTERVAL": "15m"})
	assert.Equal(t, 15*time.Minute, cfg.Reconcile.Interval)
}

// Ein Intervall von Sekunden setzt den API-Server unter Dauerlast, ohne dass
// jemand es merkt: der Abgleich liest je Instanz mindestens ein Objekt.
func TestAbgleichKonfiguration_ZuKurzesIntervallWirdGemeldet(t *testing.T) {
	cfg := loadWith(t, map[string]string{"RECONCILE_INTERVAL": "5s"})

	assert.Equal(t, 5*time.Second, cfg.Reconcile.Interval, "die Angabe gilt - gewarnt wird, nicht bevormundet")
	assert.NotEmpty(t, cfg.Warnings)
	joined := ""
	for _, w := range cfg.Warnings {
		joined += w
	}
	assert.Contains(t, joined, "RECONCILE_INTERVAL")
}
