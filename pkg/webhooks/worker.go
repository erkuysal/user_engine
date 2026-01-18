package webhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/userengine/presence/pkg/config"
	"github.com/userengine/presence/pkg/metrics"

	"github.com/go-redis/redis/v8"
	"github.com/rs/zerolog/log"
)

// Worker consumes webhook events from Redis Stream and delivers them.
type Worker struct {
	rdb          *redis.Client
	cfg          *config.Config
	producer     *Producer
	httpClient   *http.Client
	streamName   string
	groupName    string
	consumerName string
}

// NewWorker creates a new webhook worker.
func NewWorker(rdb *redis.Client, cfg *config.Config, consumerName string) *Worker {
	return &Worker{
		rdb:          rdb,
		cfg:          cfg,
		producer:     NewProducer(rdb, cfg),
		streamName:   cfg.WebhookStreamName,
		groupName:    cfg.WebhookConsumerGroup,
		consumerName: consumerName,
		httpClient: &http.Client{
			Timeout: cfg.WebhookTimeout,
		},
	}
}

// Run starts the worker loop, processing webhook events until context is cancelled.
func (w *Worker) Run(ctx context.Context) error {
	// Ensure stream and consumer group exist
	if err := w.producer.EnsureStream(ctx); err != nil {
		return fmt.Errorf("failed to ensure stream: %w", err)
	}

	log.Info().
		Str("stream", w.streamName).
		Str("group", w.groupName).
		Str("consumer", w.consumerName).
		Msg("webhook worker started")

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("webhook worker stopped")
			return ctx.Err()
		default:
		}

		// Read events from stream (blocking with timeout)
		streams, err := w.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    w.groupName,
			Consumer: w.consumerName,
			Streams:  []string{w.streamName, ">"},
			Count:    w.cfg.WebhookBatchSize,
			Block:    5 * time.Second,
		}).Result()

		if err != nil {
			if err == redis.Nil {
				// No new messages, continue
				continue
			}
			log.Error().Err(err).Msg("failed to read from stream")
			time.Sleep(time.Second)
			continue
		}

		for _, stream := range streams {
			for _, message := range stream.Messages {
				w.processMessage(ctx, message)
			}
		}
	}
}

func (w *Worker) processMessage(ctx context.Context, message redis.XMessage) {
	data, ok := message.Values["data"].(string)
	if !ok {
		log.Error().Str("message_id", message.ID).Msg("invalid message format")
		w.ackMessage(ctx, message.ID)
		return
	}

	var event WebhookEvent
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		log.Error().Err(err).Str("message_id", message.ID).Msg("failed to unmarshal webhook event")
		w.ackMessage(ctx, message.ID)
		return
	}

	// Attempt delivery
	if err := w.deliver(ctx, event); err != nil {
		log.Error().Err(err).
			Str("event_id", event.ID).
			Str("webhook_url", event.WebhookURL).
			Int("attempt", event.Attempt).
			Msg("webhook delivery failed")

		// Check if we should retry
		if event.Attempt < w.cfg.WebhookRetryAttempts {
			// Calculate backoff delay
			delay := w.calculateBackoff(event.Attempt)

			// Schedule retry after delay
			go func() {
				select {
				case <-ctx.Done():
					return
				case <-time.After(delay):
					if err := w.producer.PublishRetry(ctx, event); err != nil {
						log.Error().Err(err).Str("event_id", event.ID).Msg("failed to schedule retry")
					}
				}
			}()
		} else {
			log.Warn().
				Str("event_id", event.ID).
				Str("webhook_url", event.WebhookURL).
				Int("attempts", event.Attempt).
				Msg("webhook delivery failed permanently, moving to dead letter")

			// Could add dead letter queue here
			metrics.Global().IncEventPublishError()
		}
	}

	// Acknowledge the message
	w.ackMessage(ctx, message.ID)
}

func (w *Worker) deliver(ctx context.Context, event WebhookEvent) error {
	payload, err := json.Marshal(event.Event)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", event.WebhookURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "UserEngine-Webhook/1.0")
	req.Header.Set("X-Webhook-Event-ID", event.ID)
	req.Header.Set("X-Webhook-Event-Type", event.Event.Type)
	req.Header.Set("X-Webhook-Timestamp", event.CreatedAt.Format(time.RFC3339))

	// Add signature header if available
	if event.Signature != "" {
		req.Header.Set("X-Webhook-Signature", "sha256="+event.Signature)
	}

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body (limited to prevent memory issues)
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))

	// Check for successful status code (2xx)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(body))
	}

	log.Debug().
		Str("event_id", event.ID).
		Str("webhook_url", event.WebhookURL).
		Int("status", resp.StatusCode).
		Msg("webhook delivered successfully")

	return nil
}

func (w *Worker) ackMessage(ctx context.Context, messageID string) {
	if err := w.rdb.XAck(ctx, w.streamName, w.groupName, messageID).Err(); err != nil {
		log.Error().Err(err).Str("message_id", messageID).Msg("failed to ack message")
	}
}

func (w *Worker) calculateBackoff(attempt int) time.Duration {
	// Exponential backoff: delay * 2^attempt with max of 5 minutes
	delay := float64(w.cfg.WebhookRetryDelay) * math.Pow(2, float64(attempt))
	maxDelay := 5 * time.Minute
	if delay > float64(maxDelay) {
		delay = float64(maxDelay)
	}
	return time.Duration(delay)
}
