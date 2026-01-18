package events

import (
	"context"
	"encoding/json"

	"github.com/userengine/presence/pkg/metrics"

	"github.com/go-redis/redis/v8"
)

// Publisher publishes presence events.
type Publisher interface {
	Publish(ctx context.Context, scopeID string, event PresenceEvent) error
}

// Subscriber subscribes to presence events for a scope.
type Subscriber interface {
	Subscribe(ctx context.Context, scopeID string) (<-chan PresenceEvent, func(), error)
}

// EventBus combines Publisher and Subscriber.
type EventBus interface {
	Publisher
	Subscriber
}

// RedisPubSub implements EventBus using Redis Pub/Sub.
type RedisPubSub struct {
	rdb *redis.Client
}

// NewRedisPubSub creates a new Redis Pub/Sub event bus.
func NewRedisPubSub(rdb *redis.Client) *RedisPubSub {
	return &RedisPubSub{rdb: rdb}
}

// Publish sends a presence event to the scope's channel.
func (r *RedisPubSub) Publish(ctx context.Context, scopeID string, event PresenceEvent) error {
	channel := ChannelName(scopeID)

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	if err := r.rdb.Publish(ctx, channel, data).Err(); err != nil {
		metrics.Global().IncEventPublishError()
		return err
	}

	metrics.Global().IncEventPublished(event.Type)
	return nil
}

// Subscribe returns a channel of presence events for a scope.
func (r *RedisPubSub) Subscribe(ctx context.Context, scopeID string) (<-chan PresenceEvent, func(), error) {
	channel := ChannelName(scopeID)
	pubsub := r.rdb.Subscribe(ctx, channel)

	// Wait for subscription confirmation
	_, err := pubsub.Receive(ctx)
	if err != nil {
		pubsub.Close()
		return nil, nil, err
	}

	events := make(chan PresenceEvent, 100)

	go func() {
		defer close(events)
		ch := pubsub.Channel()

		for msg := range ch {
			var event PresenceEvent
			if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
				continue
			}

			select {
			case events <- event:
			default:
				// Drop event if buffer full (slow consumer)
			}
		}
	}()

	cancel := func() {
		pubsub.Close()
	}

	return events, cancel, nil
}

// ChannelName returns the Pub/Sub channel for a scope's presence events.
func ChannelName(scopeID string) string {
	return "presence:scope:" + scopeID
}

// NoOpPublisher is a publisher that does nothing.
// Useful for testing or when events aren't needed.
type NoOpPublisher struct{}

// Publish does nothing.
func (NoOpPublisher) Publish(ctx context.Context, scopeID string, event PresenceEvent) error {
	return nil
}
