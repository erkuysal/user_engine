package test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/userengine/presence/pkg/metrics"
)

func TestMetricsNew(t *testing.T) {
	m := metrics.New()
	if m == nil {
		t.Fatal("New() returned nil")
	}
}

func TestMetricsIncConnections(t *testing.T) {
	m := metrics.New()

	m.IncConnections("scope1")
	m.IncConnections("scope1")
	m.IncConnections("scope2")

	if m.TotalConnections() != 3 {
		t.Errorf("TotalConnections() = %d, want 3", m.TotalConnections())
	}

	if m.ConnectionCount("scope1") != 2 {
		t.Errorf("ConnectionCount(scope1) = %d, want 2", m.ConnectionCount("scope1"))
	}

	if m.ConnectionCount("scope2") != 1 {
		t.Errorf("ConnectionCount(scope2) = %d, want 1", m.ConnectionCount("scope2"))
	}
}

func TestMetricsDecConnections(t *testing.T) {
	m := metrics.New()

	m.IncConnections("scope1")
	m.IncConnections("scope1")

	m.DecConnections("scope1", true) // normal close

	if m.TotalConnections() != 1 {
		t.Errorf("TotalConnections() = %d, want 1", m.TotalConnections())
	}

	if m.ConnectionCount("scope1") != 1 {
		t.Errorf("ConnectionCount(scope1) = %d, want 1", m.ConnectionCount("scope1"))
	}

	// Check disconnect tracking
	snap := m.Snapshot()
	disconnects := snap["disconnects"].(map[string]interface{})
	if disconnects["normal"].(int64) != 1 {
		t.Errorf("normal disconnects = %d, want 1", disconnects["normal"])
	}
}

func TestMetricsDecConnections_Abnormal(t *testing.T) {
	m := metrics.New()

	m.IncConnections("scope1")
	m.DecConnections("scope1", false) // abnormal close

	snap := m.Snapshot()
	disconnects := snap["disconnects"].(map[string]interface{})
	if disconnects["abnormal"].(int64) != 1 {
		t.Errorf("abnormal disconnects = %d, want 1", disconnects["abnormal"])
	}
}

func TestMetricsDecConnections_NoNegative(t *testing.T) {
	m := metrics.New()

	// Decrement without increment - should not go negative
	m.DecConnections("scope1", true)

	if m.TotalConnections() != 0 {
		t.Errorf("TotalConnections() = %d, want 0 (should not go negative)", m.TotalConnections())
	}

	if m.ConnectionCount("scope1") != 0 {
		t.Errorf("ConnectionCount(scope1) = %d, want 0 (should not go negative)", m.ConnectionCount("scope1"))
	}
}

func TestMetricsEventMetrics(t *testing.T) {
	m := metrics.New()

	m.IncEventPublished("user_online")
	m.IncEventPublished("user_online")
	m.IncEventPublished("user_offline")
	m.IncEventConsumed("user_online")
	m.IncEventDropped()
	m.IncEventPublishError()

	snap := m.Snapshot()
	events := snap["events"].(map[string]interface{})

	published := events["published"].(map[string]int64)
	if published["user_online"] != 2 {
		t.Errorf("published[user_online] = %d, want 2", published["user_online"])
	}
	if published["user_offline"] != 1 {
		t.Errorf("published[user_offline] = %d, want 1", published["user_offline"])
	}

	if events["dropped"].(int64) != 1 {
		t.Errorf("dropped = %d, want 1", events["dropped"])
	}

	if events["publish_errors"].(int64) != 1 {
		t.Errorf("publish_errors = %d, want 1", events["publish_errors"])
	}
}

func TestMetricsRecordRedisOp(t *testing.T) {
	m := metrics.New()

	m.RecordRedisOp("GET", 10*time.Millisecond)
	m.RecordRedisOp("GET", 20*time.Millisecond)
	m.RecordRedisOp("SET", 5*time.Millisecond)

	snap := m.Snapshot()
	redis := snap["redis"].(map[string]interface{})
	ops := redis["operations"].(map[string]int64)

	if ops["GET"] != 2 {
		t.Errorf("redis ops[GET] = %d, want 2", ops["GET"])
	}
	if ops["SET"] != 1 {
		t.Errorf("redis ops[SET] = %d, want 1", ops["SET"])
	}
}

func TestMetricsIncRedisError(t *testing.T) {
	m := metrics.New()

	m.IncRedisError()
	m.IncRedisError()

	snap := m.Snapshot()
	redis := snap["redis"].(map[string]interface{})
	if redis["errors"].(int64) != 2 {
		t.Errorf("redis errors = %d, want 2", redis["errors"])
	}
}

func TestMetricsHeartbeatMetrics(t *testing.T) {
	m := metrics.New()

	m.IncHeartbeat(true)  // accepted
	m.IncHeartbeat(true)  // accepted
	m.IncHeartbeat(false) // rejected

	snap := m.Snapshot()
	heartbeats := snap["heartbeats"].(map[string]interface{})

	if heartbeats["received"].(int64) != 2 {
		t.Errorf("heartbeats received = %d, want 2", heartbeats["received"])
	}
	if heartbeats["rejected"].(int64) != 1 {
		t.Errorf("heartbeats rejected = %d, want 1", heartbeats["rejected"])
	}
}

func TestMetricsSweeperMetrics(t *testing.T) {
	m := metrics.New()

	m.IncSweeperTick()
	m.IncSweeperTick()
	m.RecordSweeperResults(5, 2)

	snap := m.Snapshot()
	sweeper := snap["sweeper"].(map[string]interface{})

	if sweeper["ticks"].(int64) != 2 {
		t.Errorf("sweeper ticks = %d, want 2", sweeper["ticks"])
	}
	if sweeper["sessions_pruned"].(int64) != 5 {
		t.Errorf("sessions_pruned = %d, want 5", sweeper["sessions_pruned"])
	}
	if sweeper["users_transitioned"].(int64) != 2 {
		t.Errorf("users_transitioned = %d, want 2", sweeper["users_transitioned"])
	}
}

func TestMetricsDebouncerMetrics(t *testing.T) {
	m := metrics.New()

	m.IncDebouncerJobScheduled()
	m.IncDebouncerJobScheduled()
	m.IncDebouncerJobExecuted()
	m.IncDebouncerJobCancelled(3)

	snap := m.Snapshot()
	debouncer := snap["debouncer"].(map[string]interface{})

	if debouncer["jobs_scheduled"].(int64) != 2 {
		t.Errorf("jobs_scheduled = %d, want 2", debouncer["jobs_scheduled"])
	}
	if debouncer["jobs_executed"].(int64) != 1 {
		t.Errorf("jobs_executed = %d, want 1", debouncer["jobs_executed"])
	}
	if debouncer["jobs_cancelled"].(int64) != 3 {
		t.Errorf("jobs_cancelled = %d, want 3", debouncer["jobs_cancelled"])
	}
}

func TestMetricsHandler(t *testing.T) {
	m := metrics.New()
	m.IncConnections("scope1")
	m.IncEventPublished("user_online")

	handler := m.Handler()

	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()

	// Check Prometheus format
	if !strings.Contains(body, "presence_connections_active") {
		t.Error("response should contain presence_connections_active")
	}
	if !strings.Contains(body, "presence_events_published_total") {
		t.Error("response should contain presence_events_published_total")
	}
	if !strings.Contains(body, "# TYPE") {
		t.Error("response should contain TYPE declarations")
	}
	if !strings.Contains(body, "# HELP") {
		t.Error("response should contain HELP descriptions")
	}
}

func TestMetricsSnapshot(t *testing.T) {
	m := metrics.New()
	m.IncConnections("scope1")

	snap := m.Snapshot()

	if snap == nil {
		t.Fatal("Snapshot() returned nil")
	}

	// Check structure
	if _, ok := snap["connections"]; !ok {
		t.Error("snapshot should contain connections")
	}
	if _, ok := snap["events"]; !ok {
		t.Error("snapshot should contain events")
	}
	if _, ok := snap["redis"]; !ok {
		t.Error("snapshot should contain redis")
	}
	if _, ok := snap["uptime_seconds"]; !ok {
		t.Error("snapshot should contain uptime_seconds")
	}
}

func TestMetricsGlobal(t *testing.T) {
	g := metrics.Global()
	if g == nil {
		t.Fatal("Global() returned nil")
	}

	// Should return same instance
	g2 := metrics.Global()
	if g != g2 {
		t.Error("Global() should return same instance")
	}
}

func TestMetricsConcurrentAccess(t *testing.T) {
	m := metrics.New()
	done := make(chan bool)

	// Run concurrent operations
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				m.IncConnections("scope1")
				m.DecConnections("scope1", true)
				m.IncEventPublished("test")
				m.RecordRedisOp("GET", time.Millisecond)
				m.Snapshot()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should not panic or have race conditions
	_ = m.TotalConnections()
}
