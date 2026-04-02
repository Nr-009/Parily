package middleware

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"parily.dev/app/internal/metrics"
)

// PrometheusMiddleware records HTTP request count and latency for every
// request that passes through the Gin router. Register it once on the router
// and it covers all endpoints automatically — no per-handler changes needed.
//
// Labels:
//   method      — GET, POST, PATCH, DELETE
//   path        — the route template e.g. /api/rooms/:roomID/files/:fileID
//                 NOT the actual URL — this prevents high cardinality from
//                 UUIDs flooding the metric with thousands of unique label sets
//   status_code — 200, 201, 400, 403, 500 etc.
func PrometheusMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		// let the request run
		c.Next()

		duration := time.Since(start).Seconds()
		status := strconv.Itoa(c.Writer.Status())

		// FullPath() returns the route template e.g. /api/rooms/:roomID
		// If the route was not matched it returns "" — we label it "unknown"
		// to avoid empty label values which Prometheus handles poorly.
		path := c.FullPath()
		if path == "" {
			path = "unknown"
		}

		metrics.HttpRequestsTotal.WithLabelValues(
			c.Request.Method,
			path,
			status,
		).Inc()

		metrics.HttpRequestDuration.WithLabelValues(
			c.Request.Method,
			path,
		).Observe(duration)
	}
}