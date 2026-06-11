package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// HTTP layer
	RequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "killuadb_api_requests_total",
		Help: "Total API requests by method, path, and status code.",
	}, []string{"method", "path", "status"})

	RequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "killuadb_api_request_duration_seconds",
		Help:    "API request latency histogram.",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
	}, []string{"method", "path"})



	// ── DB pool / client count ──
	PgPoolCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "backend_pg_pool_count",
			Help: "Number of cached Postgres connection pools (= active Postgres instances)",
		},
	)
	MongoClientCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "backend_mongo_client_count",
			Help: "Number of cached Mongo clients (= active Mongo instances)",
		},
	)
	PgPoolAcquiredConns = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "backend_pg_pool_acquired_connections",
			Help: "Currently acquired connections per pool",
		},
		[]string{"pool_id"},
	)
	PgPoolIdleConns = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "backend_pg_pool_idle_connections",
			Help: "Currently idle connections per pool",
		},
		[]string{"pool_id"},
	)

	// ── Query latency ──
	PgQueryDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "backend_pg_query_duration_seconds",
			Help:    "Postgres query latency",
			Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		},
		[]string{"operation"},
	)
	MongoQueryDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "backend_mongo_query_duration_seconds",
			Help:    "MongoDB query latency",
			Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		},
		[]string{"operation"},
	)

	// ── Ping latency ──
	PgPingLatency = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "backend_pg_ping_latency_seconds",
			Help:    "Postgres connection ping latency",
			Buckets: prometheus.DefBuckets,
		},
	)
	MongoPingLatency = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "backend_mongo_ping_latency_seconds",
			Help:    "MongoDB connection ping latency",
			Buckets: prometheus.DefBuckets,
		},
	)

	// ── Errors ──
	DbErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "backend_db_errors_total",
			Help: "Total database errors",
		},
		[]string{"type", "operation"},
	)
	PgPoolErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "backend_pg_pool_errors_total",
			Help: "Postgres pool errors",
		},
		[]string{"error"},
	)

	// ── Provisioning ──
	InstancesCreatedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "backend_instances_created_total",
			Help: "Total DB instances created",
		},
		[]string{"type", "tier"},
	)
	InstancesDeletedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "backend_instances_deleted_total",
			Help: "Total DB instances deleted",
		},
		[]string{"type"},
	)
	InstancesCurrent = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "backend_instances_current",
			Help: "Current number of provisioned DB instances",
		},
		[]string{"type"},
	)
	ProvisioningDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "killuadb_provisioning_duration_seconds",
			Help:    "Time taken to provision or deprovision a database instance.",
			Buckets: []float64{1, 5, 10, 30, 60, 120, 300},
		},
		[]string{"type", "operation", "status"},
	)
	ProvisioningErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "backend_provisioning_errors_total",
			Help: "Total provisioning failures",
		},
		[]string{"type"},
	)

	// ── K8s capacity (background refresh) ──
	K8sInstanceCount = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "backend_k8s_instance_count",
			Help: "Number of DB instance CRDs in Kubernetes",
		},
		[]string{"type", "namespace"},
	)
)
