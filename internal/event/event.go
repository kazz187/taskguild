package event

import (
	"encoding/json"
	"reflect"
	"strings"
	"time"
)

// EventType represents the type of event
type EventType string

const (
	// Task events
	TaskCreated       EventType = "task.created"
	TaskStatusChanged EventType = "task.status_changed"
	TaskClosed        EventType = "task.closed"
	TaskAssigned      EventType = "task.assigned"
	TaskUnassigned    EventType = "task.unassigned"

	// Agent events
	AgentStarted       EventType = "agent.started"
	AgentStopped       EventType = "agent.stopped"
	AgentStatusChanged EventType = "agent.status_changed"
	AgentAssigned      EventType = "agent.assigned"
	AgentUnassigned    EventType = "agent.unassigned"

	// Approval events
	ApprovalRequested EventType = "approval.requested"
	ApprovalGranted   EventType = "approval.granted"
	ApprovalRejected  EventType = "approval.rejected"

	// Git events
	GitCommitted EventType = "git.committed"
	GitPushed    EventType = "git.pushed"
	GitMerged    EventType = "git.merged"
)

// Event represents a typed system event
type Event[T any] struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`
	Data      T         `json:"data"`
}

// EventMessage represents a serialized event for transport
type EventMessage struct {
	ID        string          `json:"id"`
	Type      EventType       `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Source    string          `json:"source"`
	Data      json.RawMessage `json:"data"`
}

// NewEvent creates a new typed event
func NewEvent[T any](source string, data T) *Event[T] {
	return &Event[T]{
		ID:        generateEventID(),
		Timestamp: time.Now(),
		Source:    source,
		Data:      data,
	}
}

// ToMessage converts a typed event to a transport message
func (e *Event[T]) ToMessage() (*EventMessage, error) {
	rawData, err := json.Marshal(e.Data)
	if err != nil {
		return nil, err
	}

	eventType := inferEventType(e.Data)
	return &EventMessage{
		ID:        e.ID,
		Type:      eventType,
		Timestamp: e.Timestamp,
		Source:    e.Source,
		Data:      rawData,
	}, nil
}

// FromMessage converts a transport message to a typed event
func FromMessage[T any](msg *EventMessage) (*Event[T], error) {
	var data T
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		return nil, err
	}

	return &Event[T]{
		ID:        msg.ID,
		Timestamp: msg.Timestamp,
		Source:    msg.Source,
		Data:      data,
	}, nil
}

// inferEventType infers EventType from data type
func inferEventType(data any) EventType {
	dataType := reflect.TypeOf(data)

	// Handle pointer types
	if dataType.Kind() == reflect.Ptr {
		dataType = dataType.Elem()
	}

	typeName := dataType.Name()

	// Convert from Go type name to event type
	switch typeName {
	case "TaskCreatedData":
		return TaskCreated
	case "TaskStatusChangedData":
		return TaskStatusChanged
	case "TaskClosedData":
		return TaskClosed
	case "AgentStatusChangedData":
		return AgentStatusChanged
	case "ApprovalRequestData":
		return ApprovalRequested
	default:
		// Fallback: convert CamelCase to snake_case
		return EventType(camelToSnake(typeName))
	}
}

// camelToSnake converts CamelCase to snake_case
func camelToSnake(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteByte('.')
		}
		result.WriteRune(r + ('a' - 'A'))
	}
	return strings.ToLower(result.String())
}

// generateEventID generates a unique event ID
func generateEventID() string {
	return time.Now().Format("20060102150405") + "-" + generateRandomString(8)
}

// generateRandomString generates a random string of specified length
func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
	}
	return string(b)
}

// TaskCreatedData represents data for task created event
type TaskCreatedData struct {
	TaskID      string `json:"task_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Type        string `json:"type"`
}

// TaskStatusChangedData represents data for task status changed event
type TaskStatusChangedData struct {
	TaskID     string `json:"task_id"`
	FromStatus string `json:"from_status"`
	ToStatus   string `json:"to_status"`
}

// TaskClosedData represents data for task closed event
type TaskClosedData struct {
	TaskID string `json:"task_id"`
}

// AgentStatusChangedData represents data for agent status changed event
type AgentStatusChangedData struct {
	AgentID    string `json:"agent_id"`
	Role       string `json:"role"`
	FromStatus string `json:"from_status"`
	ToStatus   string `json:"to_status"`
}

// ApprovalRequestData represents data for approval request event
type ApprovalRequestData struct {
	RequestID   string                 `json:"request_id"`
	AgentID     string                 `json:"agent_id"`
	TaskID      string                 `json:"task_id"`
	Action      string                 `json:"action"`
	Description string                 `json:"description"`
	Details     map[string]interface{} `json:"details"`
}
