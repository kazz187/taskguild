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
	Description string            `json:"description"`
	Metadata    map[string]string `json:"metadata"`
}

// TryAcquireProcessRequest represents a request to atomically acquire a process
// using compare-and-swap semantics
type TryAcquireProcessRequest struct {
	TaskID      string `json:"task_id"`
	ProcessName string `json:"process_name"`
	AgentID     string `json:"agent_id"`
}

// AvailableProcess represents a process that is available for execution
type AvailableProcess struct {
	TaskID      string `json:"task_id"`
	ProcessName string `json:"process_name"`
	Task        *Task  `json:"task"`
}

// UpdateTaskFields updates task with new values
func (t *Task) Update(req *UpdateTaskRequest) {
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
