package task

import "time"

// Task represents a work unit in the TaskGuild system
type Task struct {
	ID             string            `yaml:"id"`
	Title          string            `yaml:"title"`
	Description    string            `yaml:"description"`
	Type           string            `yaml:"type"`
	Status         string            `yaml:"status"`
	Worktree       string            `yaml:"worktree"`
	Branch         string            `yaml:"branch"`
	AssignedTo     string            `yaml:"assigned_to"`
	AssignedAgents []string          `yaml:"assigned_agents"`
	Metadata       map[string]string `yaml:"metadata"`
	CreatedAt      time.Time         `yaml:"created_at"`
	UpdatedAt      time.Time         `yaml:"updated_at"`
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
	// TryAcquireTask atomically acquires a task using compare-and-swap semantics
	TryAcquireTask(req *TryAcquireTaskRequest) (*Task, error)
	// ReleaseTask clears the task assignment (for task completion or error)
	ReleaseTask(taskID, agentID string) error
}
