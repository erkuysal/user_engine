package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// EnvFile is the default .env file name.
const EnvFile = ".env"

// loadEnvFile attempts to load environment variables from .env files.
// It searches in the following order:
// 1. Current directory
// 2. Parent directory (useful when running from cmd/ subdirectories)
// 3. Project root (if running from nested directories)
//
// Environment-specific files are also loaded if they exist:
// .env.local, .env.{environment}, .env.{environment}.local
//
// Variables already set in the environment take precedence over .env files.
func loadEnvFile() {
	// Try to find and load .env from various locations
	locations := []string{
		EnvFile,                            // Current directory
		filepath.Join("..", EnvFile),       // Parent directory
		filepath.Join("..", "..", EnvFile), // Grandparent (for cmd/*/main.go)
	}

	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			godotenv.Load(loc) // Ignore errors, env vars are optional
			break
		}
	}

	// Also try to load environment-specific files
	env := os.Getenv("ENVIRONMENT")
	if env == "" {
		env = "development"
	}

	// Load in order: .env.local, .env.{env}, .env.{env}.local
	// Later files override earlier ones, but env vars always win
	envFiles := []string{
		".env.local",
		".env." + env,
		".env." + env + ".local",
	}

	for _, f := range envFiles {
		godotenv.Load(f)
	}
}

// Config holds all configuration for the presence engine.
type Config struct {
	// Redis connection
	RedisAddr     string
	RedisPassword string
	RedisDB       int

	// Redis Cluster settings
	RedisClusterEnabled bool     // Enable Redis Cluster mode
	RedisClusterAddrs   []string // Cluster node addresses (comma-separated)

	// Service addresses (for standalone mode)
	GatewayAddr string
	APIAddr     string

	// JWT
	JWTSecret string

	// Session settings
	SessionTTL time.Duration

	// Heartbeat settings
	HeartbeatInterval    time.Duration
	HeartbeatRateLimit   time.Duration // Min time between heartbeats
	HeartbeatJitterMax   time.Duration
	ReconnectGracePeriod time.Duration

	// Debounce settings
	DisconnectDebounceDelay time.Duration
	DebouncerPollInterval   time.Duration
	DebounceJobTTL          time.Duration

	// Flapping protection (hysteresis)
	FlappingWindow    time.Duration // Window to track transitions
	FlappingThreshold int           // Max transitions in window before suppressing
	OfflineDelay      time.Duration // Sustained offline duration before broadcasting

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
	CORSAllowedOrigins []string // List of allowed origins, empty = allow all (dev only)
	CORSAllowAll       bool     // If true, allow all origins (insecure, dev only)

	// Webhook settings
	WebhookEnabled       bool          // Enable webhook notifications
	WebhookStreamName    string        // Redis Stream name for webhook events
	WebhookConsumerGroup string        // Consumer group name
	WebhookBatchSize     int64         // Max events to process per batch
	WebhookRetryAttempts int           // Max retry attempts for failed webhooks
	WebhookRetryDelay    time.Duration // Initial delay between retries
	WebhookTimeout       time.Duration // HTTP request timeout
	WebhookSecret        string        // Secret for HMAC signatures

	// Environment
	Environment string // "development", "staging", "production"
}

// Load reads configuration from environment variables with sensible defaults.
// It automatically loads variables from .env files if present.
func Load() *Config {
	// Load .env file(s) before reading environment variables
	loadEnvFile()

	return loadConfig()
}

// LoadFromFile loads configuration from a specific .env file path.
func LoadFromFile(path string) (*Config, error) {
	if err := godotenv.Load(path); err != nil {
		return nil, err
	}
	return loadConfig(), nil
}

// MustLoadFromFile loads configuration from a specific .env file or panics.
func MustLoadFromFile(path string) *Config {
	cfg, err := LoadFromFile(path)
	if err != nil {
		panic("failed to load config from " + path + ": " + err.Error())
	}
	return cfg
}

func loadConfig() *Config {
	env := getEnv("ENVIRONMENT", "development")
	corsOrigins := getEnvStringSlice("CORS_ALLOWED_ORIGINS", nil)
	corsAllowAll := getEnvBool("CORS_ALLOW_ALL", env == "development")

	return &Config{
		// Redis
		RedisAddr:           getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword:       getEnv("REDIS_PASSWORD", ""),
		RedisDB:             getEnvInt("REDIS_DB", 0),
		RedisClusterEnabled: getEnvBool("REDIS_CLUSTER_ENABLED", false),
		RedisClusterAddrs:   getEnvStringSlice("REDIS_CLUSTER_ADDRS", nil),

		// Service addresses
		GatewayAddr: getEnv("GATEWAY_ADDR", ":8080"),
		APIAddr:     getEnv("API_ADDR", ":8081"),

		// JWT
		JWTSecret: getEnv("JWT_SECRET", "dev-secret-change-in-production"),

		// Session: 45s TTL as per plan
		SessionTTL: getEnvDuration("SESSION_TTL", 45*time.Second),

		// Heartbeat: ~15s with jitter, rate limit 5s min
		HeartbeatInterval:    getEnvDuration("HEARTBEAT_INTERVAL", 15*time.Second),
		HeartbeatRateLimit:   getEnvDuration("HEARTBEAT_RATE_LIMIT", 5*time.Second),
		HeartbeatJitterMax:   getEnvDuration("HEARTBEAT_JITTER_MAX", 3*time.Second),
		ReconnectGracePeriod: getEnvDuration("RECONNECT_GRACE_PERIOD", 5*time.Second),

		// Debounce: 5s delay as per plan
		DisconnectDebounceDelay: getEnvDuration("DISCONNECT_DEBOUNCE_DELAY", 5*time.Second),
		DebouncerPollInterval:   getEnvDuration("DEBOUNCER_POLL_INTERVAL", 1*time.Second),
		DebounceJobTTL:          getEnvDuration("DEBOUNCE_JOB_TTL", 60*time.Second),

		// Flapping protection
		FlappingWindow:    getEnvDuration("FLAPPING_WINDOW", 30*time.Second),
		FlappingThreshold: getEnvInt("FLAPPING_THRESHOLD", 5),
		OfflineDelay:      getEnvDuration("OFFLINE_DELAY", 15*time.Second),

		// Sweeper: 30-45s interval, 50-200ms budget per scope
		SweeperInterval:       getEnvDuration("SWEEPER_INTERVAL", 30*time.Second),
		SweeperBudgetPerScope: getEnvDuration("SWEEPER_BUDGET_PER_SCOPE", 100*time.Millisecond),

		// Gateway
		MaxPendingEvents:   getEnvInt("MAX_PENDING_EVENTS", 100),
		WriteTimeout:       getEnvDuration("WRITE_TIMEOUT", 10*time.Second),
		PongWait:           getEnvDuration("PONG_WAIT", 60*time.Second),
		PingPeriod:         getEnvDuration("PING_PERIOD", 54*time.Second), // Must be less than PongWait
		MaxMessageSize:     int64(getEnvInt("MAX_MESSAGE_SIZE", 4096)),
		MaxConnsPerUser:    getEnvInt("MAX_CONNS_PER_USER", 10),
		SlowConsumerBuffer: getEnvInt("SLOW_CONSUMER_BUFFER", 100),

		// CORS
		CORSAllowedOrigins: corsOrigins,
		CORSAllowAll:       corsAllowAll,

		// Webhooks
		WebhookEnabled:       getEnvBool("WEBHOOK_ENABLED", false),
		WebhookStreamName:    getEnv("WEBHOOK_STREAM_NAME", "presence:webhooks"),
		WebhookConsumerGroup: getEnv("WEBHOOK_CONSUMER_GROUP", "webhook-workers"),
		WebhookBatchSize:     int64(getEnvInt("WEBHOOK_BATCH_SIZE", 10)),
		WebhookRetryAttempts: getEnvInt("WEBHOOK_RETRY_ATTEMPTS", 3),
		WebhookRetryDelay:    getEnvDuration("WEBHOOK_RETRY_DELAY", 5*time.Second),
		WebhookTimeout:       getEnvDuration("WEBHOOK_TIMEOUT", 10*time.Second),
		WebhookSecret:        getEnv("WEBHOOK_SECRET", ""),

		// Environment
		Environment: env,
	}
}

// Default returns a config with sensible defaults without reading env vars.
func Default() *Config {
	return &Config{
		RedisAddr:               "localhost:6379",
		RedisPassword:           "",
		RedisDB:                 0,
		RedisClusterEnabled:     false,
		RedisClusterAddrs:       nil,
		GatewayAddr:             ":8080",
		APIAddr:                 ":8081",
		JWTSecret:               "dev-secret-change-in-production",
		SessionTTL:              45 * time.Second,
		HeartbeatInterval:       15 * time.Second,
		HeartbeatRateLimit:      5 * time.Second,
		HeartbeatJitterMax:      3 * time.Second,
		ReconnectGracePeriod:    5 * time.Second,
		DisconnectDebounceDelay: 5 * time.Second,
		DebouncerPollInterval:   1 * time.Second,
		DebounceJobTTL:          60 * time.Second,
		FlappingWindow:          30 * time.Second,
		FlappingThreshold:       5,
		OfflineDelay:            15 * time.Second,
		SweeperInterval:         30 * time.Second,
		SweeperBudgetPerScope:   100 * time.Millisecond,
		MaxPendingEvents:        100,
		WriteTimeout:            10 * time.Second,
		PongWait:                60 * time.Second,
		PingPeriod:              54 * time.Second,
		MaxMessageSize:          4096,
		MaxConnsPerUser:         10,
		SlowConsumerBuffer:      100,
		CORSAllowedOrigins:      nil,
		CORSAllowAll:            true,
		WebhookEnabled:          false,
		WebhookStreamName:       "presence:webhooks",
		WebhookConsumerGroup:    "webhook-workers",
		WebhookBatchSize:        10,
		WebhookRetryAttempts:    3,
		WebhookRetryDelay:       5 * time.Second,
		WebhookTimeout:          10 * time.Second,
		WebhookSecret:           "",
		Environment:             "development",
	}
}

// IsProduction returns true if running in production environment.
func (c *Config) IsProduction() bool {
	return c.Environment == "production"
}

// IsDevelopment returns true if running in development environment.
func (c *Config) IsDevelopment() bool {
	return c.Environment == "development"
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

// Validate checks the configuration for common issues.
// Returns nil if valid, otherwise returns ValidationErrors.
func (c *Config) Validate() error {
	var errors ValidationErrors

	// In production, reject dev secrets
	if c.IsProduction() {
		if strings.HasPrefix(c.JWTSecret, "dev-secret") {
			errors = append(errors, ValidationError{
				Field:   "JWT_SECRET",
				Message: "dev-secret is not allowed in production",
			})
		}

		if c.JWTSecret == "" {
			errors = append(errors, ValidationError{
				Field:   "JWT_SECRET",
				Message: "JWT_SECRET is required in production",
			})
		}

		if c.CORSAllowAll {
			errors = append(errors, ValidationError{
				Field:   "CORS_ALLOW_ALL",
				Message: "CORS_ALLOW_ALL=true is not allowed in production",
			})
		}

		if len(c.CORSAllowedOrigins) == 0 {
			errors = append(errors, ValidationError{
				Field:   "CORS_ALLOWED_ORIGINS",
				Message: "CORS_ALLOWED_ORIGINS must be set in production",
			})
		}

		if c.WebhookEnabled && c.WebhookSecret == "" {
			errors = append(errors, ValidationError{
				Field:   "WEBHOOK_SECRET",
				Message: "WEBHOOK_SECRET is required when webhooks are enabled in production",
			})
		}
	}

	// Validate required fields
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

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		switch strings.ToLower(val) {
		case "true", "1", "yes", "on":
			return true
		case "false", "0", "no", "off":
			return false
		}
	}
	return defaultVal
}

func getEnvStringSlice(key string, defaultVal []string) []string {
	if val := os.Getenv(key); val != "" {
		parts := strings.Split(val, ",")
		result := make([]string, 0, len(parts))
		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				result = append(result, trimmed)
			}
		}
		if len(result) > 0 {
			return result
		}
	}
	return defaultVal
}
