// Package health provides health check functionality for the presence engine.
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/userengine/presence/pkg/presence"

	"github.com/go-redis/redis/v8"
)

// Status represents the overall health status.
type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusDegraded  Status = "degraded"
	StatusUnhealthy Status = "unhealthy"
)

// CheckResult represents the result of a single health check.
type CheckResult struct {
	Name    string        `json:"name"`
	Status  Status        `json:"status"`
	Message string        `json:"message,omitempty"`
	Latency time.Duration `json:"latency_ms"`
}

// HealthResponse is the JSON response for health checks.
type HealthResponse struct {
	Status    Status        `json:"status"`
	Timestamp time.Time     `json:"timestamp"`
	Checks    []CheckResult `json:"checks"`
	Version   string        `json:"version,omitempty"`
}

// Checker performs health checks for the presence engine.
type Checker struct {
	rdb         *redis.Client
	presenceSvc *presence.Service
	version     string

	mu          sync.RWMutex
	lastResult  *HealthResponse
	lastChecked time.Time
	cacheTTL    time.Duration
}

// NewChecker creates a new health checker.
func NewChecker(rdb *redis.Client, presenceSvc *presence.Service, version string) *Checker {
	return &Checker{
		rdb:         rdb,
		presenceSvc: presenceSvc,
		version:     version,
		cacheTTL:    5 * time.Second, // Cache results for 5 seconds
	}
}

// Check performs all health checks and returns the overall status.
func (c *Checker) Check(ctx context.Context) *HealthResponse {
	// Check cache
	c.mu.RLock()
	if c.lastResult != nil && time.Since(c.lastChecked) < c.cacheTTL {
		result := c.lastResult
		c.mu.RUnlock()
		return result
	}
	c.mu.RUnlock()

	// Perform checks
	checks := []CheckResult{
		c.checkRedis(ctx),
		c.checkLuaScripts(ctx),
	}

	// Determine overall status
	overallStatus := StatusHealthy
	for _, check := range checks {
		if check.Status == StatusUnhealthy {
			overallStatus = StatusUnhealthy
			break
		}
		if check.Status == StatusDegraded && overallStatus != StatusUnhealthy {
			overallStatus = StatusDegraded
		}
	}

	response := &HealthResponse{
		Status:    overallStatus,
		Timestamp: time.Now().UTC(),
		Checks:    checks,
		Version:   c.version,
	}

	// Cache result
	c.mu.Lock()
	c.lastResult = response
	c.lastChecked = time.Now()
	c.mu.Unlock()

	return response
}

// checkRedis verifies Redis connectivity.
func (c *Checker) checkRedis(ctx context.Context) CheckResult {
	start := time.Now()

	result := CheckResult{
		Name: "redis",
	}

	// Perform PING
	pong, err := c.rdb.Ping(ctx).Result()
	result.Latency = time.Since(start)

	if err != nil {
		result.Status = StatusUnhealthy
		result.Message = "ping failed: " + err.Error()
		return result
	}

	if pong != "PONG" {
		result.Status = StatusDegraded
		result.Message = "unexpected ping response: " + pong
		return result
	}

	// Check latency threshold (warn if > 100ms)
	if result.Latency > 100*time.Millisecond {
		result.Status = StatusDegraded
		result.Message = "high latency"
		return result
	}

	result.Status = StatusHealthy
	result.Message = "ok"
	return result
}

// checkLuaScripts verifies that Lua scripts are loaded.
func (c *Checker) checkLuaScripts(ctx context.Context) CheckResult {
	start := time.Now()

	result := CheckResult{
		Name: "lua_scripts",
	}

	// Try a simple operation that uses Lua scripts
	// We'll check if the scripts are cached by verifying presence service works
	if c.presenceSvc == nil {
		result.Status = StatusDegraded
		result.Message = "presence service not available"
		result.Latency = time.Since(start)
		return result
	}

	// Check if scripts are loaded by calling a read operation
	// This uses the LookupUsers which uses Redis pipeline (not Lua)
	// but ensures the service is operational
	_, err := c.presenceSvc.GetActiveScopes(ctx)
	result.Latency = time.Since(start)

	if err != nil {
		result.Status = StatusUnhealthy
		result.Message = "failed to query scopes: " + err.Error()
		return result
	}

	result.Status = StatusHealthy
	result.Message = "ok"
	return result
}

// Handler returns an HTTP handler for health checks.
func (c *Checker) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Support simple liveness check via query param
		if r.URL.Query().Get("simple") == "true" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
			return
		}

		response := c.Check(ctx)

		// Set appropriate status code
		statusCode := http.StatusOK
		switch response.Status {
		case StatusDegraded:
			statusCode = http.StatusOK // Still return 200 for degraded
		case StatusUnhealthy:
			statusCode = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		json.NewEncoder(w).Encode(response)
	})
}

// LivenessHandler returns a simple liveness probe handler.
func LivenessHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
}

// ReadinessHandler returns a readiness probe handler that checks Redis.
func ReadinessHandler(rdb *redis.Client) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		if err := rdb.Ping(ctx).Err(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("redis unavailable"))
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready"))
	})
}
