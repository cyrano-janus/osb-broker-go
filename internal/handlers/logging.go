package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"
)

// correlationIDKey is the gin context key for the request correlation ID.
const correlationIDKey = "correlation_id"

// originatingIdentityKey holds the decoded X-OSB-Originating-Identity payload.
const originatingIdentityKey = "originating_identity"

func newUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fall back to timestamp-based ID; randomness failure should not
		// take down request handling.
		return time.Now().UTC().Format("20060102150405.000000000")
	}
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// structuredLogger logs one JSON line per request including the correlation
// ID, the originating identity (when provided by the platform), method,
// path, status and duration.
type structuredLogEntry struct {
	Timestamp           string `json:"timestamp"`
	Level               string `json:"level"`
	Message             string `json:"message"`
	CorrelationID       string `json:"correlation_id"`
	Method              string `json:"method"`
	Path                string `json:"path"`
	Status              int    `json:"status"`
	DurationMS          int64  `json:"duration_ms"`
	ClientIP            string `json:"client_ip,omitempty"`
	OriginatingIdentity string `json:"originating_identity,omitempty"`
}

func structuredLoggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		// Correlation ID: honour inbound header, else generate a UUID.
		cid := c.GetHeader("X-Correlation-ID")
		if cid == "" {
			cid = newUUID()
		}
		c.Set(correlationIDKey, cid)
		c.Header("X-Correlation-ID", cid)

		// Originating Identity: base64 JSON per OSB spec; kept raw for the
		// log line and stored decoded-ish in the context for handlers.
		originating := c.GetHeader("X-OSB-Originating-Identity")

		c.Next()

		entry := structuredLogEntry{
			Timestamp:           start.UTC().Format(time.RFC3339Nano),
			Level:               "info",
			Message:             "request",
			CorrelationID:       cid,
			Method:              c.Request.Method,
			Path:                c.Request.URL.Path,
			Status:              c.Writer.Status(),
			DurationMS:          time.Since(start).Milliseconds(),
			ClientIP:            c.ClientIP(),
			OriginatingIdentity: originating,
		}
		line, err := json.Marshal(entry)
		if err != nil {
			return
		}
		gin.DefaultWriter.Write(append(line, '\n'))
	}
}
