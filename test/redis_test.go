package test

import (
	"testing"

	"github.com/userengine/presence/pkg/config"
	uredis "github.com/userengine/presence/pkg/redis"
)

func TestRedisNewClient_Standalone(t *testing.T) {
	cfg := &config.Config{
		RedisAddr:           "localhost:6379",
		RedisClusterEnabled: false,
	}

	client := uredis.NewClient(cfg)
	if client == nil {
		t.Fatal("NewClient() returned nil")
	}

	// Verify it's not a cluster client
	if uredis.IsCluster(client) {
		t.Error("expected standalone client, got cluster client")
	}
}

func TestRedisNewClient_Cluster(t *testing.T) {
	cfg := &config.Config{
		RedisClusterEnabled: true,
		RedisClusterAddrs:   []string{"localhost:7000", "localhost:7001", "localhost:7002"},
	}

	client := uredis.NewClient(cfg)
	if client == nil {
		t.Fatal("NewClient() returned nil")
	}

	// Verify it's a cluster client
	if !uredis.IsCluster(client) {
		t.Error("expected cluster client, got standalone client")
	}
}

func TestRedisIsCluster(t *testing.T) {
	standaloneConfig := &config.Config{
		RedisAddr:           "localhost:6379",
		RedisClusterEnabled: false,
	}
	standalone := uredis.NewClient(standaloneConfig)

	if uredis.IsCluster(standalone) {
		t.Error("IsCluster() = true for standalone client, want false")
	}

	clusterConfig := &config.Config{
		RedisClusterEnabled: true,
		RedisClusterAddrs:   []string{"localhost:7000"},
	}
	cluster := uredis.NewClient(clusterConfig)

	if !uredis.IsCluster(cluster) {
		t.Error("IsCluster() = false for cluster client, want true")
	}
}

func TestRedisAsStandalone(t *testing.T) {
	cfg := &config.Config{
		RedisAddr:           "localhost:6379",
		RedisClusterEnabled: false,
	}

	client := uredis.NewClient(cfg)
	standalone := uredis.AsStandalone(client)

	if standalone == nil {
		t.Error("AsStandalone() returned nil for standalone client")
	}
}

func TestRedisAsCluster(t *testing.T) {
	cfg := &config.Config{
		RedisClusterEnabled: true,
		RedisClusterAddrs:   []string{"localhost:7000"},
	}

	client := uredis.NewClient(cfg)
	cluster := uredis.AsCluster(client)

	if cluster == nil {
		t.Error("AsCluster() returned nil for cluster client")
	}
}

func TestRedisAsStandalone_WithClusterClient(t *testing.T) {
	cfg := &config.Config{
		RedisClusterEnabled: true,
		RedisClusterAddrs:   []string{"localhost:7000"},
	}

	client := uredis.NewClient(cfg)
	standalone := uredis.AsStandalone(client)

	if standalone != nil {
		t.Error("AsStandalone() should return nil for cluster client")
	}
}

func TestRedisAsCluster_WithStandaloneClient(t *testing.T) {
	cfg := &config.Config{
		RedisAddr:           "localhost:6379",
		RedisClusterEnabled: false,
	}

	client := uredis.NewClient(cfg)
	cluster := uredis.AsCluster(client)

	if cluster != nil {
		t.Error("AsCluster() should return nil for standalone client")
	}
}
