package metrics

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// MetricsMiddleware records metrics for Fiber routes.
func MetricsMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if c.Path() == "/metrics" {
			return c.Next()
		}

		startedAt := time.Now()
		err := c.Next()
		route := NormalizeRoute(c.Route().Path)
		HTTPRequestDuration.WithLabelValues(c.Method(), route).Observe(time.Since(startedAt).Seconds())
		HTTPSRequests.WithLabelValues(c.Method(), route, strconv.Itoa(c.Response().StatusCode())).Inc()
		return err
	}
}

// Handler adapts Prometheus' net/http handler for Fiber.
func Handler() fiber.Handler {
	Register()
	return adaptor.HTTPHandler(promhttp.Handler())
}
