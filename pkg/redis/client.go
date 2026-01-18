// Package redis provides Redis client factory functions for the presence engine.
package redis

import (
	"context"

	"github.com/userengine/presence/pkg/config"

	"github.com/go-redis/redis/v8"
)

// UniversalClient is a Redis client that works with both standalone and cluster modes.
// It wraps either redis.Client or redis.ClusterClient.
type UniversalClient interface {
	redis.Cmdable
	Close() error
}

// NewClient creates a Redis client based on configuration.
// If RedisClusterEnabled is true, returns a cluster client.
// Otherwise, returns a standalone client.
func NewClient(cfg *config.Config) UniversalClient {
	if cfg.RedisClusterEnabled && len(cfg.RedisClusterAddrs) > 0 {
		return redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:    cfg.RedisClusterAddrs,
			Password: cfg.RedisPassword,
		})
	}

	return redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
}

// Ping tests the Redis connection.
func Ping(ctx context.Context, client UniversalClient) error {
	// Cmdable interface includes Ping, so we can call it directly
	return client.Ping(ctx).Err()
}

// IsCluster returns true if the client is a cluster client.
func IsCluster(client UniversalClient) bool {
	_, ok := client.(*redis.ClusterClient)
	return ok
}

// AsStandalone casts the client to a standalone client if possible.
// Returns nil if it's a cluster client.
func AsStandalone(client UniversalClient) *redis.Client {
	c, _ := client.(*redis.Client)
	return c
}

// AsCluster casts the client to a cluster client if possible.
// Returns nil if it's a standalone client.
func AsCluster(client UniversalClient) *redis.ClusterClient {
	c, _ := client.(*redis.ClusterClient)
	return c
}
