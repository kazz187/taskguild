package task

import "time"

// Task represents a work unit in the TaskGuild system
type Task struct {
	ID              string    `yaml:"id"`
	Title           string    `yaml:"title"`
	Type            string    `yaml:"type"`
	Status          string    `yaml:"status"`
	Worktree        string    `yaml:"worktree"`
	Branch          string    `yaml:"branch"`
	AssignedAgents  []string  `yaml:"assigned_agents"`
	CreatedAt       time.Time `yaml:"created_at"`
	UpdatedAt       time.Time `yaml:"updated_at"`
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
	CreateTask(title, taskType string) (*Task, error)
	GetTask(id string) (*Task, error)
	ListTasks() ([]*Task, error)
	UpdateTaskStatus(id, status string) error
	CloseTask(id string) error
	AssignAgent(taskID, agentID string) error
	UnassignAgent(taskID, agentID string) error
}