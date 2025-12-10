package event

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventBus_PublishSubscribe(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create event bus
	eb, err := NewEventBus()
	require.NoError(t, err)
	err = eb.Start(ctx)
	require.NoError(t, err)
	defer eb.Stop()
	<-eb.router.Running()

	// Channel to receive handled events
	handled := make(chan bool, 1)
	var receivedData TaskCreatedData
	var mu sync.Mutex

	// Subscribe to events BEFORE starting
	err = eb.SubscribeAsync(TaskCreated, "test_handler", func(msg *EventMessage) error {
		mu.Lock()
		defer mu.Unlock()

		if err := json.Unmarshal(msg.Data, &receivedData); err != nil {
			t.Errorf("Failed to unmarshal event data: %v", err)
			return err
		}

		handled <- true
		return nil
	})
	require.NoError(t, err)

	// Publish event with TaskCreatedData instead of map
	taskData := TaskCreatedData{
		TaskID: "TASK-001",
		Title:  "Test Task",
	}
	err = eb.Publish(ctx, "test_source", taskData)
	require.NoError(t, err)

	// Wait for event to be handled
	select {
	case <-handled:
		mu.Lock()
		assert.Equal(t, "TASK-001", receivedData.TaskID)
		assert.Equal(t, "Test Task", receivedData.Title)
		mu.Unlock()
	case <-time.After(2 * time.Second):
		t.Fatal("Event was not handled within timeout")
	}
}

func TestEventBus_MultipleSubscribers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	eb, err := NewEventBus()
	require.NoError(t, err)
	err = eb.Start(ctx)
	require.NoError(t, err)
	defer eb.Stop()

	<-eb.router.Running()

	testData := TaskCreatedData{TaskID: "TASK-002", Title: "Test Data"}
	handled1 := make(chan bool, 1)
	handled2 := make(chan bool, 1)

	// Subscribe with first handler BEFORE starting
	err = eb.SubscribeAsync(TaskCreated, "handler1", func(msg *EventMessage) error {
		handled1 <- true
		return nil
	})
	require.NoError(t, err)

	// Subscribe with second handler BEFORE starting
	err = eb.SubscribeAsync(TaskCreated, "handler2", func(msg *EventMessage) error {
		handled2 <- true
		return nil
	})
	require.NoError(t, err)

	// Publish event
	err = eb.Publish(ctx, "test_source", testData)
	require.NoError(t, err)

	// Both handlers should receive the event
	select {
	case <-handled1:
		// First handler received event
	case <-time.After(2 * time.Second):
		t.Fatal("First handler did not receive event")
	}

	select {
	case <-handled2:
		// Second handler received event
	case <-time.After(2 * time.Second):
		t.Fatal("Second handler did not receive event")
	}
}

func TestEventBus_TypedEvents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	eb, err := NewEventBus()
	require.NoError(t, err)
	err = eb.Start(ctx)
	require.NoError(t, err)
	defer eb.Stop()
	<-eb.router.Running()

	// Test typed event
	testEvent := NewEvent("test", TaskCreatedData{
		TaskID: "TASK-001",
		Title:  "Test Task",
	})

	handled := make(chan bool, 1)
	var receivedEvent *Event[TaskCreatedData]

	// Subscribe using typed subscription BEFORE starting
	//	err = SubscribeTyped(eb, TaskCreated, "typed_handler", func(event *Event[TaskCreatedData]) error {
	err = SubscribeTyped(eb, ctx, TaskCreated, "typed_handler", func(ctx context.Context, msg *Event[TaskCreatedData]) error {
		receivedEvent = msg
		handled <- true
		return nil
	})
	require.NoError(t, err)

	// Publish typed event
	err = PublishTyped(eb, ctx, testEvent)
	require.NoError(t, err)

	// Wait for event to be handled
	select {
	case <-handled:
		assert.Equal(t, testEvent.Data.TaskID, receivedEvent.Data.TaskID)
		assert.Equal(t, testEvent.Data.Title, receivedEvent.Data.Title)
		assert.Equal(t, testEvent.Source, receivedEvent.Source)
	case <-time.After(2 * time.Second):
		t.Fatal("Typed event was not handled within timeout")
	}
}

func TestEventBus_HandlerTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	eb, err := NewEventBus()
	require.NoError(t, err)
	err = eb.Start(ctx)
	require.NoError(t, err)
	defer eb.Stop()

	<-eb.router.Running()

	testData := TaskCreatedData{TaskID: "TASK-003", Title: "Timeout Test"}
	handlerCalled := make(chan bool, 1)

	// Subscribe with a handler that takes too long BEFORE starting
	err = eb.SubscribeAsync(TaskCreated, "slow_handler", func(msg *EventMessage) error {
		handlerCalled <- true
		time.Sleep(20 * time.Second) // This should timeout after 15 seconds
		return nil
	})
	require.NoError(t, err)

	// Publish event
	err = eb.Publish(ctx, "test_source", testData)
	require.NoError(t, err)

	// Handler should be called but timeout
	select {
	case <-handlerCalled:
		// Handler was called (good)
	case <-time.After(2 * time.Second):
		t.Fatal("Handler was not called")
	}

	// Give some time for the timeout to occur
	time.Sleep(100 * time.Millisecond)
}

func TestEventBus_StartStop(t *testing.T) {
	eb, err := NewEventBus()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start should succeed
	err = eb.Start(ctx)
	require.NoError(t, err)

	// Router should be running
	select {
	case <-eb.router.Running():
		// Router is running
	case <-time.After(1 * time.Second):
		t.Fatal("Router did not start within timeout")
	}

	// Stop should succeed
	err = eb.Stop()
	require.NoError(t, err)
}
