package config

import "time"

// devConfig returns the development configuration.
// Development mode has relaxed security for local testing.
func devConfig() *Config {
	cfg := baseConfig()
	cfg.Environment = "development"

	// JWT - dev secret (can be overridden by env var)
	cfg.JWTSecret = getEnv("JWT_SECRET", "dev-secret-change-in-production")

	// Session - shorter TTLs for faster testing
	cfg.SessionTTL = getEnvDuration("SESSION_TTL", 12*time.Second)
	cfg.HeartbeatInterval = getEnvDuration("HEARTBEAT_INTERVAL", 5*time.Second)
	cfg.HeartbeatRateLimit = getEnvDuration("HEARTBEAT_RATE_LIMIT", 2*time.Second)
	cfg.HeartbeatJitterMax = getEnvDuration("HEARTBEAT_JITTER_MAX", 1*time.Second)
	cfg.ReconnectGracePeriod = getEnvDuration("RECONNECT_GRACE_PERIOD", 3*time.Second)

	// Debounce - faster for testing
	cfg.DisconnectDebounceDelay = getEnvDuration("DISCONNECT_DEBOUNCE_DELAY", 2*time.Second)
	cfg.DebouncerPollInterval = getEnvDuration("DEBOUNCER_POLL_INTERVAL", 500*time.Millisecond)
	cfg.DebounceJobTTL = getEnvDuration("DEBOUNCE_JOB_TTL", 30*time.Second)

	// Sweeper - more frequent in development
	cfg.SweeperInterval = getEnvDuration("SWEEPER_INTERVAL", 5*time.Second)
	cfg.SweeperBudgetPerScope = getEnvDuration("SWEEPER_BUDGET_PER_SCOPE", 100*time.Millisecond)

	// Gateway - relaxed limits
	cfg.MaxPendingEvents = getEnvInt("MAX_PENDING_EVENTS", 100)
	cfg.MaxConnsPerUser = getEnvInt("MAX_CONNS_PER_USER", 10)
	cfg.SlowConsumerBuffer = getEnvInt("SLOW_CONSUMER_BUFFER", 100)

	// CORS - always allow all in development mode (ignore .env CORS settings)
	cfg.CORSAllowAll = true
	cfg.CORSAllowedOrigins = []string{"*"} // Not used when CORSAllowAll is true

	// Webhooks - disabled by default in development
	cfg.WebhookEnabled = getEnvBool("WEBHOOK_ENABLED", false)

	return cfg
}
