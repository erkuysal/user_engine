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
func loadEnvFile() {
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
}

// baseConfig returns the base configuration with common defaults.
// This is extended by environment-specific configs (dev, staging, prod).
func baseConfig() *Config {
	return &Config{
		// Redis - can be overridden by env vars
		RedisAddr:           getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword:       getEnv("REDIS_PASSWORD", ""),
		RedisDB:             getEnvInt("REDIS_DB", 0),
		RedisClusterEnabled: getEnvBool("REDIS_CLUSTER_ENABLED", false),
		RedisClusterAddrs:   getEnvStringSlice("REDIS_CLUSTER_ADDRS", nil),

		// Service addresses
		GatewayAddr: getEnv("GATEWAY_ADDR", ":8080"),
		APIAddr:     getEnv("API_ADDR", ":8081"),
		WorkerHealthAddr: getEnv("WORKER_HEALTH_ADDR", ":8080"),

		// Webhooks - common settings
		WebhookStreamName:    getEnv("WEBHOOK_STREAM_NAME", "presence:webhooks"),
		WebhookConsumerGroup: getEnv("WEBHOOK_CONSUMER_GROUP", "webhook-workers"),
		WebhookBatchSize:     int64(getEnvInt("WEBHOOK_BATCH_SIZE", 10)),
		WebhookRetryAttempts: getEnvInt("WEBHOOK_RETRY_ATTEMPTS", 3),
		WebhookRetryDelay:    getEnvDuration("WEBHOOK_RETRY_DELAY", 5*time.Second),
		WebhookTimeout:       getEnvDuration("WEBHOOK_TIMEOUT", 10*time.Second),
		WebhookSecret:        getEnv("WEBHOOK_SECRET", ""),

		// Flapping protection - common settings
		FlappingWindow:    getEnvDuration("FLAPPING_WINDOW", 30*time.Second),
		FlappingThreshold: getEnvInt("FLAPPING_THRESHOLD", 5),
		OfflineDelay:      getEnvDuration("OFFLINE_DELAY", 15*time.Second),

		// Gateway - common settings
		WriteTimeout:   getEnvDuration("WRITE_TIMEOUT", 10*time.Second),
		PongWait:       getEnvDuration("PONG_WAIT", 60*time.Second),
		PingPeriod:     getEnvDuration("PING_PERIOD", 54*time.Second),
		MaxMessageSize: int64(getEnvInt("MAX_MESSAGE_SIZE", 4096)),
		DisconnectSlowConsumers: getEnvBool("DISCONNECT_SLOW_CONSUMERS", false),
		MaxFriendSubscriptions:  getEnvInt("MAX_FRIEND_SUBSCRIPTIONS", 1000),
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

// IsStaging returns true if running in staging environment.
func (c *Config) IsStaging() bool {
	return c.Environment == "staging"
}

// Helper functions for reading environment variables

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
