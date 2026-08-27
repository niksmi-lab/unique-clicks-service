package metrics

import (
	"net/http"
	"runtime"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const namespace = "unique_clicks"

type Metrics struct {
	registry *prometheus.Registry

	httpRequests       *prometheus.CounterVec
	httpDuration       *prometheus.HistogramVec
	httpInFlight       prometheus.Gauge
	clicks             *prometheus.CounterVec
	metricsQueries     *prometheus.CounterVec
	storageOperations  *prometheus.CounterVec
	storageDuration    *prometheus.HistogramVec
	cleanupRuns        *prometheus.CounterVec
	cleanupDeletedRows prometheus.Counter
	cleanupDuration    prometheus.Histogram
	cleanupLastSuccess prometheus.Gauge
}

func New(pool *pgxpool.Pool, version string) *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total number of HTTP requests.",
		}, []string{"method", "route", "status"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request duration in seconds.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"method", "route", "status"}),
		httpInFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "requests_in_flight",
			Help:      "Current number of HTTP requests being processed.",
		}),
		clicks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "clicks_total",
			Help:      "Click tracking attempts grouped by result.",
		}, []string{"result"}),
		metricsQueries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "metrics_queries_total",
			Help:      "Business metrics queries grouped by result.",
		}, []string{"result"}),
		storageOperations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "storage",
			Name:      "operations_total",
			Help:      "PostgreSQL operations grouped by operation and result.",
		}, []string{"operation", "result"}),
		storageDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "storage",
			Name:      "operation_duration_seconds",
			Help:      "PostgreSQL operation duration in seconds.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"operation"}),
		cleanupRuns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "cleanup",
			Name:      "runs_total",
			Help:      "Retention cleanup runs grouped by result.",
		}, []string{"result"}),
		cleanupDeletedRows: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "cleanup",
			Name:      "deleted_rows_total",
			Help:      "Total number of expired click rows deleted.",
		}),
		cleanupDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "cleanup",
			Name:      "duration_seconds",
			Help:      "Retention cleanup duration in seconds.",
			Buckets:   prometheus.DefBuckets,
		}),
		cleanupLastSuccess: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "cleanup",
			Name:      "last_success_timestamp_seconds",
			Help:      "Unix timestamp of the last successful retention cleanup.",
		}),
	}

	buildInfo := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "build_info",
		Help:      "Build information for the running service.",
	}, []string{"version", "go_version"})
	buildInfo.WithLabelValues(version, runtime.Version()).Set(1)

	m.registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		buildInfo,
		m.httpRequests,
		m.httpDuration,
		m.httpInFlight,
		m.clicks,
		m.metricsQueries,
		m.storageOperations,
		m.storageDuration,
		m.cleanupRuns,
		m.cleanupDeletedRows,
		m.cleanupDuration,
		m.cleanupLastSuccess,
	)

	registerPoolCollectors(m.registry, pool)
	return m
}

func (m *Metrics) Handler() http.Handler {
	handler := promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
	return promhttp.InstrumentMetricHandler(m.registry, handler)
}

func (m *Metrics) ObserveHTTPRequest(method, route string, status int, duration time.Duration) {
	statusLabel := strconv.Itoa(status)
	m.httpRequests.WithLabelValues(method, route, statusLabel).Inc()
	m.httpDuration.WithLabelValues(method, route, statusLabel).Observe(duration.Seconds())
}

func (m *Metrics) IncHTTPInFlight() {
	m.httpInFlight.Inc()
}

func (m *Metrics) DecHTTPInFlight() {
	m.httpInFlight.Dec()
}

func (m *Metrics) ObserveClick(result string) {
	m.clicks.WithLabelValues(result).Inc()
}

func (m *Metrics) ObserveMetricsQuery(result string) {
	m.metricsQueries.WithLabelValues(result).Inc()
}

func (m *Metrics) ObserveStorageOperation(operation, result string, duration time.Duration) {
	m.storageOperations.WithLabelValues(operation, result).Inc()
	m.storageDuration.WithLabelValues(operation).Observe(duration.Seconds())
}

func (m *Metrics) ObserveCleanup(result string, deletedRows int64, duration time.Duration) {
	m.cleanupRuns.WithLabelValues(result).Inc()
	m.cleanupDuration.Observe(duration.Seconds())
	if result == "success" {
		m.cleanupLastSuccess.SetToCurrentTime()
	}
	if deletedRows > 0 {
		m.cleanupDeletedRows.Add(float64(deletedRows))
	}
}

func registerPoolCollectors(registry *prometheus.Registry, pool *pgxpool.Pool) {
	gauges := []prometheus.Collector{
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: namespace, Subsystem: "db_pool", Name: "acquired_connections",
			Help: "Number of currently acquired database connections.",
		}, func() float64 { return float64(pool.Stat().AcquiredConns()) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: namespace, Subsystem: "db_pool", Name: "idle_connections",
			Help: "Number of currently idle database connections.",
		}, func() float64 { return float64(pool.Stat().IdleConns()) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: namespace, Subsystem: "db_pool", Name: "constructing_connections",
			Help: "Number of database connections currently being established.",
		}, func() float64 { return float64(pool.Stat().ConstructingConns()) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: namespace, Subsystem: "db_pool", Name: "total_connections",
			Help: "Total number of database connections.",
		}, func() float64 { return float64(pool.Stat().TotalConns()) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: namespace, Subsystem: "db_pool", Name: "max_connections",
			Help: "Maximum number of database connections.",
		}, func() float64 { return float64(pool.Stat().MaxConns()) }),
		prometheus.NewCounterFunc(prometheus.CounterOpts{
			Namespace: namespace, Subsystem: "db_pool", Name: "acquires_total",
			Help: "Cumulative number of successful connection acquires.",
		}, func() float64 { return float64(pool.Stat().AcquireCount()) }),
		prometheus.NewCounterFunc(prometheus.CounterOpts{
			Namespace: namespace, Subsystem: "db_pool", Name: "canceled_acquires_total",
			Help: "Cumulative number of canceled connection acquires.",
		}, func() float64 { return float64(pool.Stat().CanceledAcquireCount()) }),
		prometheus.NewCounterFunc(prometheus.CounterOpts{
			Namespace: namespace, Subsystem: "db_pool", Name: "empty_acquires_total",
			Help: "Cumulative number of acquires that waited for a connection.",
		}, func() float64 { return float64(pool.Stat().EmptyAcquireCount()) }),
		prometheus.NewCounterFunc(prometheus.CounterOpts{
			Namespace: namespace, Subsystem: "db_pool", Name: "new_connections_total",
			Help: "Cumulative number of database connections established.",
		}, func() float64 { return float64(pool.Stat().NewConnsCount()) }),
		prometheus.NewCounterFunc(prometheus.CounterOpts{
			Namespace: namespace, Subsystem: "db_pool", Name: "acquire_duration_seconds_total",
			Help: "Cumulative time spent acquiring database connections.",
		}, func() float64 { return pool.Stat().AcquireDuration().Seconds() }),
		prometheus.NewCounterFunc(prometheus.CounterOpts{
			Namespace: namespace, Subsystem: "db_pool", Name: "empty_acquire_wait_seconds_total",
			Help: "Cumulative time spent waiting because the pool was empty.",
		}, func() float64 { return pool.Stat().EmptyAcquireWaitTime().Seconds() }),
		prometheus.NewCounterFunc(prometheus.CounterOpts{
			Namespace: namespace, Subsystem: "db_pool", Name: "max_idle_destroyed_total",
			Help: "Cumulative connections closed because they were idle.",
		}, func() float64 { return float64(pool.Stat().MaxIdleDestroyCount()) }),
		prometheus.NewCounterFunc(prometheus.CounterOpts{
			Namespace: namespace, Subsystem: "db_pool", Name: "max_lifetime_destroyed_total",
			Help: "Cumulative connections closed because they exceeded maximum lifetime.",
		}, func() float64 { return float64(pool.Stat().MaxLifetimeDestroyCount()) }),
	}
	registry.MustRegister(gauges...)
}
