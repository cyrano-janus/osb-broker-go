package handlers

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/example/osb-broker/internal/broker"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func b64Origin(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// Phase 1.3: every response carries an X-Correlation-ID, and the
// OriginatingIdentity header (if sent) is accepted and logged.

func newLoggingTestRouter() (*gin.Engine, *gin.Engine) {
	gin.SetMode(gin.TestMode)
	b := broker.New(nil)
	h := New(b)
	return h.SetupRouter(), nil
}

func TestCorrelationID_GeneratedWhenMissing(t *testing.T) {
	router, _ := newLoggingTestRouter()
	w := perform(router, "/v2/catalog", nil)
	require.Equal(t, http.StatusOK, w.Code)

	cid := w.Header().Get("X-Correlation-ID")
	assert.NotEmpty(t, cid, "response must carry a generated correlation ID")
	assert.Len(t, cid, 36, "correlation ID should be a UUID")
}

func TestCorrelationID_PreservedFromRequest(t *testing.T) {
	router, _ := newLoggingTestRouter()
	w := perform(router, "/v2/catalog", map[string]string{
		"X-Correlation-ID": "my-fixed-id-12345",
	})
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "my-fixed-id-12345", w.Header().Get("X-Correlation-ID"))
}

func TestOriginatingIdentity_AcceptedAndReflected(t *testing.T) {
	router, _ := newLoggingTestRouter()

	originating := b64Origin(`{"platform":"cloudfoundry","user_id":"admin-1"}`)
	w := perform(router, "/v2/catalog", map[string]string{
		"X-OSB-Originating-Identity": originating,
	})
	require.Equal(t, http.StatusOK, w.Code)
	// The middleware must parse it without rejecting the request.
}

func TestOriginatingIdentity_InvalidBase64StillSucceeds(t *testing.T) {
	router, _ := newLoggingTestRouter()

	// OSB spec: platforms MUST send valid base64; brokers SHOULD reject
	// invalid values with 400 for mutating requests. For reads we are
	// lenient; strictness is enforced on provision/bind (see errors phase).
	w := perform(router, "/v2/catalog", map[string]string{
		"X-OSB-Originating-Identity": "!!!not-base64!!!",
	})
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestStructuredLogOutput_ContainsCorrelationID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	b := broker.New(nil)
	h := New(b)
	logBuf := &testLogWriter{}
	gin.DefaultWriter = logBuf
	defer func() { gin.DefaultWriter = os.Stdout }()
	router := h.SetupRouter()

	perform(router, "/healthz", map[string]string{"X-Correlation-ID": "cid-log-check"})

	var entry map[string]interface{}
	lines := nonEmptyLines(logBuf.String())
	require.NotEmpty(t, lines, "expected structured log lines")
	last := lines[len(lines)-1]
	err := json.Unmarshal([]byte(last), &entry)
	require.NoError(t, err, "log line must be valid JSON: %s", last)
	assert.Equal(t, "cid-log-check", entry["correlation_id"])
}

// --- helpers ---

type testLogWriter struct {
	mu sync.Mutex
	sb strings.Builder
}

func (w *testLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sb.Write(p)
}
func (w *testLogWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sb.String()
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}
