package metrics

import "github.com/prometheus/client_golang/prometheus"

// ── HTTP ─────────────────────────────────────────────────────────────────────

// HttpRequestsTotal counts every HTTP request, labeled by method, path and
// status code. Registered by the Prometheus middleware automatically.
var HttpRequestsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total number of HTTP requests.",
	},
	[]string{"method", "path", "status_code"},
)

// HttpRequestDuration tracks how long each HTTP request takes.
// Buckets cover fast API responses through slow Kafka/MongoDB operations.
var HttpRequestDuration = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency in seconds.",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
	},
	[]string{"method", "path"},
)

// ── WebSocket ────────────────────────────────────────────────────────────────

// ActiveWebsocketConnections tracks how many WebSocket connections are open
// right now across all hubs (Yjs, Room, Notify).
var ActiveWebsocketConnections = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: "active_websocket_connections",
		Help: "Number of currently open WebSocket connections.",
	},
)

// WebsocketReconnectionsTotal counts WebSocket reconnection events.
// High reconnection rates signal network instability or server issues.
var WebsocketReconnectionsTotal = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: "websocket_reconnections_total",
		Help: "Total number of WebSocket reconnection events.",
	},
)

// ── Rooms ────────────────────────────────────────────────────────────────────

// ActiveRoomsTotal tracks how many rooms have at least one active user.
var ActiveRoomsTotal = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: "active_rooms_total",
		Help: "Number of rooms with at least one active WebSocket connection.",
	},
)

// RoomJoinsTotal counts the total rate of room joins over time.
// Complements ActiveRoomsTotal which only shows current state.
var RoomJoinsTotal = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: "room_joins_total",
		Help: "Total number of room join events.",
	},
)

// ── Yjs / Saves ──────────────────────────────────────────────────────────────

// YjsSavesTotal counts every SaveState call that reaches MongoDB.
// Compare with YjsSavesSkippedTotal to see dedup effectiveness.
var YjsSavesTotal = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: "yjs_saves_total",
		Help: "Total number of Yjs state saves written to MongoDB.",
	},
)

// YjsSavesSkippedTotal counts saves skipped by the dedup check.
// High skip rate means the dedup optimization is working well.
var YjsSavesSkippedTotal = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: "yjs_saves_skipped_total",
		Help: "Total number of Yjs saves skipped because content was unchanged.",
	},
)

// ── Version History / Snapshots ───────────────────────────────────────────────

// SnapshotHitTotal counts version history requests served from MongoDB snapshot
// cache — no Kafka replay needed. High hit rate = snapshot cache is working.
var SnapshotHitTotal = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: "snapshot_hit_total",
		Help: "Total number of version history requests served from MongoDB snapshot cache.",
	},
)

// SnapshotMissTotal counts version history requests that required a full Kafka
// replay because no snapshot existed. Shows how often the slow path is hit.
var SnapshotMissTotal = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: "snapshot_miss_total",
		Help: "Total number of version history requests that required Kafka replay.",
	},
)

// ── Code Execution ───────────────────────────────────────────────────────────

// ExecutionsTotal counts every code execution attempt, labeled by language
// so you can see which languages are used most.
var ExecutionsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "executions_total",
		Help: "Total number of code executions, by language.",
	},
	[]string{"language"},
)

// ExecutionDuration tracks how long each code execution takes end to end
// including Docker container spinup time.
var ExecutionDuration = prometheus.NewHistogram(
	prometheus.HistogramOpts{
		Name:    "execution_duration_seconds",
		Help:    "Code execution duration in seconds including container spinup.",
		Buckets: []float64{0.5, 1, 2, 5, 10, 15, 20, 30},
	},
)

// ExecutionTimeoutTotal counts executions killed by the 30s hard timeout.
// Any non-zero value here means user code is hitting the limit.
var ExecutionTimeoutTotal = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: "execution_timeout_total",
		Help: "Total number of executions killed by the 30 second timeout.",
	},
)

// ExecutionOutputTruncatedTotal counts executions where output exceeded the
// 50kb cap. Shows how often users hit the truncation limit.
var ExecutionOutputTruncatedTotal = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: "execution_output_truncated_total",
		Help: "Total number of executions where output was truncated at 50kb.",
	},
)

// ── Kafka ─────────────────────────────────────────────────────────────────────

// KafkaPublishErrorsTotal counts failed Kafka publish attempts, labeled by
// topic so you can see which topic is having issues.
var KafkaPublishErrorsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "kafka_publish_errors_total",
		Help: "Total number of failed Kafka publish attempts, by topic.",
	},
	[]string{"topic"},
)

// ── Redis ─────────────────────────────────────────────────────────────────────

// RedisPublishErrorsTotal counts failed Redis publish calls.
// These are critical — a failed publish means clients miss an event.
var RedisPublishErrorsTotal = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: "redis_publish_errors_total",
		Help: "Total number of failed Redis publish calls.",
	},
)

// ── MongoDB ──────────────────────────────────────────────────────────────────

// MongodbErrorsTotal counts MongoDB operation errors, labeled by operation
// type so you can pinpoint which operations are failing.
var MongodbErrorsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "mongodb_errors_total",
		Help: "Total number of MongoDB operation errors, by operation type.",
	},
	[]string{"operation"},
)

// ── Init ─────────────────────────────────────────────────────────────────────

// Init registers all metrics with the default Prometheus registry.
// Call once at startup in main() before serving any traffic.
func Init() {
	prometheus.MustRegister(
		HttpRequestsTotal,
		HttpRequestDuration,
		ActiveWebsocketConnections,
		WebsocketReconnectionsTotal,
		ActiveRoomsTotal,
		RoomJoinsTotal,
		YjsSavesTotal,
		YjsSavesSkippedTotal,
		SnapshotHitTotal,
		SnapshotMissTotal,
		ExecutionsTotal,
		ExecutionDuration,
		ExecutionTimeoutTotal,
		ExecutionOutputTruncatedTotal,
		KafkaPublishErrorsTotal,
		RedisPublishErrorsTotal,
		MongodbErrorsTotal,
	)
}

// InitServer registers metrics relevant to the WS server binary.
// Call once in cmd/server/main.go
func InitServer() {
    prometheus.MustRegister(
        HttpRequestsTotal,
        HttpRequestDuration,
        ActiveWebsocketConnections,
        WebsocketReconnectionsTotal,
        ActiveRoomsTotal,
        RoomJoinsTotal,
        YjsSavesTotal,
        YjsSavesSkippedTotal,
        SnapshotHitTotal,
        SnapshotMissTotal,
        KafkaPublishErrorsTotal,
        RedisPublishErrorsTotal,
        MongodbErrorsTotal,
    )
}

// InitExecutor registers metrics relevant to the executor binary.
// Call once in cmd/executor/main.go
func InitExecutor() {
    prometheus.MustRegister(
        ExecutionsTotal,
        ExecutionDuration,
        ExecutionTimeoutTotal,
        ExecutionOutputTruncatedTotal,
        KafkaPublishErrorsTotal,
        RedisPublishErrorsTotal,
    )
}