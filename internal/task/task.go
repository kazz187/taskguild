package task

import "time"

// Task represents a work unit in the TaskGuild system
type Task struct {
	ID          string                   `yaml:"id"`
	Title       string                   `yaml:"title"`
	Description string                   `yaml:"description"`
	Type        string                   `yaml:"type"`
	Worktree    string                   `yaml:"worktree"`
	Branch      string                   `yaml:"branch"`
	Processes   map[string]*ProcessState `yaml:"processes"`
	Metadata    map[string]string        `yaml:"metadata"`
	CreatedAt   time.Time                `yaml:"created_at"`
	UpdatedAt   time.Time                `yaml:"updated_at"`
}

// IsClosed returns true if all processes are completed
func (t *Task) IsClosed() bool {
	for _, state := range t.Processes {
		if state.Status != ProcessStatusCompleted {
			return false
		}
	}
	return true
}

// GetOverallStatus returns a summary status based on process states
func (t *Task) GetOverallStatus() string {
	hasInProgress := false
	hasPending := false
	hasRejected := false

	for _, state := range t.Processes {
		switch state.Status {
		case ProcessStatusInProgress:
			hasInProgress = true
		case ProcessStatusPending:
			hasPending = true
		case ProcessStatusRejected:
			hasRejected = true
		}
	}

	if t.IsClosed() {
		return "CLOSED"
	}
	if hasRejected {
		return "REJECTED"
	}
	if hasInProgress {
		return "IN_PROGRESS"
	}
	if hasPending {
		return "PENDING"
	}
	return "UNKNOWN"
}

// GetAssignedAgents returns all agents currently assigned to processes
func (t *Task) GetAssignedAgents() []string {
	agents := make(map[string]bool)
	for _, state := range t.Processes {
		if state.AssignedTo != "" {
			agents[state.AssignedTo] = true
		}
	}
	result := make([]string, 0, len(agents))
	for agent := range agents {
		result = append(result, agent)
	}
	return result
}

// Repository defines the interface for task persistence operations
type Repository interface {
	Create(task *Task) error
	GetByID(id string) (*Task, error)
	GetAll() ([]*Task, error)
	Update(task *Task) error
	Delete(id string) error
}

// Service defines the interface for task business logic operations
type Service interface {
	CreateTask(req *CreateTaskRequest) (*Task, error)
	GetTask(id string) (*Task, error)
	ListTasks() ([]*Task, error)
	UpdateTask(req *UpdateTaskRequest) (*Task, error)
	CloseTask(id string) (*Task, error)

	// Process-based operations
	// TryAcquireProcess atomically acquires a process using compare-and-swap semantics
	TryAcquireProcess(req *TryAcquireProcessRequest) (*Task, error)
	// CompleteProcess marks a process as completed and clears assignment
	CompleteProcess(taskID, processName, agentID string) error
	// RejectProcess marks a process as rejected and resets dependent processes
	RejectProcess(taskID, processName, agentID, reason string) error
	// GetAvailableProcesses returns processes that can be started
	GetAvailableProcesses(processName string) ([]*AvailableProcess, error)
	// WatchProcess creates a channel to receive process state changes
	WatchProcess(taskID, processName string) <-chan ProcessChangeEvent
	// UnwatchProcess removes a process watcher
	UnwatchProcess(taskID, processName string, ch <-chan ProcessChangeEvent)
	// GetTaskDefinition returns the task definition
	GetTaskDefinition() *TaskDefinition
}
