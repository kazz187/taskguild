package event

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// EventLogger logs events to files
type EventLogger struct {
	logDir string
	mu     sync.Mutex
}

// NewEventLogger creates a new event logger
func NewEventLogger(logDir string) (*EventLogger, error) {
	// Create log directory if it doesn't exist
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	return &EventLogger{
		logDir: logDir,
	}, nil
}

// LogEvent logs an event message to a file
func (el *EventLogger) LogEvent(ctx context.Context, eventMsg *EventMessage) error {
	el.mu.Lock()
	defer el.mu.Unlock()

	// Create log entry
	logEntry := struct {
		*EventMessage
		LoggedAt string `json:"logged_at"`
	}{
		EventMessage: eventMsg,
		LoggedAt:     time.Now().Format(time.RFC3339),
	}

	// Marshal to JSON (compact format for NDJSON)
	data, err := json.Marshal(logEntry)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Get log file path
	logFile := el.getLogFilePath(eventMsg.Timestamp)

	// Open or create log file
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	// Write event to file
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("failed to write event to log: %w", err)
	}

	// Add newline separator
	if _, err := file.WriteString("\n"); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	return nil
}

// getLogFilePath returns the log file path for a given timestamp
func (el *EventLogger) getLogFilePath(timestamp time.Time) string {
	// Use daily log files in NDJSON format
	filename := fmt.Sprintf("events_%s.ndjson", timestamp.Format("2006-01-02"))
	return filepath.Join(el.logDir, filename)
}

// RegisterEventLogger registers the event logger with the event bus
func RegisterEventLogger(eventBus *EventBus, logger *EventLogger) {
	// Log all event types
	allEventTypes := []EventType{
		TaskCreated, TaskStatusChanged, TaskClosed, TaskAssigned, TaskUnassigned,
		AgentStarted, AgentStopped, AgentStatusChanged, AgentAssigned, AgentUnassigned,
		ApprovalRequested, ApprovalGranted, ApprovalRejected,
		GitCommitted, GitPushed, GitMerged,
	}

	for _, eventType := range allEventTypes {
		if err := eventBus.SubscribeAsync(eventType, fmt.Sprintf("logger-%s", eventType), func(eventMsg *EventMessage) error {
			ctx := context.Background()
			if err := logger.LogEvent(ctx, eventMsg); err != nil {
				log.Printf("Failed to log event %s: %v", eventMsg.ID, err)
			}
			return nil
		}); err != nil {
			log.Printf("Failed to subscribe to event %s: %v", eventType, err)
			continue
		}
	}
}

// EventLogReader reads events from log files
type EventLogReader struct {
	logDir string
}

// NewEventLogReader creates a new event log reader
func NewEventLogReader(logDir string) *EventLogReader {
	return &EventLogReader{
		logDir: logDir,
	}
}

// ReadEvents reads events from a specific date
func (elr *EventLogReader) ReadEvents(date time.Time) ([]*EventMessage, error) {
	logFile := filepath.Join(elr.logDir, fmt.Sprintf("events_%s.ndjson", date.Format("2006-01-02")))

	data, err := os.ReadFile(logFile)
	if err != nil {
		if os.IsNotExist(err) {
			return []*EventMessage{}, nil
		}
		return nil, fmt.Errorf("failed to read log file: %w", err)
	}

	// Split by newlines and parse each event
	var events []*EventMessage
	lines := strings.Split(string(data), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var logEntry struct {
			*EventMessage
			LoggedAt string `json:"logged_at"`
		}

		if err := json.Unmarshal([]byte(line), &logEntry); err != nil {
			log.Printf("Failed to unmarshal event: %v", err)
			continue
		}

		events = append(events, logEntry.EventMessage)
	}

	return events, nil
}

// ReadEventsByType reads events of a specific type
func (elr *EventLogReader) ReadEventsByType(date time.Time, eventType EventType) ([]*EventMessage, error) {
	allEvents, err := elr.ReadEvents(date)
	if err != nil {
		return nil, err
	}

	var filteredEvents []*EventMessage
	for _, event := range allEvents {
		if event.Type == eventType {
			filteredEvents = append(filteredEvents, event)
		}
	}

	return filteredEvents, nil
}
