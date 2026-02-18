package config

import (
	"net/url"
	"strings"
	"time"
)

// prodConfig returns the production configuration.
// Production mode enforces strict security and validation.
func prodConfig() *Config {
	cfg := baseConfig()
	cfg.Environment = "production"

	// JWT - MUST be provided via env var, no default
	cfg.JWTSecret = getEnv("JWT_SECRET", "")

	// Session - longer TTLs for stability
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

	// CORS - MUST be explicitly configured, no wildcards
	cfg.CORSAllowAll = false // Hardcoded false in production
	cfg.CORSAllowedOrigins = getEnvStringSlice("CORS_ALLOWED_ORIGINS", nil)

	// Webhooks - enabled by default in production
	cfg.WebhookEnabled = getEnvBool("WEBHOOK_ENABLED", true)

	return cfg
}

// ValidationError represents a configuration validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return e.Field + ": " + e.Message
}

// ValidationErrors is a collection of validation errors.
type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return ""
	}
	if len(e) == 1 {
		return e[0].Error()
	}
	msg := "multiple validation errors:\n"
	for _, err := range e {
		msg += "  - " + err.Error() + "\n"
	}
	return msg
}

// validateProduction validates production-specific requirements.
func validateProduction(cfg *Config) error {
	var errors ValidationErrors

	// JWT validation
	if cfg.JWTSecret == "" {
		errors = append(errors, ValidationError{
			Field:   "JWT_SECRET",
			Message: "JWT_SECRET is required in production",
		})
	}

	if strings.HasPrefix(cfg.JWTSecret, "dev-secret") {
		errors = append(errors, ValidationError{
			Field:   "JWT_SECRET",
			Message: "dev-secret is not allowed in production",
		})
	}

	if len(cfg.JWTSecret) > 0 && len(cfg.JWTSecret) < 32 {
		errors = append(errors, ValidationError{
			Field:   "JWT_SECRET",
			Message: "JWT_SECRET should be at least 32 characters in production",
		})
	}

	// CORS validation
	if cfg.CORSAllowAll {
		errors = append(errors, ValidationError{
			Field:   "CORS_ALLOW_ALL",
			Message: "CORS_ALLOW_ALL=true is not allowed in production",
		})
	}

	if len(cfg.CORSAllowedOrigins) == 0 {
		errors = append(errors, ValidationError{
			Field:   "CORS_ALLOWED_ORIGINS",
			Message: "CORS_ALLOWED_ORIGINS must be set in production",
		})
	}

	for _, raw := range cfg.CORSAllowedOrigins {
		origin := strings.TrimSpace(raw)
		if origin == "" {
			continue
		}
		if strings.Contains(origin, "*") {
			errors = append(errors, ValidationError{
				Field:   "CORS_ALLOWED_ORIGINS",
				Message: "wildcard origins are not allowed in production (remove '*' entries)",
			})
			break
		}
		u, err := url.Parse(origin)
		if err != nil || u.Scheme == "" || u.Host == "" {
			errors = append(errors, ValidationError{
				Field:   "CORS_ALLOWED_ORIGINS",
				Message: "invalid origin in CORS_ALLOWED_ORIGINS: " + origin,
			})
			break
		}
		if u.Scheme != "https" && u.Scheme != "http" {
			errors = append(errors, ValidationError{
				Field:   "CORS_ALLOWED_ORIGINS",
				Message: "CORS origin must be http or https: " + origin,
			})
			break
		}
	}

	// Webhook validation
	if cfg.WebhookEnabled && cfg.WebhookSecret == "" {
		errors = append(errors, ValidationError{
			Field:   "WEBHOOK_SECRET",
			Message: "WEBHOOK_SECRET is required when webhooks are enabled in production",
		})
	}

	if len(errors) > 0 {
		return errors
	}
	return nil
}
