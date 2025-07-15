package event

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
)

type PubSub interface {
	message.Publisher
	message.Subscriber
}

// EventBus manages event publishing and subscription
type EventBus struct {
	pubSub PubSub
	router *message.Router
	logger watermill.LoggerAdapter
}

// EventHandler is a function that handles events
type EventHandler[T any] func(ctx context.Context, event *Event[T]) error

// NewEventBus creates a new event bus
func NewEventBus() *EventBus {
	logger := watermill.NopLogger{}

	pubSub := gochannel.NewGoChannel(
		gochannel.Config{
			OutputChannelBuffer: 256,
		},
		logger,
	)

	router, err := message.NewRouter(message.RouterConfig{
		CloseTimeout: 5 * time.Second, // Allow time for handlers to complete gracefully
	}, logger)
	if err != nil {
		log.Fatalf("failed to create router: %v", err)
	}

	return &EventBus{
		pubSub: pubSub,
		router: router,
		logger: logger,
	}
}

// Start starts the event bus
func (eb *EventBus) Start(ctx context.Context) error {
	// Run router in a goroutine
	errCh := make(chan error, 1)
	defer close(errCh)

	go func() {
		if err := eb.router.Run(ctx); err != nil {
			errCh <- fmt.Errorf("event bus router failed: %w", err)
		}
	}()
	// Wait for router to start or error
	select {
	case <-ctx.Done():
		return fmt.Errorf("event bus context cancelled: %w", ctx.Err())
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("event bus router error: %w", err)
		}
	case <-eb.router.Running():
		// Router started successfully
		return nil
	}
	return fmt.Errorf("event bus router did not start successfully")
}

func (eb *EventBus) Stop() error {
	// Close the pubsub to stop publishing and subscribing
	if err := eb.pubSub.Close(); err != nil {
		return fmt.Errorf("failed to close pubsub: %w", err)
	}

	// Close the router
	if err := eb.router.Close(); err != nil {
		return fmt.Errorf("failed to close router: %w", err)
	}

	return nil
}

// Publish publishes an event
func (eb *EventBus) Publish(ctx context.Context, source string, data any) error {
	eventType := inferEventType(data)
	eventMsg := &EventMessage{
		ID:        generateEventID(),
		Type:      eventType,
		Timestamp: time.Now(),
		Source:    source,
	}

	rawData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal event data: %w", err)
	}
	eventMsg.Data = rawData

	payload, err := json.Marshal(eventMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	msg := message.NewMessage(watermill.NewUUID(), payload)
	msg.SetContext(ctx)

	if err := eb.pubSub.Publish(string(eventType), msg); err != nil {
		return fmt.Errorf("failed to publish event: %w", err)
	}
	return nil
}

// SubscribeAsync subscribes to events using watermill's message router
func (eb *EventBus) SubscribeAsync(eventType EventType, handlerName string, handler func(msg *message.Message) error) error {
	// Wrap handler with timeout middleware
	timeoutHandler := eb.withTimeout(handler, 15*time.Second)

	eb.router.AddNoPublisherHandler(
		handlerName,
		string(eventType),
		eb.pubSub,
		timeoutHandler,
	)

	// If router is already running, start the new handler
	if eb.isRunning() {
		ctx := context.Background()
		if err := eb.router.RunHandlers(ctx); err != nil {
			return fmt.Errorf("failed to start new handler: %w", err)
		}
	}

	return nil
}

// isRunning checks if the router is running
func (eb *EventBus) isRunning() bool {
	select {
	case <-eb.router.Running():
		return true
	default:
		return false
	}
}

// withTimeout wraps a handler with timeout protection
func (eb *EventBus) withTimeout(handler func(msg *message.Message) error, timeout time.Duration) func(msg *message.Message) error {
	return func(msg *message.Message) error {
		ctx, cancel := context.WithTimeout(msg.Context(), timeout)
		defer cancel()

		// Create a channel to receive the result
		resultCh := make(chan error, 1)

		// Run handler in a goroutine
		go func() {
			resultCh <- handler(msg)
		}()

		// Wait for either completion or timeout
		select {
		case err := <-resultCh:
			return err
		case <-ctx.Done():
			return fmt.Errorf("handler timeout after %v for event type %s", timeout, msg.Metadata.Get("topic"))
		}
	}
}

// PublishTyped publishes a typed event (helper function)
func PublishTyped[T any](eb *EventBus, ctx context.Context, event *Event[T]) error {
	eventMsg, err := event.ToMessage()
	if err != nil {
		return fmt.Errorf("failed to convert event to message: %w", err)
	}

	payload, err := json.Marshal(eventMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	msg := message.NewMessage(watermill.NewUUID(), payload)
	msg.SetContext(ctx)

	if err := eb.pubSub.Publish(string(eventMsg.Type), msg); err != nil {
		return fmt.Errorf("failed to publish event: %w", err)
	}
	return nil
}

// SubscribeTyped subscribes to typed events (helper function)
func SubscribeTyped[T any](eb *EventBus, eventType EventType, handlerName string, handler EventHandler[T]) error {
	return eb.SubscribeAsync(eventType, handlerName, func(msg *message.Message) error {
		var eventMsg EventMessage
		if err := json.Unmarshal(msg.Payload, &eventMsg); err != nil {
			return fmt.Errorf("failed to unmarshal event message: %w", err)
		}

		event, err := FromMessage[T](&eventMsg)
		if err != nil {
			return fmt.Errorf("failed to convert message to event: %w", err)
		}

		return handler(msg.Context(), event)
	})
}
