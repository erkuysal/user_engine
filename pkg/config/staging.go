package config

import "time"

// stagingConfig returns the staging configuration.
// Staging mimics production settings but with relaxed validation for testing.
func stagingConfig() *Config {
	cfg := baseConfig()
	cfg.Environment = "staging"

	// JWT - should use real secrets (can be overridden)
	cfg.JWTSecret = getEnv("JWT_SECRET", "")

	// Session - production-like TTLs
	cfg.SessionTTL = getEnvDuration("SESSION_TTL", 45*time.Second)
	cfg.HeartbeatInterval = getEnvDuration("HEARTBEAT_INTERVAL", 15*time.Second)
	cfg.HeartbeatRateLimit = getEnvDuration("HEARTBEAT_RATE_LIMIT", 5*time.Second)
	cfg.HeartbeatJitterMax = getEnvDuration("HEARTBEAT_JITTER_MAX", 3*time.Second)
	cfg.ReconnectGracePeriod = getEnvDuration("RECONNECT_GRACE_PERIOD", 5*time.Second)

	// Debounce - production timings
	cfg.DisconnectDebounceDelay = getEnvDuration("DISCONNECT_DEBOUNCE_DELAY", 5*time.Second)
	cfg.DebouncerPollInterval = getEnvDuration("DEBOUNCER_POLL_INTERVAL", 1*time.Second)
	cfg.DebounceJobTTL = getEnvDuration("DEBOUNCE_JOB_TTL", 60*time.Second)

	// Sweeper - production intervals
	cfg.SweeperInterval = getEnvDuration("SWEEPER_INTERVAL", 30*time.Second)
	cfg.SweeperBudgetPerScope = getEnvDuration("SWEEPER_BUDGET_PER_SCOPE", 100*time.Millisecond)

	// Gateway - production limits
	cfg.MaxPendingEvents = getEnvInt("MAX_PENDING_EVENTS", 200)
	cfg.MaxConnsPerUser = getEnvInt("MAX_CONNS_PER_USER", 5)
	cfg.SlowConsumerBuffer = getEnvInt("SLOW_CONSUMER_BUFFER", 200)

	// CORS - explicit origins required
	cfg.CORSAllowAll = getEnvBool("CORS_ALLOW_ALL", false)
	cfg.CORSAllowedOrigins = getEnvStringSlice("CORS_ALLOWED_ORIGINS", nil)

	// Webhooks - enabled by default in staging
	cfg.WebhookEnabled = getEnvBool("WEBHOOK_ENABLED", true)

	return cfg
}
