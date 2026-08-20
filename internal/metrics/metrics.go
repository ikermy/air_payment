package metrics

import (
	"errors"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

var (
	registerOnce sync.Once

	HTTPSRequests       = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "air", Subsystem: "pay", Name: "http_requests_total", Help: "Total number of HTTP requests handled by the service."}, []string{"method", "route", "status"})
	HTTPRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Namespace: "air", Subsystem: "pay", Name: "http_request_duration_seconds", Help: "Duration of HTTP requests handled by the service.", Buckets: prometheus.DefBuckets}, []string{"method", "route"})
	PaymentOperations   = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "air", Subsystem: "pay", Name: "payment_operations_total", Help: "Payment operation results."}, []string{"operation", "status"})
	SSEConnections      = prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "air", Subsystem: "pay", Name: "sse_connections", Help: "Number of active SSE connections."})
	PaymentStatus       = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "air", Subsystem: "pay", Name: "payment_status_total", Help: "Payments grouped by final or current status."}, []string{"status", "currency"})
	PaymentAmount       = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "air", Subsystem: "pay", Name: "payment_amount_total", Help: "Total processed payment amount."}, []string{"currency"})
	PaymentDuration     = prometheus.NewHistogramVec(prometheus.HistogramOpts{Namespace: "air", Subsystem: "pay", Name: "payment_processing_duration_seconds", Help: "Duration of payment operations."}, []string{"operation"})
	BybitRequests       = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "air", Subsystem: "pay", Name: "bybit_api_requests_total", Help: "Bybit API request results."}, []string{"operation", "status"})
	BybitDuration       = prometheus.NewHistogramVec(prometheus.HistogramOpts{Namespace: "air", Subsystem: "pay", Name: "bybit_api_request_duration_seconds", Help: "Duration of Bybit API operations."}, []string{"operation"})
	MonitorErrors       = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "air", Subsystem: "pay", Name: "payment_monitor_errors_total", Help: "Payment monitor errors."}, []string{"operation"})
	SSEMessages         = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "air", Subsystem: "pay", Name: "sse_messages_total", Help: "SSE message delivery results."}, []string{"event", "status"})
	PaymentReuse        = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "air", Subsystem: "pay", Name: "payment_reuse_total", Help: "Payment reuse results."}, []string{"result"})
	ActivePayments      = prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: "air", Subsystem: "pay", Name: "active_payments", Help: "Number of active payments."}, []string{"status"})
	StatusChanges       = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "air", Subsystem: "pay", Name: "payment_status_changes_total", Help: "Payment status transitions."}, []string{"from", "to", "currency"})
	DatabaseOperations  = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "air", Subsystem: "pay", Name: "database_operations_total", Help: "Database operation results."}, []string{"operation", "status"})
	DatabaseDuration    = prometheus.NewHistogramVec(prometheus.HistogramOpts{Namespace: "air", Subsystem: "pay", Name: "database_operation_duration_seconds", Help: "Database operation duration."}, []string{"operation"})
	BybitRateLimits     = prometheus.NewCounter(prometheus.CounterOpts{Namespace: "air", Subsystem: "pay", Name: "bybit_rate_limit_total", Help: "Bybit rate limit responses."})
	SSEDuration         = prometheus.NewHistogramVec(prometheus.HistogramOpts{Namespace: "air", Subsystem: "pay", Name: "sse_connection_duration_seconds", Help: "SSE connection duration."}, []string{"status"})
	SSEDropped          = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "air", Subsystem: "pay", Name: "sse_messages_dropped_total", Help: "SSE messages dropped by reason."}, []string{"reason"})
	MonitorCycles       = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "air", Subsystem: "pay", Name: "payment_monitor_cycles_total", Help: "Payment monitor cycle results."}, []string{"status"})
	MonitorDuration     = prometheus.NewHistogram(prometheus.HistogramOpts{Namespace: "air", Subsystem: "pay", Name: "payment_monitor_cycle_duration_seconds", Help: "Payment monitor cycle duration."})
)

func Register() {
	registerOnce.Do(func() {
		prometheus.DefaultRegisterer = prometheus.WrapRegistererWithPrefix("", prometheus.DefaultRegisterer)
		registerCollector(collectors.NewGoCollector())
		registerCollector(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
		registerCollector(HTTPSRequests)
		registerCollector(HTTPRequestDuration)
		registerCollector(PaymentOperations)
		registerCollector(SSEConnections)
		registerCollector(PaymentStatus)
		registerCollector(PaymentAmount)
		registerCollector(PaymentDuration)
		registerCollector(BybitRequests)
		registerCollector(BybitDuration)
		registerCollector(MonitorErrors)
		registerCollector(SSEMessages)
		registerCollector(PaymentReuse)
		registerCollector(ActivePayments)
		registerCollector(StatusChanges)
		registerCollector(DatabaseOperations)
		registerCollector(DatabaseDuration)
		registerCollector(BybitRateLimits)
		registerCollector(SSEDuration)
		registerCollector(SSEDropped)
		registerCollector(MonitorCycles)
		registerCollector(MonitorDuration)
	})
}

func registerCollector(collector prometheus.Collector) {
	if err := prometheus.Register(collector); err != nil {
		var alreadyRegisteredError prometheus.AlreadyRegisteredError
		if errors.As(err, &alreadyRegisteredError) {
			return
		}
		panic(err)
	}
}

func NormalizeRoute(path string) string {
	if path == "" {
		return "unknown"
	}
	if path == "/metrics" {
		return path
	}
	trimmed := strings.TrimSuffix(path, "/")
	if trimmed == "" {
		return "/"
	}
	return trimmed
}

func ObservePaymentOperation(operation, status string) {
	PaymentOperations.WithLabelValues(normalizeLabel(operation, "unknown"), normalizeLabel(status, "unknown")).Inc()
}

func ObservePaymentStatus(status, currency string) {
	PaymentStatus.WithLabelValues(normalizeLabel(status, "unknown"), normalizeLabel(currency, "unknown")).Inc()
}

func ObservePaymentAmount(currency string, amount float64) {
	if amount > 0 {
		PaymentAmount.WithLabelValues(normalizeLabel(currency, "unknown")).Add(amount)
	}
}

func ObservePaymentDuration(operation string, seconds float64) {
	PaymentDuration.WithLabelValues(normalizeLabel(operation, "unknown")).Observe(seconds)
}

func ObserveBybit(operation, status string) {
	BybitRequests.WithLabelValues(normalizeLabel(operation, "unknown"), normalizeLabel(status, "unknown")).Inc()
}

func ObserveMonitorError(operation string) {
	MonitorErrors.WithLabelValues(normalizeLabel(operation, "unknown")).Inc()
}
func ObserveSSEMessage(event, status string) {
	SSEMessages.WithLabelValues(normalizeLabel(event, "unknown"), normalizeLabel(status, "unknown")).Inc()
}
func ObservePaymentReuse(result string) {
	PaymentReuse.WithLabelValues(normalizeLabel(result, "unknown")).Inc()
}

func SetActivePayments(status string, count float64) {
	ActivePayments.WithLabelValues(normalizeLabel(status, "unknown")).Set(count)
}
func ObserveStatusChange(from, to, currency string) {
	StatusChanges.WithLabelValues(normalizeLabel(from, "unknown"), normalizeLabel(to, "unknown"), normalizeLabel(currency, "unknown")).Inc()
}
func ObserveDatabase(operation, status string, seconds float64) {
	DatabaseOperations.WithLabelValues(normalizeLabel(operation, "unknown"), normalizeLabel(status, "unknown")).Inc()
	DatabaseDuration.WithLabelValues(normalizeLabel(operation, "unknown")).Observe(seconds)
}
func ObserveSSEDrop(reason string) {
	SSEDropped.WithLabelValues(normalizeLabel(reason, "unknown")).Inc()
}
func ObserveMonitorCycle(status string, seconds float64) {
	MonitorCycles.WithLabelValues(normalizeLabel(status, "unknown")).Inc()
	MonitorDuration.Observe(seconds)
}

func normalizeLabel(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
