package task

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/kazz187/taskguild/internal/event"
)

// ServiceImpl implements the Service interface
type ServiceImpl struct {
	repository Repository
	eventBus   *event.EventBus
}

// NewService creates a new task service instance with event bus
func NewService(repository Repository, eventBus *event.EventBus) Service {
	return &ServiceImpl{
		repository: repository,
		eventBus:   eventBus,
	}
}

// CreateTask creates a new task with the given request
func (s *ServiceImpl) CreateTask(req *CreateTaskRequest) (*Task, error) {
	if req.Title == "" {
		return nil, fmt.Errorf("task title cannot be empty")
	}

	taskType := req.Type
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
		Title:          req.Title,
		Description:    req.Description,
		Type:           taskType,
		Status:         string(StatusCreated),
		Worktree:       worktreePath,
		Branch:         branchName,
		AssignedAgents: []string{},
		Metadata:       req.Metadata,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := s.repository.Create(task); err != nil {
		return nil, fmt.Errorf("failed to create task: %w", err)
	}

	// Publish event
	if s.eventBus != nil {
		eventData := &event.TaskCreatedData{
			TaskID:      task.ID,
			Title:       task.Title,
			Description: task.Description,
			Type:        task.Type,
		}
		if err := s.eventBus.Publish(context.Background(), "task-service", eventData); err != nil {
			return nil, fmt.Errorf("failed to publish task created event: %w", err)
		}
	}

	return task, nil
}

// GetTask retrieves a task by ID
func (s *ServiceImpl) GetTask(id string) (*Task, error) {
	if id == "" {
		return nil, fmt.Errorf("task ID cannot be empty")
	}

	task, err := s.repository.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("failed to get task %s: %w", id, err)
	}

	return task, nil
}

// ListTasks returns all tasks
func (s *ServiceImpl) ListTasks() ([]*Task, error) {
	tasks, err := s.repository.GetAll()
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}

	return tasks, nil
}

// UpdateTask updates a task with the given request
func (s *ServiceImpl) UpdateTask(req *UpdateTaskRequest) (*Task, error) {
	if req.ID == "" {
		return nil, fmt.Errorf("task ID cannot be empty")
	}

	task, err := s.repository.GetByID(req.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get task %s: %w", req.ID, err)
	}

	oldStatus := task.Status
	task.Update(req)

	if err := s.repository.Update(task); err != nil {
		return nil, fmt.Errorf("failed to update task: %w", err)
	}

	// Publish event if status changed
	if s.eventBus != nil && oldStatus != task.Status {
		eventData := &event.TaskStatusChangedData{
			TaskID:    task.ID,
			OldStatus: oldStatus,
			NewStatus: task.Status,
			ChangedBy: "system",
			ChangedAt: time.Now(),
			Reason:    "Task status updated",
		}
		s.eventBus.Publish(context.Background(), "task-service", eventData)
	}

	return task, nil
}

// CloseTask closes a task
func (s *ServiceImpl) CloseTask(id string) (*Task, error) {
	req := &UpdateTaskRequest{
		ID:     id,
		Status: StatusClosed,
	}
	return s.UpdateTask(req)
}

// generateTaskID generates a unique task ID
func (s *ServiceImpl) generateTaskID() (string, error) {
	tasks, err := s.repository.GetAll()
	if err != nil {
		return "", err
	}

	maxID := 0
	for _, task := range tasks {
		if strings.HasPrefix(task.ID, "TASK-") {
			var id int
			if _, err := fmt.Sscanf(task.ID, "TASK-%d", &id); err == nil && id > maxID {
				maxID = id
			}
		}
	}

	return fmt.Sprintf("TASK-%03d", maxID+1), nil
}
