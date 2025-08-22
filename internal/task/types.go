package task

import "time"

// CreateTaskRequest represents a request to create a new task
type CreateTaskRequest struct {
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Type        string            `json:"type"`
	Metadata    map[string]string `json:"metadata"`
}

// UpdateTaskRequest represents a request to update a task
type UpdateTaskRequest struct {
	ID          string            `json:"id"`
	Status      Status            `json:"status"`
	Description string            `json:"description"`
	Metadata    map[string]string `json:"metadata"`
}

// TryAcquireTaskRequest represents a request to atomically acquire a task
// using compare-and-swap semantics
type TryAcquireTaskRequest struct {
	ID             string `json:"id"`
	ExpectedStatus Status `json:"expected_status"`
	NewStatus      Status `json:"new_status"`
	AgentID        string `json:"agent_id"`
}

// Status represents task status
type Status string

const (
	StatusCreated     Status = "CREATED"
	StatusAnalyzing   Status = "ANALYZING"
	StatusDesigned    Status = "DESIGNED"
	StatusInProgress  Status = "IN_PROGRESS"
	StatusReviewReady Status = "REVIEW_READY"
	StatusQAReady     Status = "QA_READY"
	StatusClosed      Status = "CLOSED"
	StatusCancelled   Status = "CANCELLED"
)

// UpdateTaskFields updates task with new values
func (t *Task) Update(req *UpdateTaskRequest) {
	if req.Status != "" {
		t.Status = string(req.Status)
	}
	if req.Description != "" {
		t.Description = req.Description
	}
	if req.Metadata != nil {
		if t.Metadata == nil {
			t.Metadata = make(map[string]string)
		}
		for k, v := range req.Metadata {
			t.Metadata[k] = v
		}
	}
	t.UpdatedAt = time.Now()
}
