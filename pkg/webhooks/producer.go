// Package webhooks provides async webhook delivery via Redis Streams.
package webhooks

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/userengine/presence/pkg/config"
	"github.com/userengine/presence/pkg/events"

	"github.com/go-redis/redis/v8"
	"github.com/rs/zerolog/log"
)

// WebhookEvent represents an event to be sent via webhook.
type WebhookEvent struct {
	ID         string               `json:"id"`
	Event      events.PresenceEvent `json:"event"`
	WebhookURL string               `json:"webhook_url"`
	CreatedAt  time.Time            `json:"created_at"`
	Attempt    int                  `json:"attempt"`
	Signature  string               `json:"signature,omitempty"`
}

// Producer publishes webhook events to a Redis Stream.
type Producer struct {
	rdb        *redis.Client
	cfg        *config.Config
	streamName string
}

// NewProducer creates a new webhook producer.
func NewProducer(rdb *redis.Client, cfg *config.Config) *Producer {
	return &Producer{
		rdb:        rdb,
		cfg:        cfg,
		streamName: cfg.WebhookStreamName,
	}
}

// Publish adds a webhook event to the stream for async delivery.
func (p *Producer) Publish(ctx context.Context, event events.PresenceEvent, webhookURL string) error {
	if !p.cfg.WebhookEnabled {
		return nil
	}

	webhookEvent := WebhookEvent{
		ID:         event.EventID,
		Event:      event,
		WebhookURL: webhookURL,
		CreatedAt:  time.Now().UTC(),
		Attempt:    0,
	}

	// Generate HMAC signature if secret is configured
	if p.cfg.WebhookSecret != "" {
		payload, _ := json.Marshal(event)
		webhookEvent.Signature = p.sign(payload)
	}

	data, err := json.Marshal(webhookEvent)
	if err != nil {
		return err
	}

	// Add to Redis Stream with auto-generated ID
	_, err = p.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: p.streamName,
		Values: map[string]interface{}{
			"data": string(data),
		},
	}).Result()

	if err != nil {
		log.Error().Err(err).
			Str("stream", p.streamName).
			Str("event_id", event.EventID).
			Msg("failed to publish webhook event")
		return err
	}

	log.Debug().
		Str("stream", p.streamName).
		Str("event_id", event.EventID).
		Str("webhook_url", webhookURL).
		Msg("webhook event published")

	return nil
}

// PublishRetry adds a failed webhook event back to the stream for retry.
func (p *Producer) PublishRetry(ctx context.Context, event WebhookEvent) error {
	event.Attempt++
	event.CreatedAt = time.Now().UTC()

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	_, err = p.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: p.streamName,
		Values: map[string]interface{}{
			"data": string(data),
		},
	}).Result()

	return err
}

// sign generates an HMAC-SHA256 signature for the payload.
func (p *Producer) sign(payload []byte) string {
	mac := hmac.New(sha256.New, []byte(p.cfg.WebhookSecret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// EnsureStream creates the stream and consumer group if they don't exist.
func (p *Producer) EnsureStream(ctx context.Context) error {
	// Create consumer group, will fail silently if already exists
	err := p.rdb.XGroupCreateMkStream(ctx, p.streamName, p.cfg.WebhookConsumerGroup, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return err
	}
	return nil
}
