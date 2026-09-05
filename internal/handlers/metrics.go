// metrics.go: Prometheus metrics for the OSB broker.
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
//	osb_state_read_errors_total                 — failed state-store reads
//
// Die beiden Bestands-Gauges werden beim Abholen gezaehlt, nicht mitgefuehrt.
// Ein Zaehler, den der Broker bei jedem Provision hochzaehlt, faellt beim
// Neustart auf 0 zurueck, waehrend die Instanzen weiterlaufen - und er
// verpasst jede Aenderung, die nicht durch diesen Prozess ging. Kann der
// Zustandsspeicher gerade nicht gelesen werden, fehlen die beiden Metriken:
// eine Luecke im Graphen ist sichtbar, eine stehengebliebene Zahl ist es
// nicht.
package handlers

import (
	"context"
	"strconv"
	"time"

	"github.com/example/osb-broker/internal/broker"

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
	// ReadErrors zaehlt Leseversuche auf den Zustandsspeicher, die scheitern.
	// Ohne diesen Zaehler waere eine Luecke in den Bestandsmetriken nicht von
	// "es gibt gerade nichts" zu unterscheiden.
	ReadErrors prometheus.Counter

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
		ReadErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "osb_state_read_errors_total",
			Help: "Failed reads of the state store while collecting metrics.",
		}),
	}
	reg.MustRegister(
		m.Requests, m.Duration, m.Provisions, m.Bindings,
		m.Deprovisions, m.Unbinds, m.LastOp, m.ReadErrors,
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

// stateCollector meldet den Bestand, indem er ihn beim Abholen zaehlt.
//
// Kein Gauge, den irgendwer setzt: prometheus.Collector wird bei jedem Scrape
// befragt, und genau dann wird der Zustandsspeicher gezaehlt. Scheitert das,
// wird die Metrik weggelassen und osb_state_read_errors_total erhoeht - eine
// Zahl zu melden, die man nicht gemessen hat, waere eine Erfindung.
type stateCollector struct {
	counter    broker.Counter
	readErrors prometheus.Counter
	instDesc   *prometheus.Desc
	bindDesc   *prometheus.Desc
	// timeout begrenzt den Lesevorgang. Ein Scrape darf nicht haengen, nur
	// weil der API-Server gerade langsam ist.
	timeout time.Duration
}

const stateReadTimeout = 5 * time.Second

func newStateCollector(c broker.Counter, readErrors prometheus.Counter) *stateCollector {
	return &stateCollector{
		counter:    c,
		readErrors: readErrors,
		instDesc:   prometheus.NewDesc("osb_active_instances", "Currently known service instances.", nil, nil),
		bindDesc:   prometheus.NewDesc("osb_active_bindings", "Currently known service bindings.", nil, nil),
		timeout:    stateReadTimeout,
	}
}

func (s *stateCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- s.instDesc
	ch <- s.bindDesc
}

func (s *stateCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	if n, err := s.counter.CountInstances(ctx); err != nil {
		s.readErrors.Inc()
	} else {
		ch <- prometheus.MustNewConstMetric(s.instDesc, prometheus.GaugeValue, float64(n))
	}
	if n, err := s.counter.CountBindings(ctx); err != nil {
		s.readErrors.Inc()
	} else {
		ch <- prometheus.MustNewConstMetric(s.bindDesc, prometheus.GaugeValue, float64(n))
	}
}

// WatchState laesst die Bestandsmetriken den Zustandsspeicher zaehlen. Ohne
// Aufruf - oder mit einem Speicher, der nicht zaehlen kann - gibt es die
// beiden Metriken nicht.
func (m *Metrics) WatchState(store interface{}) {
	c, ok := store.(broker.Counter)
	if !ok {
		return
	}
	m.registry.MustRegister(newStateCollector(c, m.ReadErrors))
}
