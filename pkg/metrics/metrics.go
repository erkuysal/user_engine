// Package metrics provides Prometheus metrics for the presence engine.
package metrics

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Metrics holds all presence engine metrics.
type Metrics struct {
	mu sync.RWMutex

	// Connection metrics
	connectionsTotal    map[string]int64 // scope_id -> count
	connectionsByScope  map[string]int64
	connectionsActive   int64
	connectAttempts     int64
	connectFailures     int64
	disconnects         int64
	disconnectsNormal   int64
	disconnectsAbnormal int64

	// Event metrics
	eventsPublished  map[string]int64 // event_type -> count
	eventsConsumed   map[string]int64
	eventsDropped    int64
	eventPublishErrs int64

	// Redis metrics
	redisOpsTotal    map[string]int64   // operation -> count
	redisOpsDuration map[string]float64 // operation -> total_seconds
	redisErrors      int64

	// Heartbeat metrics
	heartbeatsReceived int64
	heartbeatsRejected int64

	// Sweeper metrics
	sweeperTicks           int64
	sweeperSessionsPruned  int64
	sweeperUsersTransition int64

	// Debouncer metrics
	debouncerJobsScheduled int64
	debouncerJobsExecuted  int64
	debouncerJobsCancelled int64

	// Startup time
	startTime time.Time
}

// New creates a new Metrics instance.
func New() *Metrics {
	return &Metrics{
		connectionsTotal:   make(map[string]int64),
		connectionsByScope: make(map[string]int64),
		eventsPublished:    make(map[string]int64),
		eventsConsumed:     make(map[string]int64),
		redisOpsTotal:      make(map[string]int64),
		redisOpsDuration:   make(map[string]float64),
		startTime:          time.Now(),
	}
}

// Global metrics instance
var global = New()

// Global returns the global metrics instance.
func Global() *Metrics {
	return global
}

// IncConnections increments connection count for a scope.
func (m *Metrics) IncConnections(scopeID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connectionsTotal[scopeID]++
	m.connectionsByScope[scopeID]++
	m.connectionsActive++
	m.connectAttempts++
}

// DecConnections decrements connection count for a scope.
func (m *Metrics) DecConnections(scopeID string, normal bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connectionsByScope[scopeID]--
	if m.connectionsByScope[scopeID] < 0 {
		m.connectionsByScope[scopeID] = 0
	}
	m.connectionsActive--
	if m.connectionsActive < 0 {
		m.connectionsActive = 0
	}
	m.disconnects++
	if normal {
		m.disconnectsNormal++
	} else {
		m.disconnectsAbnormal++
	}
}

// IncConnectFailure increments the connection failure counter.
func (m *Metrics) IncConnectFailure() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connectFailures++
}

// ConnectionCount returns current connections for a scope.
func (m *Metrics) ConnectionCount(scopeID string) int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connectionsByScope[scopeID]
}

// TotalConnections returns total active connections across all scopes.
func (m *Metrics) TotalConnections() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connectionsActive
}

// IncEventPublished increments published event counter.
func (m *Metrics) IncEventPublished(eventType string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventsPublished[eventType]++
}

// IncEventConsumed increments consumed event counter.
func (m *Metrics) IncEventConsumed(eventType string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventsConsumed[eventType]++
}

// IncEventDropped increments dropped event counter.
func (m *Metrics) IncEventDropped() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventsDropped++
}

// IncEventPublishError increments event publish error counter.
func (m *Metrics) IncEventPublishError() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventPublishErrs++
}

// RecordRedisOp records a Redis operation with its duration.
func (m *Metrics) RecordRedisOp(operation string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.redisOpsTotal[operation]++
	m.redisOpsDuration[operation] += duration.Seconds()
}

// IncRedisError increments Redis error counter.
func (m *Metrics) IncRedisError() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.redisErrors++
}

// IncHeartbeat increments heartbeat counter.
func (m *Metrics) IncHeartbeat(accepted bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if accepted {
		m.heartbeatsReceived++
	} else {
		m.heartbeatsRejected++
	}
}

// IncSweeperTick increments sweeper tick counter.
func (m *Metrics) IncSweeperTick() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweeperTicks++
}

// RecordSweeperResults records sweeper results.
func (m *Metrics) RecordSweeperResults(sessionsPruned, usersOffline int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweeperSessionsPruned += int64(sessionsPruned)
	m.sweeperUsersTransition += int64(usersOffline)
}

// IncDebouncerJobScheduled increments scheduled job counter.
func (m *Metrics) IncDebouncerJobScheduled() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.debouncerJobsScheduled++
}

// IncDebouncerJobExecuted increments executed job counter.
func (m *Metrics) IncDebouncerJobExecuted() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.debouncerJobsExecuted++
}

// IncDebouncerJobCancelled increments cancelled job counter.
func (m *Metrics) IncDebouncerJobCancelled(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.debouncerJobsCancelled += int64(count)
}

// Handler returns an HTTP handler for Prometheus metrics endpoint.
func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.RLock()
		defer m.mu.RUnlock()

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		// Help and type declarations
		writeMetric(w, "# HELP presence_connections_active Current number of active WebSocket connections")
		writeMetric(w, "# TYPE presence_connections_active gauge")
		writeMetric(w, "presence_connections_active "+strconv.FormatInt(m.connectionsActive, 10))

		writeMetric(w, "# HELP presence_connections_total Total WebSocket connections by scope")
		writeMetric(w, "# TYPE presence_connections_total counter")
		for scope, count := range m.connectionsTotal {
			writeMetric(w, `presence_connections_total{scope_id="`+scope+`"} `+strconv.FormatInt(count, 10))
		}

		writeMetric(w, "# HELP presence_connections_by_scope Current connections per scope")
		writeMetric(w, "# TYPE presence_connections_by_scope gauge")
		for scope, count := range m.connectionsByScope {
			writeMetric(w, `presence_connections_by_scope{scope_id="`+scope+`"} `+strconv.FormatInt(count, 10))
		}

		writeMetric(w, "# HELP presence_connect_attempts_total Total connection attempts")
		writeMetric(w, "# TYPE presence_connect_attempts_total counter")
		writeMetric(w, "presence_connect_attempts_total "+strconv.FormatInt(m.connectAttempts, 10))

		writeMetric(w, "# HELP presence_connect_failures_total Total connection failures")
		writeMetric(w, "# TYPE presence_connect_failures_total counter")
		writeMetric(w, "presence_connect_failures_total "+strconv.FormatInt(m.connectFailures, 10))

		writeMetric(w, "# HELP presence_disconnects_total Total disconnections")
		writeMetric(w, "# TYPE presence_disconnects_total counter")
		writeMetric(w, `presence_disconnects_total{type="normal"} `+strconv.FormatInt(m.disconnectsNormal, 10))
		writeMetric(w, `presence_disconnects_total{type="abnormal"} `+strconv.FormatInt(m.disconnectsAbnormal, 10))

		writeMetric(w, "# HELP presence_events_published_total Total events published by type")
		writeMetric(w, "# TYPE presence_events_published_total counter")
		for eventType, count := range m.eventsPublished {
			writeMetric(w, `presence_events_published_total{type="`+eventType+`"} `+strconv.FormatInt(count, 10))
		}

		writeMetric(w, "# HELP presence_events_consumed_total Total events consumed by type")
		writeMetric(w, "# TYPE presence_events_consumed_total counter")
		for eventType, count := range m.eventsConsumed {
			writeMetric(w, `presence_events_consumed_total{type="`+eventType+`"} `+strconv.FormatInt(count, 10))
		}

		writeMetric(w, "# HELP presence_events_dropped_total Total events dropped due to slow consumers")
		writeMetric(w, "# TYPE presence_events_dropped_total counter")
		writeMetric(w, "presence_events_dropped_total "+strconv.FormatInt(m.eventsDropped, 10))

		writeMetric(w, "# HELP presence_event_publish_errors_total Total event publish errors")
		writeMetric(w, "# TYPE presence_event_publish_errors_total counter")
		writeMetric(w, "presence_event_publish_errors_total "+strconv.FormatInt(m.eventPublishErrs, 10))

		writeMetric(w, "# HELP presence_redis_operations_total Total Redis operations by type")
		writeMetric(w, "# TYPE presence_redis_operations_total counter")
		for op, count := range m.redisOpsTotal {
			writeMetric(w, `presence_redis_operations_total{operation="`+op+`"} `+strconv.FormatInt(count, 10))
		}

		writeMetric(w, "# HELP presence_redis_operation_duration_seconds_total Total Redis operation duration by type")
		writeMetric(w, "# TYPE presence_redis_operation_duration_seconds_total counter")
		for op, dur := range m.redisOpsDuration {
			writeMetric(w, `presence_redis_operation_duration_seconds_total{operation="`+op+`"} `+strconv.FormatFloat(dur, 'f', 6, 64))
		}

		writeMetric(w, "# HELP presence_redis_errors_total Total Redis errors")
		writeMetric(w, "# TYPE presence_redis_errors_total counter")
		writeMetric(w, "presence_redis_errors_total "+strconv.FormatInt(m.redisErrors, 10))

		writeMetric(w, "# HELP presence_heartbeats_total Total heartbeats received")
		writeMetric(w, "# TYPE presence_heartbeats_total counter")
		writeMetric(w, `presence_heartbeats_total{status="accepted"} `+strconv.FormatInt(m.heartbeatsReceived, 10))
		writeMetric(w, `presence_heartbeats_total{status="rejected"} `+strconv.FormatInt(m.heartbeatsRejected, 10))

		writeMetric(w, "# HELP presence_sweeper_ticks_total Total sweeper ticks")
		writeMetric(w, "# TYPE presence_sweeper_ticks_total counter")
		writeMetric(w, "presence_sweeper_ticks_total "+strconv.FormatInt(m.sweeperTicks, 10))

		writeMetric(w, "# HELP presence_sweeper_sessions_pruned_total Total sessions pruned by sweeper")
		writeMetric(w, "# TYPE presence_sweeper_sessions_pruned_total counter")
		writeMetric(w, "presence_sweeper_sessions_pruned_total "+strconv.FormatInt(m.sweeperSessionsPruned, 10))

		writeMetric(w, "# HELP presence_sweeper_users_offline_total Total users transitioned offline by sweeper")
		writeMetric(w, "# TYPE presence_sweeper_users_offline_total counter")
		writeMetric(w, "presence_sweeper_users_offline_total "+strconv.FormatInt(m.sweeperUsersTransition, 10))

		writeMetric(w, "# HELP presence_debouncer_jobs_scheduled_total Total debouncer jobs scheduled")
		writeMetric(w, "# TYPE presence_debouncer_jobs_scheduled_total counter")
		writeMetric(w, "presence_debouncer_jobs_scheduled_total "+strconv.FormatInt(m.debouncerJobsScheduled, 10))

		writeMetric(w, "# HELP presence_debouncer_jobs_executed_total Total debouncer jobs executed")
		writeMetric(w, "# TYPE presence_debouncer_jobs_executed_total counter")
		writeMetric(w, "presence_debouncer_jobs_executed_total "+strconv.FormatInt(m.debouncerJobsExecuted, 10))

		writeMetric(w, "# HELP presence_debouncer_jobs_cancelled_total Total debouncer jobs cancelled")
		writeMetric(w, "# TYPE presence_debouncer_jobs_cancelled_total counter")
		writeMetric(w, "presence_debouncer_jobs_cancelled_total "+strconv.FormatInt(m.debouncerJobsCancelled, 10))

		writeMetric(w, "# HELP presence_uptime_seconds Time since service started")
		writeMetric(w, "# TYPE presence_uptime_seconds gauge")
		writeMetric(w, "presence_uptime_seconds "+strconv.FormatFloat(time.Since(m.startTime).Seconds(), 'f', 2, 64))
	})
}

func writeMetric(w http.ResponseWriter, line string) {
	w.Write([]byte(line + "\n"))
}

// Snapshot returns a copy of current metrics for JSON serialization.
func (m *Metrics) Snapshot() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	connectionsByScope := make(map[string]int64)
	for k, v := range m.connectionsByScope {
		connectionsByScope[k] = v
	}

	eventsPublished := make(map[string]int64)
	for k, v := range m.eventsPublished {
		eventsPublished[k] = v
	}

	redisOps := make(map[string]int64)
	for k, v := range m.redisOpsTotal {
		redisOps[k] = v
	}

	return map[string]interface{}{
		"connections": map[string]interface{}{
			"active":   m.connectionsActive,
			"by_scope": connectionsByScope,
			"attempts": m.connectAttempts,
			"failures": m.connectFailures,
		},
		"disconnects": map[string]interface{}{
			"total":    m.disconnects,
			"normal":   m.disconnectsNormal,
			"abnormal": m.disconnectsAbnormal,
		},
		"events": map[string]interface{}{
			"published":      eventsPublished,
			"dropped":        m.eventsDropped,
			"publish_errors": m.eventPublishErrs,
		},
		"redis": map[string]interface{}{
			"operations": redisOps,
			"errors":     m.redisErrors,
		},
		"heartbeats": map[string]interface{}{
			"received": m.heartbeatsReceived,
			"rejected": m.heartbeatsRejected,
		},
		"sweeper": map[string]interface{}{
			"ticks":              m.sweeperTicks,
			"sessions_pruned":    m.sweeperSessionsPruned,
			"users_transitioned": m.sweeperUsersTransition,
		},
		"debouncer": map[string]interface{}{
			"jobs_scheduled": m.debouncerJobsScheduled,
			"jobs_executed":  m.debouncerJobsExecuted,
			"jobs_cancelled": m.debouncerJobsCancelled,
		},
		"uptime_seconds": time.Since(m.startTime).Seconds(),
	}
}
