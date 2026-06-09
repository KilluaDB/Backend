package middleware

import (
	"strconv"
	"time"

	"backend/internal/metrics"

	"github.com/gin-gonic/gin"
)

func MetricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		duration := time.Since(start).Seconds()
		path := c.FullPath()
		if path == "" {
			path = "unmatched"
		}
		status := strconv.Itoa(c.Writer.Status())
		metrics.RequestsTotal.WithLabelValues(c.Request.Method, path, status).Inc()
		metrics.RequestDuration.WithLabelValues(c.Request.Method, path).Observe(duration)
	}
}
