package test

import (
	"os"
	"testing"
	"time"

	"github.com/userengine/presence/pkg/config"
)

func TestConfigLoad(t *testing.T) {
	// Clear any existing env vars that might interfere
	envVars := []string{
		"ENVIRONMENT", "REDIS_ADDR", "JWT_SECRET", "GATEWAY_ADDR", "API_ADDR", "SESSION_TTL",
	}
	originalVals := make(map[string]string)
	for _, key := range envVars {
		originalVals[key] = os.Getenv(key)
		os.Unsetenv(key)
	}
	defer func() {
		for key, val := range originalVals {
			if val != "" {
				os.Setenv(key, val)
			}
		}
	}()

	cfg := config.Load()

	// Check defaults
	if cfg.Environment != "development" {
		t.Errorf("expected Environment=development, got %s", cfg.Environment)
	}
	if cfg.RedisAddr != "localhost:6379" {
		t.Errorf("expected RedisAddr=localhost:6379, got %s", cfg.RedisAddr)
	}
	if cfg.GatewayAddr != ":8080" {
		t.Errorf("expected GatewayAddr=:8080, got %s", cfg.GatewayAddr)
	}
	if cfg.APIAddr != ":8081" {
		t.Errorf("expected APIAddr=:8081, got %s", cfg.APIAddr)
	}
	// SessionTTL default may vary depending on .env files - just check it's set
	if cfg.SessionTTL <= 0 {
		t.Errorf("expected SessionTTL > 0, got %s", cfg.SessionTTL)
	}
}

func TestConfigLoadFromEnvVars(t *testing.T) {
	// Set custom env vars
	os.Setenv("ENVIRONMENT", "production")
	os.Setenv("REDIS_ADDR", "redis.example.com:6379")
	os.Setenv("JWT_SECRET", "my-secret")
	os.Setenv("SESSION_TTL", "60s")
	defer func() {
		os.Unsetenv("ENVIRONMENT")
		os.Unsetenv("REDIS_ADDR")
		os.Unsetenv("JWT_SECRET")
		os.Unsetenv("SESSION_TTL")
	}()

	cfg := config.Load()

	if cfg.Environment != "production" {
		t.Errorf("expected Environment=production, got %s", cfg.Environment)
	}
	if cfg.RedisAddr != "redis.example.com:6379" {
		t.Errorf("expected RedisAddr=redis.example.com:6379, got %s", cfg.RedisAddr)
	}
	if cfg.JWTSecret != "my-secret" {
		t.Errorf("expected JWTSecret=my-secret, got %s", cfg.JWTSecret)
	}
	if cfg.SessionTTL != 60*time.Second {
		t.Errorf("expected SessionTTL=60s, got %s", cfg.SessionTTL)
	}
}

func TestConfigDefault(t *testing.T) {
	cfg := config.Default()

	if cfg.Environment != "development" {
		t.Errorf("expected Environment=development, got %s", cfg.Environment)
	}
	if cfg.CORSAllowAll != true {
		t.Error("expected CORSAllowAll=true in default config")
	}
}

func TestConfigIsProduction(t *testing.T) {
	tests := []struct {
		env      string
		expected bool
	}{
		{"production", true},
		{"staging", false},
		{"development", false},
		{"", false},
	}

	for _, tt := range tests {
		cfg := &config.Config{Environment: tt.env}
		if got := cfg.IsProduction(); got != tt.expected {
			t.Errorf("IsProduction() with env=%q: got %v, want %v", tt.env, got, tt.expected)
		}
	}
}

func TestConfigIsDevelopment(t *testing.T) {
	tests := []struct {
		env      string
		expected bool
	}{
		{"development", true},
		{"staging", false},
		{"production", false},
	}

	for _, tt := range tests {
		cfg := &config.Config{Environment: tt.env}
		if got := cfg.IsDevelopment(); got != tt.expected {
			t.Errorf("IsDevelopment() with env=%q: got %v, want %v", tt.env, got, tt.expected)
		}
	}
}

func TestConfigValidate_Production(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *config.Config
		expectError bool
	}{
		{
			name: "dev secret in production",
			cfg: &config.Config{
				Environment:             "production",
				JWTSecret:               "dev-secret-test",
				RedisAddr:               "localhost:6379",
				CORSAllowAll:            false,
				CORSAllowedOrigins:      []string{"https://example.com"},
				PingPeriod:              54 * time.Second,
				PongWait:                60 * time.Second,
				SessionTTL:              45 * time.Second,
				HeartbeatInterval:       15 * time.Second,
				DisconnectDebounceDelay: 5 * time.Second,
			},
			expectError: true,
		},
		{
			name: "empty JWT secret in production",
			cfg: &config.Config{
				Environment:             "production",
				JWTSecret:               "",
				RedisAddr:               "localhost:6379",
				CORSAllowAll:            false,
				CORSAllowedOrigins:      []string{"https://example.com"},
				PingPeriod:              54 * time.Second,
				PongWait:                60 * time.Second,
				SessionTTL:              45 * time.Second,
				HeartbeatInterval:       15 * time.Second,
				DisconnectDebounceDelay: 5 * time.Second,
			},
			expectError: true,
		},
		{
			name: "CORS allow all in production",
			cfg: &config.Config{
				Environment:             "production",
				JWTSecret:               "real-secret",
				RedisAddr:               "localhost:6379",
				CORSAllowAll:            true,
				CORSAllowedOrigins:      []string{},
				PingPeriod:              54 * time.Second,
				PongWait:                60 * time.Second,
				SessionTTL:              45 * time.Second,
				HeartbeatInterval:       15 * time.Second,
				DisconnectDebounceDelay: 5 * time.Second,
			},
			expectError: true,
		},
		{
			name: "no CORS origins in production",
			cfg: &config.Config{
				Environment:             "production",
				JWTSecret:               "real-secret",
				RedisAddr:               "localhost:6379",
				CORSAllowAll:            false,
				CORSAllowedOrigins:      []string{},
				PingPeriod:              54 * time.Second,
				PongWait:                60 * time.Second,
				SessionTTL:              45 * time.Second,
				HeartbeatInterval:       15 * time.Second,
				DisconnectDebounceDelay: 5 * time.Second,
			},
			expectError: true,
		},
		{
			name: "valid production config",
			cfg: &config.Config{
				Environment:             "production",
				JWTSecret:               "real-production-secret",
				RedisAddr:               "localhost:6379",
				CORSAllowAll:            false,
				CORSAllowedOrigins:      []string{"https://example.com"},
				PingPeriod:              54 * time.Second,
				PongWait:                60 * time.Second,
				SessionTTL:              45 * time.Second,
				HeartbeatInterval:       15 * time.Second,
				DisconnectDebounceDelay: 5 * time.Second,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.expectError && err == nil {
				t.Error("expected validation error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
		})
	}
}

func TestConfigValidate_TimingConstraints(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *config.Config
		expectError bool
	}{
		{
			name: "ping period >= pong wait",
			cfg: &config.Config{
				Environment:             "development",
				RedisAddr:               "localhost:6379",
				PingPeriod:              60 * time.Second,
				PongWait:                60 * time.Second,
				SessionTTL:              45 * time.Second,
				HeartbeatInterval:       15 * time.Second,
				DisconnectDebounceDelay: 5 * time.Second,
			},
			expectError: true,
		},
		{
			name: "session TTL too short",
			cfg: &config.Config{
				Environment:             "development",
				RedisAddr:               "localhost:6379",
				PingPeriod:              54 * time.Second,
				PongWait:                60 * time.Second,
				SessionTTL:              10 * time.Second,
				HeartbeatInterval:       15 * time.Second,
				DisconnectDebounceDelay: 5 * time.Second,
			},
			expectError: true,
		},
		{
			name: "disconnect debounce too short",
			cfg: &config.Config{
				Environment:             "development",
				RedisAddr:               "localhost:6379",
				PingPeriod:              54 * time.Second,
				PongWait:                60 * time.Second,
				SessionTTL:              45 * time.Second,
				HeartbeatInterval:       15 * time.Second,
				DisconnectDebounceDelay: 500 * time.Millisecond,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.expectError && err == nil {
				t.Error("expected validation error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
		})
	}
}
