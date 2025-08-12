package event

import (
	"context"
	"testing"
	"time"
)

func TestNewEvent(t *testing.T) {
	source := "test-service"
	data := TaskCreatedData{
		TaskID: "TASK-001",
		Title:  "Test Task",
	}

	event := NewEvent(source, data)

	if event.Source != source {
		t.Errorf("Expected event source %s, got %s", source, event.Source)
	}

	if event.ID == "" {
		t.Error("Expected event ID to be generated")
	}

	if event.Timestamp.IsZero() {
		t.Error("Expected timestamp to be set")
	}

	if event.Data.TaskID != "TASK-001" {
		t.Error("Expected task_id to be TASK-001")
	}
}

func TestEventBus_PublishSubscribeAsync(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventBus := NewEventBus()

	// Channel to receive events
	received := make(chan *Event[TaskCreatedData], 1)

	// Subscribe to TaskCreated events using SubscribeTyped
	err := SubscribeTyped(eventBus, TaskCreated, "test-handler", func(ctx context.Context, event *Event[TaskCreatedData]) error {
		received <- event
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Start the event bus in background
	go func() {
		if err := eventBus.Start(ctx); err != nil && err != context.Canceled {
			t.Errorf("Failed to start event bus: %v", err)
		}
	}()

	// Wait a bit for the router to start
	time.Sleep(100 * time.Millisecond)

	// Publish a TaskCreated event
	data := TaskCreatedData{
		TaskID:      "TASK-001",
		Title:       "Test Task",
		Description: "Test Description",
		Type:        "feature",
	}

	event := NewEvent("test-service", data)
	err = PublishTyped(eventBus, ctx, event)
	if err != nil {
		t.Fatalf("Failed to publish event: %v", err)
	}

	// Wait for event to be received
	select {
	case receivedEvent := <-received:
		if receivedEvent.Data.TaskID != "TASK-001" {
			t.Error("Expected task_id to be TASK-001")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Event not received within timeout")
	}
}

func TestEventBus_MultipleSubscribersAsync(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventBus := NewEventBus()

	// Channels to receive events
	received1 := make(chan *Event[TaskStatusChangedData], 1)
	received2 := make(chan *Event[TaskStatusChangedData], 1)

	// Subscribe two handlers to the same event type using SubscribeTyped
	err := SubscribeTyped(eventBus, TaskStatusChanged, "test-handler-1", func(ctx context.Context, event *Event[TaskStatusChangedData]) error {
		received1 <- event
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to subscribe handler 1: %v", err)
	}

	err = SubscribeTyped(eventBus, TaskStatusChanged, "test-handler-2", func(ctx context.Context, event *Event[TaskStatusChangedData]) error {
		received2 <- event
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to subscribe handler 2: %v", err)
	}

	// Start the event bus in background
	go func() {
		if err := eventBus.Start(ctx); err != nil && err != context.Canceled {
			t.Errorf("Failed to start event bus: %v", err)
		}
	}()

	// Wait a bit for the router to start
	time.Sleep(100 * time.Millisecond)

	// Publish a TaskStatusChanged event
	data := TaskStatusChangedData{
		TaskID:    "TASK-001",
		OldStatus: "CREATED",
		NewStatus: "IN_PROGRESS",
	}

	event := NewEvent("test-service", data)
	err = PublishTyped(eventBus, ctx, event)
	if err != nil {
		t.Fatalf("Failed to publish event: %v", err)
	}

	// Wait for both handlers to receive the event
	received1Count := 0
	received2Count := 0

	for received1Count == 0 || received2Count == 0 {
		select {
		case <-received1:
			received1Count++
		case <-received2:
			received2Count++
		case <-time.After(2 * time.Second):
			t.Fatal("Not all handlers received the event within timeout")
		}
	}
}
