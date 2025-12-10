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
	repository      Repository
	eventBus        *event.EventBus
	taskDefinition  *TaskDefinition
	processEventBus *ProcessEventBus
}

// NewService creates a new task service instance with event bus
func NewService(repository Repository, eventBus *event.EventBus) Service {
	// Load task definition
	taskDef, err := LoadTaskDefinition("")
	if err != nil {
		// Use default if loading fails
		taskDef = DefaultTaskDefinition()
	}

	return &ServiceImpl{
		repository:      repository,
		eventBus:        eventBus,
		taskDefinition:  taskDef,
		processEventBus: NewProcessEventBus(),
	}
}

// GetTaskDefinition returns the task definition
func (s *ServiceImpl) GetTaskDefinition() *TaskDefinition {
	return s.taskDefinition
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
		ID:          taskID,
		Title:       req.Title,
		Description: req.Description,
		Type:        taskType,
		Worktree:    worktreePath,
		Branch:      branchName,
		Processes:   s.taskDefinition.CreateInitialProcessStates(),
		Metadata:    req.Metadata,
		CreatedAt:   now,
		UpdatedAt:   now,
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

	task.Update(req)

	if err := s.repository.Update(task); err != nil {
		return nil, fmt.Errorf("failed to update task: %w", err)
	}

	return task, nil
}

// CloseTask marks all processes as completed
func (s *ServiceImpl) CloseTask(id string) (*Task, error) {
	task, err := s.repository.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("failed to get task %s: %w", id, err)
	}

	// Mark all processes as completed
	for name, state := range task.Processes {
		oldStatus := state.Status
		state.Status = ProcessStatusCompleted
		state.AssignedTo = ""

		// Notify watchers
		s.processEventBus.Notify(ProcessChangeEvent{
			TaskID:      id,
			ProcessName: name,
			OldStatus:   oldStatus,
			NewStatus:   ProcessStatusCompleted,
			ChangedBy:   "system",
		})
	}

	task.UpdatedAt = time.Now()

	if err := s.repository.Update(task); err != nil {
		return nil, fmt.Errorf("failed to close task: %w", err)
	}

	return task, nil
}

// TryAcquireProcess atomically acquires a process using compare-and-swap semantics
func (s *ServiceImpl) TryAcquireProcess(req *TryAcquireProcessRequest) (*Task, error) {
	if req.TaskID == "" {
		return nil, fmt.Errorf("task ID cannot be empty")
	}
	if req.ProcessName == "" {
		return nil, fmt.Errorf("process name cannot be empty")
	}
	if req.AgentID == "" {
		return nil, fmt.Errorf("agent ID cannot be empty")
	}

	task, err := s.repository.GetByID(req.TaskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get task %s: %w", req.TaskID, err)
	}

	state, ok := task.Processes[req.ProcessName]
	if !ok {
		return nil, fmt.Errorf("process %s not found in task %s", req.ProcessName, req.TaskID)
	}

	// Check if process can be started (dependencies are met)
	if !s.taskDefinition.CanStart(req.ProcessName, task.Processes) {
		return nil, fmt.Errorf("process %s cannot be started: dependencies not met or already in progress", req.ProcessName)
	}

	// Check if already assigned to another agent
	if state.AssignedTo != "" && state.AssignedTo != req.AgentID {
		return nil, fmt.Errorf("process %s is already assigned to agent %s", req.ProcessName, state.AssignedTo)
	}

	oldStatus := state.Status

	// Atomically update status and assign to agent
	state.Status = ProcessStatusInProgress
	state.AssignedTo = req.AgentID
	task.UpdatedAt = time.Now()

	if err := s.repository.Update(task); err != nil {
		return nil, fmt.Errorf("failed to acquire process: %w", err)
	}

	// Notify watchers
	s.processEventBus.Notify(ProcessChangeEvent{
		TaskID:      req.TaskID,
		ProcessName: req.ProcessName,
		OldStatus:   oldStatus,
		NewStatus:   ProcessStatusInProgress,
		ChangedBy:   req.AgentID,
	})

	// Publish event
	if s.eventBus != nil {
		eventData := &event.TaskStatusChangedData{
			TaskID:    task.ID,
			OldStatus: string(oldStatus),
			NewStatus: string(ProcessStatusInProgress),
			ChangedBy: req.AgentID,
			ChangedAt: time.Now(),
			Reason:    fmt.Sprintf("Process %s acquired by agent %s", req.ProcessName, req.AgentID),
		}
		s.eventBus.Publish(context.Background(), "task-service", eventData)
	}

	return task, nil
}

// CompleteProcess marks a process as completed and clears assignment
func (s *ServiceImpl) CompleteProcess(taskID, processName, agentID string) error {
	if taskID == "" {
		return fmt.Errorf("task ID cannot be empty")
	}
	if processName == "" {
		return fmt.Errorf("process name cannot be empty")
	}
	if agentID == "" {
		return fmt.Errorf("agent ID cannot be empty")
	}

	task, err := s.repository.GetByID(taskID)
	if err != nil {
		return fmt.Errorf("failed to get task %s: %w", taskID, err)
	}

	state, ok := task.Processes[processName]
	if !ok {
		return fmt.Errorf("process %s not found in task %s", processName, taskID)
	}

	// Only allow the assigned agent to complete the process
	if state.AssignedTo != agentID {
		return fmt.Errorf("process %s is not assigned to agent %s (assigned to: %s)",
			processName, agentID, state.AssignedTo)
	}

	oldStatus := state.Status

	// Clear assignment and mark as completed
	state.Status = ProcessStatusCompleted
	state.AssignedTo = ""
	task.UpdatedAt = time.Now()

	if err := s.repository.Update(task); err != nil {
		return fmt.Errorf("failed to complete process: %w", err)
	}

	// Notify watchers
	s.processEventBus.Notify(ProcessChangeEvent{
		TaskID:      taskID,
		ProcessName: processName,
		OldStatus:   oldStatus,
		NewStatus:   ProcessStatusCompleted,
		ChangedBy:   agentID,
	})

	// Publish event
	if s.eventBus != nil {
		eventData := &event.TaskStatusChangedData{
			TaskID:    task.ID,
			OldStatus: string(oldStatus),
			NewStatus: string(ProcessStatusCompleted),
			ChangedBy: agentID,
			ChangedAt: time.Now(),
			Reason:    fmt.Sprintf("Process %s completed by agent %s", processName, agentID),
		}
		s.eventBus.Publish(context.Background(), "task-service", eventData)
	}

	return nil
}

// RejectProcess marks a process as rejected and resets dependent processes
func (s *ServiceImpl) RejectProcess(taskID, processName, agentID, reason string) error {
	if taskID == "" {
		return fmt.Errorf("task ID cannot be empty")
	}
	if processName == "" {
		return fmt.Errorf("process name cannot be empty")
	}
	if agentID == "" {
		return fmt.Errorf("agent ID cannot be empty")
	}

	task, err := s.repository.GetByID(taskID)
	if err != nil {
		return fmt.Errorf("failed to get task %s: %w", taskID, err)
	}

	state, ok := task.Processes[processName]
	if !ok {
		return fmt.Errorf("process %s not found in task %s", processName, taskID)
	}

	// Only allow the assigned agent to reject the process
	if state.AssignedTo != agentID {
		return fmt.Errorf("process %s is not assigned to agent %s (assigned to: %s)",
			processName, agentID, state.AssignedTo)
	}

	// Get the process definition to find dependencies
	processDef, ok := s.taskDefinition.GetProcess(processName)
	if !ok {
		return fmt.Errorf("process definition %s not found", processName)
	}

	// Reset the dependency processes (the ones this process depends on)
	processesToReset := make([]string, 0)
	for _, dep := range processDef.DependsOn {
		processesToReset = append(processesToReset, dep)
	}

	// Also reset all processes that depend on the ones being reset (cascading reset)
	for _, dep := range processDef.DependsOn {
		allDependents := s.taskDefinition.GetAllDependents(dep)
		processesToReset = append(processesToReset, allDependents...)
	}

	// Reset the current process
	oldStatus := state.Status
	state.Status = ProcessStatusPending
	state.AssignedTo = ""

	// Notify watchers for current process
	s.processEventBus.Notify(ProcessChangeEvent{
		TaskID:      taskID,
		ProcessName: processName,
		OldStatus:   oldStatus,
		NewStatus:   ProcessStatusPending,
		ChangedBy:   agentID,
	})

	// Reset dependency processes and their dependents
	for _, procName := range processesToReset {
		if procState, ok := task.Processes[procName]; ok {
			procOldStatus := procState.Status
			procState.Status = ProcessStatusPending
			procState.AssignedTo = ""

			// Notify watchers
			s.processEventBus.Notify(ProcessChangeEvent{
				TaskID:      taskID,
				ProcessName: procName,
				OldStatus:   procOldStatus,
				NewStatus:   ProcessStatusPending,
				ChangedBy:   agentID,
			})
		}
	}

	task.UpdatedAt = time.Now()

	if err := s.repository.Update(task); err != nil {
		return fmt.Errorf("failed to reject process: %w", err)
	}

	// Publish event
	if s.eventBus != nil {
		eventData := &event.TaskStatusChangedData{
			TaskID:    task.ID,
			OldStatus: string(oldStatus),
			NewStatus: string(ProcessStatusPending),
			ChangedBy: agentID,
			ChangedAt: time.Now(),
			Reason:    fmt.Sprintf("Process %s rejected by agent %s: %s", processName, agentID, reason),
		}
		s.eventBus.Publish(context.Background(), "task-service", eventData)
	}

	return nil
}

// GetAvailableProcesses returns processes that can be started for a given process name
func (s *ServiceImpl) GetAvailableProcesses(processName string) ([]*AvailableProcess, error) {
	tasks, err := s.repository.GetAll()
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}

	var available []*AvailableProcess
	for _, task := range tasks {
		// Skip closed tasks
		if task.IsClosed() {
			continue
		}

		// Check if the requested process can be started
		if s.taskDefinition.CanStart(processName, task.Processes) {
			available = append(available, &AvailableProcess{
				TaskID:      task.ID,
				ProcessName: processName,
				Task:        task,
			})
		}
	}

	return available, nil
}

// WatchProcess creates a channel to receive process state changes
func (s *ServiceImpl) WatchProcess(taskID, processName string) <-chan ProcessChangeEvent {
	return s.processEventBus.Watch(taskID, processName)
}

// UnwatchProcess removes a process watcher
func (s *ServiceImpl) UnwatchProcess(taskID, processName string, ch <-chan ProcessChangeEvent) {
	s.processEventBus.Unwatch(taskID, processName, ch)
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
