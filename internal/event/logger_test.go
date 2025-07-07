package event

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEventLogger_LogEvent(t *testing.T) {
	// Create temporary directory for logs
	tmpDir := t.TempDir()

	logger, err := NewEventLogger(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create event logger: %v", err)
	}

	// Create an event
	event := NewEvent("test", TaskCreatedData{
		TaskID: "TASK-001",
		Title:  "Test Task",
	})
	eventMsg, err := event.ToMessage()
	if err != nil {
		t.Fatalf("Failed to convert event to message: %v", err)
	}

	// Log the event
	ctx := context.Background()
	err = logger.LogEvent(ctx, eventMsg)
	if err != nil {
		t.Fatalf("Failed to log event: %v", err)
	}

	// Verify log file exists
	logFile := filepath.Join(tmpDir, "events_"+eventMsg.Timestamp.Format("2006-01-02")+".ndjson")
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Error("Log file was not created")
	}

	// Read and verify log content
	reader := NewEventLogReader(tmpDir)
	events, err := reader.ReadEvents(eventMsg.Timestamp)
	if err != nil {
		t.Fatalf("Failed to read events: %v", err)
	}

	if len(events) != 1 {
		t.Errorf("Expected 1 event, got %d", len(events))
	}

	if events[0].ID != eventMsg.ID {
		t.Errorf("Expected event ID %s, got %s", eventMsg.ID, events[0].ID)
	}
}

func TestEventLogger_MultipleEvents(t *testing.T) {
	// Create temporary directory for logs
	tmpDir := t.TempDir()

	logger, err := NewEventLogger(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create event logger: %v", err)
	}

	ctx := context.Background()
	now := time.Now()

	// Create events with proper data structures
	event1 := NewEvent("test", TaskCreatedData{TaskID: "TASK-001"})
	eventMsg1, _ := event1.ToMessage()
	eventMsg1.Timestamp = now

	event2 := NewEvent("test", TaskStatusChangedData{
		TaskID:     "TASK-001",
		FromStatus: "CREATED",
		ToStatus:   "IN_PROGRESS",
	})
	eventMsg2, _ := event2.ToMessage()
	eventMsg2.Timestamp = now

	event3 := NewEvent("test", TaskClosedData{TaskID: "TASK-001"})
	eventMsg3, _ := event3.ToMessage()
	eventMsg3.Timestamp = now

	events := []*EventMessage{eventMsg1, eventMsg2, eventMsg3}

	for _, eventMsg := range events {
		if err := logger.LogEvent(ctx, eventMsg); err != nil {
			t.Fatalf("Failed to log event: %v", err)
		}
	}

	// Read events back
	reader := NewEventLogReader(tmpDir)
	readEvents, err := reader.ReadEvents(now)
	if err != nil {
		t.Fatalf("Failed to read events: %v", err)
	}

	if len(readEvents) != 3 {
		t.Errorf("Expected 3 events, got %d", len(readEvents))
	}
}

func TestEventLogReader_ReadEventsByType(t *testing.T) {
	// Create temporary directory for logs
	tmpDir := t.TempDir()

	logger, err := NewEventLogger(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create event logger: %v", err)
	}

	ctx := context.Background()
	now := time.Now()

	// Log events of different types with proper data structures
	eventDataList := []struct {
		data      interface{}
		eventType EventType
	}{
		{TaskCreatedData{TaskID: "TASK-001"}, TaskCreated},
		{TaskCreatedData{TaskID: "TASK-002"}, TaskCreated},
		{TaskStatusChangedData{TaskID: "TASK-001", FromStatus: "CREATED", ToStatus: "IN_PROGRESS"}, TaskStatusChanged},
		{TaskClosedData{TaskID: "TASK-002"}, TaskClosed},
	}

	for _, item := range eventDataList {
		eventMsg := &EventMessage{
			ID:        generateEventID(),
			Type:      item.eventType,
			Timestamp: now,
			Source:    "test",
		}
		rawData, _ := json.Marshal(item.data)
		eventMsg.Data = rawData

		if err := logger.LogEvent(ctx, eventMsg); err != nil {
			t.Fatalf("Failed to log event: %v", err)
		}
	}

	// Read only TaskCreated events
	reader := NewEventLogReader(tmpDir)
	taskCreatedEvents, err := reader.ReadEventsByType(now, TaskCreated)
	if err != nil {
		t.Fatalf("Failed to read events by type: %v", err)
	}

	if len(taskCreatedEvents) != 2 {
		t.Errorf("Expected 2 TaskCreated events, got %d", len(taskCreatedEvents))
	}

	for _, event := range taskCreatedEvents {
		if event.Type != TaskCreated {
			t.Errorf("Expected event type %s, got %s", TaskCreated, event.Type)
		}
	}
}

func TestEventLogger_ConcurrentLogging(t *testing.T) {
	// Create temporary directory for logs
	tmpDir := t.TempDir()

	logger, err := NewEventLogger(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create event logger: %v", err)
	}

	ctx := context.Background()
	now := time.Now()

	// Log events concurrently
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(i int) {
			event := NewEvent("test", TaskCreatedData{
				TaskID: "TASK-" + string(rune('A'+i)),
			})
			eventMsg, err := event.ToMessage()
			if err != nil {
				t.Errorf("Failed to convert event to message: %v", err)
				done <- true
				return
			}
			eventMsg.Timestamp = now
			if err := logger.LogEvent(ctx, eventMsg); err != nil {
				t.Errorf("Failed to log event: %v", err)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Read events back
	reader := NewEventLogReader(tmpDir)
	events, err := reader.ReadEvents(now)
	if err != nil {
		t.Fatalf("Failed to read events: %v", err)
	}

	if len(events) != 10 {
		t.Errorf("Expected 10 events, got %d", len(events))
	}
}
