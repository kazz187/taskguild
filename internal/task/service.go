package task

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// TaskService implements Service interface with business logic for task operations
type TaskService struct {
	repository Repository
}

// NewTaskService creates a new task service instance
func NewTaskService(repository Repository) *TaskService {
	return &TaskService{
		repository: repository,
	}
}

// CreateTask creates a new task with the given title and type
func (s *TaskService) CreateTask(title, taskType string) (*Task, error) {
	if title == "" {
		return nil, fmt.Errorf("task title cannot be empty")
	}

	if taskType == "" {
		taskType = "task"
	}

	// Generate task ID
	taskID, err := s.generateTaskID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate task ID: %w", err)
	}

	// Create worktree and branch names
	worktreePath := filepath.Join(".taskguild", "worktrees", taskID)
	branchName := fmt.Sprintf("%s/%s", taskType, strings.ToLower(strings.ReplaceAll(taskID, "-", "-")))

	now := time.Now()
	task := &Task{
		ID:             taskID,
		Title:          title,
		Type:           taskType,
		Status:         "CREATED",
		Worktree:       worktreePath,
		Branch:         branchName,
		AssignedAgents: []string{},
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := s.repository.Create(task); err != nil {
		return nil, fmt.Errorf("failed to create task: %w", err)
	}

	return task, nil
}

// GetTask retrieves a task by its ID
func (s *TaskService) GetTask(id string) (*Task, error) {
	if id == "" {
		return nil, fmt.Errorf("task ID cannot be empty")
	}

	task, err := s.repository.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	return task, nil
}

// ListTasks retrieves all tasks
func (s *TaskService) ListTasks() ([]*Task, error) {
	tasks, err := s.repository.GetAll()
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}

	return tasks, nil
}

// UpdateTaskStatus updates the status of a task
func (s *TaskService) UpdateTaskStatus(id, status string) error {
	if id == "" {
		return fmt.Errorf("task ID cannot be empty")
	}

	if status == "" {
		return fmt.Errorf("status cannot be empty")
	}

	task, err := s.repository.GetByID(id)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	task.Status = status
	task.UpdatedAt = time.Now()

	if err := s.repository.Update(task); err != nil {
		return fmt.Errorf("failed to update task: %w", err)
	}

	return nil
}

// CloseTask marks a task as closed
func (s *TaskService) CloseTask(id string) error {
	return s.UpdateTaskStatus(id, "CLOSED")
}

// AssignAgent assigns an agent to a task
func (s *TaskService) AssignAgent(taskID, agentID string) error {
	if taskID == "" {
		return fmt.Errorf("task ID cannot be empty")
	}

	if agentID == "" {
		return fmt.Errorf("agent ID cannot be empty")
	}

	task, err := s.repository.GetByID(taskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	// Check if agent is already assigned
	for _, assignedAgent := range task.AssignedAgents {
		if assignedAgent == agentID {
			return fmt.Errorf("agent %s is already assigned to task %s", agentID, taskID)
		}
	}

	task.AssignedAgents = append(task.AssignedAgents, agentID)
	task.UpdatedAt = time.Now()

	if err := s.repository.Update(task); err != nil {
		return fmt.Errorf("failed to update task: %w", err)
	}

	return nil
}

// UnassignAgent removes an agent from a task
func (s *TaskService) UnassignAgent(taskID, agentID string) error {
	if taskID == "" {
		return fmt.Errorf("task ID cannot be empty")
	}

	if agentID == "" {
		return fmt.Errorf("agent ID cannot be empty")
	}

	task, err := s.repository.GetByID(taskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	// Find and remove the agent
	for i, assignedAgent := range task.AssignedAgents {
		if assignedAgent == agentID {
			task.AssignedAgents = append(task.AssignedAgents[:i], task.AssignedAgents[i+1:]...)
			task.UpdatedAt = time.Now()

			if err := s.repository.Update(task); err != nil {
				return fmt.Errorf("failed to update task: %w", err)
			}

			return nil
		}
	}

	return fmt.Errorf("agent %s is not assigned to task %s", agentID, taskID)
}

// generateTaskID generates a unique task ID
func (s *TaskService) generateTaskID() (string, error) {
	tasks, err := s.repository.GetAll()
	if err != nil {
		return "", fmt.Errorf("failed to get existing tasks: %w", err)
	}

	// Find the highest task number
	maxNumber := 0
	for _, task := range tasks {
		if strings.HasPrefix(task.ID, "TASK-") {
			var number int
			if _, err := fmt.Sscanf(task.ID, "TASK-%d", &number); err == nil {
				if number > maxNumber {
					maxNumber = number
				}
			}
		}
	}

	return fmt.Sprintf("TASK-%03d", maxNumber+1), nil
}
