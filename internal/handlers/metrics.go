// Package handlers — metrics.go: Prometheus metrics for the OSB broker.
//
// Exposed metrics:
//
//	osb_requests_total{method,path,status}      — request counter per OSB route
//	osb_request_duration_seconds{method,path}   — latency histogram
//	osb_provisions_total{service_id,plan_id}    — successful provisions
//	osb_bindings_total{service_id}              — successful binds
//	osb_deprovisions_total{service_id,reason}   — deprovisions (reason=ok|gone)
//	osb_unbinds_total{service_id}               — unbinds
//	osb_last_operation_state{state}             — last_operation polls per state
//	osb_active_instances                        — gauge of known instances
//	osb_active_bindings                         — gauge of known bindings
package handlers

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics bundles all Prometheus collectors exposed by the broker.
type Metrics struct {
	Requests     *prometheus.CounterVec
	Duration     *prometheus.HistogramVec
	Provisions   *prometheus.CounterVec
	Bindings     *prometheus.CounterVec
	Deprovisions *prometheus.CounterVec
	Unbinds      *prometheus.CounterVec
	LastOp       *prometheus.CounterVec
	ActiveInst   prometheus.Gauge
	ActiveBind   prometheus.Gauge

	registry *prometheus.Registry
}

// NewMetrics creates the collector set and registers it on a fresh registry.
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		registry: reg,
		Requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "osb_requests_total",
			Help: "Total OSB API requests by method, route pattern and status.",
		}, []string{"method", "path", "status"}),
		Duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "osb_request_duration_seconds",
			Help:    "OSB API request latency in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "path"}),
		Provisions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "osb_provisions_total",
			Help: "Successful provision operations by service and plan.",
		}, []string{"service_id", "plan_id"}),
		Bindings: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "osb_bindings_total",
			Help: "Successful bind operations by service.",
		}, []string{"service_id"}),
		Deprovisions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "osb_deprovisions_total",
			Help: "Deprovision operations by service and outcome reason (ok/gone/error).",
		}, []string{"service_id", "reason"}),
		Unbinds: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "osb_unbinds_total",
			Help: "Successful unbind operations by service.",
		}, []string{"service_id"}),
		LastOp: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "osb_last_operation_state",
			Help: "last_operation polls by reported state (succeeded/in progress/failed).",
		}, []string{"state"}),
		ActiveInst: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "osb_active_instances",
			Help: "Currently known service instances.",
		}),
		ActiveBind: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "osb_active_bindings",
			Help: "Currently known service bindings.",
		}),
	}
	reg.MustRegister(
		m.Requests, m.Duration, m.Provisions, m.Bindings,
		m.Deprovisions, m.Unbinds, m.LastOp, m.ActiveInst, m.ActiveBind,
	)
	return m
}

// Registry returns the underlying Prometheus registry (for the /metrics handler).
func (m *Metrics) Registry() *prometheus.Registry { return m.registry }

// Handler serves Prometheus text exposition on GET /metrics.
func (m *Metrics) Handler() gin.HandlerFunc {
	h := promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
	return func(c *gin.Context) {
		h.ServeHTTP(c.Writer, c.Request)
	}
}

// routePattern returns a low-cardinality route template (e.g.
// "/v2/service_instances/:instance_id") instead of concrete ids.
func routePattern(c *gin.Context) string {
	if r := c.FullPath(); r != "" {
		return r
	}
	return "unmatched"
}

// MetricsMiddleware records request count and latency per route template.
// It must be registered before the auth middleware so that ALL requests
// (including 401s) are counted.
func (m *Metrics) MetricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		pattern := routePattern(c)
		status := strconv.Itoa(c.Writer.Status())
		m.Requests.WithLabelValues(c.Request.Method, pattern, status).Inc()
		m.Duration.WithLabelValues(c.Request.Method, pattern).Observe(time.Since(start).Seconds())
	}
}

// metricsEnabled reports whether metrics collection is active.
func (h *Handlers) metricsEnabled() bool { return h.metrics != nil }

// observeProvision counts a successful provision.
func (h *Handlers) observeProvision(serviceID, planID string) {
	if h.metrics != nil {
		h.metrics.Provisions.WithLabelValues(serviceID, planID).Inc()
	}
}

// observeBind counts a successful bind.
func (h *Handlers) observeBind(serviceID string) {
	if h.metrics != nil {
		h.metrics.Bindings.WithLabelValues(serviceID).Inc()
	}
}

// observeUnbind counts a successful unbind.
func (h *Handlers) observeUnbind(serviceID string) {
	if h.metrics != nil {
		h.metrics.Unbinds.WithLabelValues(serviceID).Inc()
	}
}

// observeDeprovision counts a deprovision with its outcome reason.
func (h *Handlers) observeDeprovision(serviceID, reason string) {
	if h.metrics != nil {
		h.metrics.Deprovisions.WithLabelValues(serviceID, reason).Inc()
	}
}

// observeLastOperation counts a last_operation poll by reported state.
func (h *Handlers) observeLastOperation(state string) {
	if h.metrics != nil {
		h.metrics.LastOp.WithLabelValues(state).Inc()
	}
}
