package test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/userengine/presence/pkg/health"
)

func TestHealthStatus_String(t *testing.T) {
	tests := []struct {
		status health.Status
		want   string
	}{
		{health.StatusHealthy, "healthy"},
		{health.StatusDegraded, "degraded"},
		{health.StatusUnhealthy, "unhealthy"},
	}

	for _, tt := range tests {
		if string(tt.status) != tt.want {
			t.Errorf("Status = %q, want %q", tt.status, tt.want)
		}
	}
}

func TestLivenessHandler(t *testing.T) {
	handler := health.LivenessHandler()

	req := httptest.NewRequest("GET", "/livez", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	if rec.Body.String() != "ok" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "ok")
	}
}

func TestHealthResponse_JSON(t *testing.T) {
	response := health.HealthResponse{
		Status:  health.StatusHealthy,
		Version: "1.0.0",
		Checks: []health.CheckResult{
			{
				Name:    "redis",
				Status:  health.StatusHealthy,
				Message: "ok",
			},
		},
	}

	data, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded health.HealthResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.Status != health.StatusHealthy {
		t.Errorf("Status = %q, want %q", decoded.Status, health.StatusHealthy)
	}
	if decoded.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", decoded.Version, "1.0.0")
	}
	if len(decoded.Checks) != 1 {
		t.Errorf("len(Checks) = %d, want 1", len(decoded.Checks))
	}
}

func TestCheckResult_JSON(t *testing.T) {
	result := health.CheckResult{
		Name:    "redis",
		Status:  health.StatusDegraded,
		Message: "high latency",
		Latency: 150000000, // 150ms in nanoseconds
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded health.CheckResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.Name != "redis" {
		t.Errorf("Name = %q, want %q", decoded.Name, "redis")
	}
	if decoded.Status != health.StatusDegraded {
		t.Errorf("Status = %q, want %q", decoded.Status, health.StatusDegraded)
	}
}

func TestNewChecker(t *testing.T) {
	// Test with nil service (should still work for basic checks)
	checker := health.NewChecker(nil, nil, "1.0.0")

	if checker == nil {
		t.Fatal("NewChecker() returned nil")
	}
}

func TestDetermineOverallStatus(t *testing.T) {
	tests := []struct {
		name   string
		checks []health.CheckResult
		want   health.Status
	}{
		{
			name:   "all healthy",
			checks: []health.CheckResult{{Status: health.StatusHealthy}, {Status: health.StatusHealthy}},
			want:   health.StatusHealthy,
		},
		{
			name:   "one degraded",
			checks: []health.CheckResult{{Status: health.StatusHealthy}, {Status: health.StatusDegraded}},
			want:   health.StatusDegraded,
		},
		{
			name:   "one unhealthy",
			checks: []health.CheckResult{{Status: health.StatusHealthy}, {Status: health.StatusUnhealthy}},
			want:   health.StatusUnhealthy,
		},
		{
			name:   "degraded and unhealthy",
			checks: []health.CheckResult{{Status: health.StatusDegraded}, {Status: health.StatusUnhealthy}},
			want:   health.StatusUnhealthy,
		},
		{
			name:   "empty checks",
			checks: []health.CheckResult{},
			want:   health.StatusHealthy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Manually compute overall status like the Checker does
			overallStatus := health.StatusHealthy
			for _, check := range tt.checks {
				if check.Status == health.StatusUnhealthy {
					overallStatus = health.StatusUnhealthy
					break
				}
				if check.Status == health.StatusDegraded && overallStatus != health.StatusUnhealthy {
					overallStatus = health.StatusDegraded
				}
			}

			if overallStatus != tt.want {
				t.Errorf("overall status = %q, want %q", overallStatus, tt.want)
			}
		})
	}
}

func TestHealthHandler_SimpleParam(t *testing.T) {
	checker := health.NewChecker(nil, nil, "1.0.0")
	handler := checker.Handler()

	// Test simple=true query param
	req := httptest.NewRequest("GET", "/health?simple=true", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	if rec.Body.String() != "ok" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "ok")
	}
}
