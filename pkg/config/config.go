package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all configuration for the presence engine.
type Config struct {
	// Redis connection
	RedisAddr     string
	RedisPassword string
	RedisDB       int

	// Redis Cluster settings
	RedisClusterEnabled bool
	RedisClusterAddrs   []string

	// Service addresses
	GatewayAddr string
	APIAddr     string

	// JWT
	JWTSecret string

	// Session settings
	SessionTTL time.Duration

	// Heartbeat settings
	HeartbeatInterval    time.Duration
	HeartbeatRateLimit   time.Duration
	HeartbeatJitterMax   time.Duration
	ReconnectGracePeriod time.Duration

	// Debounce settings
	DisconnectDebounceDelay time.Duration
	DebouncerPollInterval   time.Duration
	DebounceJobTTL          time.Duration

	// Flapping protection
	FlappingWindow    time.Duration
	FlappingThreshold int
	OfflineDelay      time.Duration

	// Sweeper settings
	SweeperInterval       time.Duration
	SweeperBudgetPerScope time.Duration

	// Gateway settings
	MaxPendingEvents   int
	WriteTimeout       time.Duration
	PongWait           time.Duration
	PingPeriod         time.Duration
	MaxMessageSize     int64
	MaxConnsPerUser    int
	SlowConsumerBuffer int

	// CORS settings
	CORSAllowedOrigins []string
	CORSAllowAll       bool

	// Webhook settings
	WebhookEnabled       bool
	WebhookStreamName    string
	WebhookConsumerGroup string
	WebhookBatchSize     int64
	WebhookRetryAttempts int
	WebhookRetryDelay    time.Duration
	WebhookTimeout       time.Duration
	WebhookSecret        string

	// Environment
	Environment string
}

// Load reads configuration based on USERENGINE_ENVIRONMENT variable.
// Similar to Django's settings/__init__.py pattern.
//
// Environment selection (defaults to "development" if not set):
//   - "development" -> dev.go (default for local development)
//   - "staging"     -> staging.go
//   - "production"  -> prod.go (must be explicitly set)
//
// Configuration is loaded from .env files and environment variables.
func Load() *Config {
	// Load .env file(s) before reading environment variables
	loadEnvFile()

	// Get environment from USERENGINE_ENVIRONMENT (defaults to development)
	env := getEnv("USERENGINE_ENVIRONMENT", "development")

	// Print active configuration
	fmt.Printf("\n%s\nUserEngine is running on %s configuration\n%s\n\n",
		strings.Repeat("=", 50),
		strings.ToUpper(env),
		strings.Repeat("=", 50))

	// Load environment-specific config
	var cfg *Config
	switch env {
	case "production":
		cfg = prodConfig()
	case "staging":
		cfg = stagingConfig()
	default:
		cfg = devConfig()
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Configuration validation failed:\n%v\n", err)
		os.Exit(1)
	}

	return cfg
}

// LoadFromFile loads configuration from a specific .env file path.
func LoadFromFile(path string) (*Config, error) {
	if err := godotenv.Load(path); err != nil {
		return nil, err
	}
	return Load(), nil
}

// MustLoadFromFile loads configuration from a specific .env file or panics.
func MustLoadFromFile(path string) *Config {
	cfg, err := LoadFromFile(path)
	if err != nil {
		panic("failed to load config from " + path + ": " + err.Error())
	}
	return cfg
}

// Default returns a config with default development settings.
func Default() *Config {
	return devConfig()
}

// Validate checks the configuration for common issues.
// Returns nil if valid, otherwise returns ValidationErrors.
func (c *Config) Validate() error {
	var errors ValidationErrors

	// Production-specific validation
	if c.IsProduction() {
		if err := validateProduction(c); err != nil {
			return err
		}
	}

	// Common validation for all environments
	if c.RedisAddr == "" {
		errors = append(errors, ValidationError{
			Field:   "REDIS_ADDR",
			Message: "Redis address is required",
		})
	}

	// Validate timing constraints
	if c.PingPeriod >= c.PongWait {
		errors = append(errors, ValidationError{
			Field:   "PING_PERIOD",
			Message: "PING_PERIOD must be less than PONG_WAIT",
		})
	}

	if c.SessionTTL < c.HeartbeatInterval*2 {
		errors = append(errors, ValidationError{
			Field:   "SESSION_TTL",
			Message: "SESSION_TTL should be at least 2x HEARTBEAT_INTERVAL",
		})
	}

	if c.DisconnectDebounceDelay < time.Second {
		errors = append(errors, ValidationError{
			Field:   "DISCONNECT_DEBOUNCE_DELAY",
			Message: "DISCONNECT_DEBOUNCE_DELAY should be at least 1 second",
		})
	}

	// Validate webhook config if enabled
	if c.WebhookEnabled {
		if c.WebhookBatchSize < 1 {
			errors = append(errors, ValidationError{
				Field:   "WEBHOOK_BATCH_SIZE",
				Message: "WEBHOOK_BATCH_SIZE must be at least 1",
			})
		}

		if c.WebhookRetryAttempts < 0 {
			errors = append(errors, ValidationError{
				Field:   "WEBHOOK_RETRY_ATTEMPTS",
				Message: "WEBHOOK_RETRY_ATTEMPTS cannot be negative",
			})
		}

		if c.WebhookTimeout < time.Second {
			errors = append(errors, ValidationError{
				Field:   "WEBHOOK_TIMEOUT",
				Message: "WEBHOOK_TIMEOUT should be at least 1 second",
			})
		}
	}

	if len(errors) > 0 {
		return errors
	}
	return nil
}

// MustValidate calls Validate and panics if validation fails.
func (c *Config) MustValidate() {
	if err := c.Validate(); err != nil {
		panic("config validation failed: " + err.Error())
	}
}
