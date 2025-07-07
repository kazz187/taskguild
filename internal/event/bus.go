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
	logger := watermill.NewStdLogger(false, false)

	pubSub := gochannel.NewGoChannel(
		gochannel.Config{
			OutputChannelBuffer: 256,
		},
		logger,
	)

	router, err := message.NewRouter(message.RouterConfig{}, logger)
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
	return eb.router.Run(ctx)
}

// Stop stops the event bus
func (eb *EventBus) Stop() error {
	return eb.router.Close()
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
	eb.router.AddNoPublisherHandler(
		handlerName,
		string(eventType),
		eb.pubSub,
		handler,
	)

	return nil
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
