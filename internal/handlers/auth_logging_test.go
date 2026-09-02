package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/example/osb-broker/internal/broker"
	"github.com/example/osb-broker/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lastLogEntry runs one request through a router with auth enabled and
// returns the structured log line it produced, plus the raw log text.
func lastLogEntry(t *testing.T, headers map[string]string) (map[string]interface{}, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	h := New(broker.New(store.NewInMemoryStore(), nil))
	h.SetBasicAuthCredentials("broker-user", "broker-secret")

	logBuf := &testLogWriter{}
	gin.DefaultWriter = logBuf
	defer func() { gin.DefaultWriter = os.Stdout }()

	perform(h.SetupRouter(), "/v2/catalog", headers)

	lines := nonEmptyLines(logBuf.String())
	require.NotEmpty(t, lines, "expected a structured log line")
	var entry map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &entry))
	return entry, logBuf.String()
}

func TestAccessLog_RecordsAuthenticatedIdentity(t *testing.T) {
	entry, _ := lastLogEntry(t, map[string]string{
		"Authorization": basicAuthHeader("broker-user", "broker-secret"),
	})

	assert.Equal(t, float64(http.StatusOK), entry["status"])
	assert.Equal(t, "basic", entry["auth_method"])
	assert.Equal(t, "broker-user", entry["auth_subject"])
}

func TestAccessLog_NeverContainsCredentials(t *testing.T) {
	// The Authorization header must not reach the log in any form - neither
	// the base64 blob nor the decoded password.
	header := basicAuthHeader("broker-user", "broker-secret")
	_, raw := lastLogEntry(t, map[string]string{"Authorization": header})

	assert.NotContains(t, raw, "broker-secret")
	assert.NotContains(t, raw, header)
	assert.NotContains(t, raw, "Authorization")
}

func TestAccessLog_OmitsAuthFieldsOnRejectedRequest(t *testing.T) {
	entry, _ := lastLogEntry(t, nil)

	assert.Equal(t, float64(http.StatusUnauthorized), entry["status"])
	_, hasMethod := entry["auth_method"]
	assert.False(t, hasMethod, "a rejected request has no identity to record")
}
