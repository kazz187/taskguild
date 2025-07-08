package agent

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Status string

const (
	StatusIdle    Status = "IDLE"
	StatusBusy    Status = "BUSY"
	StatusWaiting Status = "WAITING"
	StatusError   Status = "ERROR"
	StatusStopped Status = "STOPPED"
)

type Action string

const (
	ActionFileWrite    Action = "file_write"
	ActionFileDelete   Action = "file_delete"
	ActionGitCommit    Action = "git_commit"
	ActionGitPush      Action = "git_push"
	ActionStatusChange Action = "status_change"
	ActionTaskCreate   Action = "task_create"
)

type EventTrigger struct {
	Event     string `yaml:"event"`
	Condition string `yaml:"condition"`
}

type ApprovalRule struct {
	Action    Action `yaml:"action"`
	Pattern   string `yaml:"pattern,omitempty"`
	Condition string `yaml:"condition,omitempty"`
}

type ScalingConfig struct {
	Min  int  `yaml:"min"`
	Max  int  `yaml:"max"`
	Auto bool `yaml:"auto"`
}

type Agent struct {
	ID               string         `yaml:"id"`
	Role             string         `yaml:"role"`
	Type             string         `yaml:"type"`
	MemoryPath       string         `yaml:"memory"`
	Triggers         []EventTrigger `yaml:"triggers"`
	ApprovalRequired []ApprovalRule `yaml:"approval_required"`
	Scaling          *ScalingConfig `yaml:"scaling,omitempty"`
	Status           Status         `yaml:"status"`
	TaskID           string         `yaml:"task_id,omitempty"`
	WorktreePath     string         `yaml:"worktree_path,omitempty"`
	CreatedAt        time.Time      `yaml:"created_at"`
	UpdatedAt        time.Time      `yaml:"updated_at"`

	// Runtime fields
	ctx    context.Context
	cancel context.CancelFunc
	mutex  sync.RWMutex
}

func NewAgent(role, agentType, memoryPath string) *Agent {
	now := time.Now()
	return &Agent{
		ID:         generateAgentID(role),
		Role:       role,
		Type:       agentType,
		MemoryPath: memoryPath,
		Status:     StatusIdle,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func (a *Agent) UpdateStatus(status Status) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.Status = status
	a.UpdatedAt = time.Now()
}

func (a *Agent) GetStatus() Status {
	a.mutex.RLock()
	defer a.mutex.RUnlock()
	return a.Status
}

func (a *Agent) AssignTask(taskID, worktreePath string) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.TaskID = taskID
	a.WorktreePath = worktreePath
	a.UpdatedAt = time.Now()
}

func (a *Agent) ClearTask() {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.TaskID = ""
	a.WorktreePath = ""
	a.UpdatedAt = time.Now()
}

func (a *Agent) IsAvailable() bool {
	a.mutex.RLock()
	defer a.mutex.RUnlock()
	return a.Status == StatusIdle
}

func (a *Agent) IsAssigned() bool {
	a.mutex.RLock()
	defer a.mutex.RUnlock()
	return a.TaskID != ""
}

func (a *Agent) RequiresApproval(action Action, target string) bool {
	for _, rule := range a.ApprovalRequired {
		if rule.Action == action {
			if rule.Pattern != "" {
				// TODO: Pattern matching logic
				return true
			}
			if rule.Condition != "" {
				// TODO: Condition evaluation logic
				return true
			}
			return true
		}
	}
	return false
}

func (a *Agent) MatchesTrigger(eventName string, context map[string]interface{}) bool {
	for _, trigger := range a.Triggers {
		if trigger.Event == eventName {
			if trigger.Condition != "" {
				// TODO: Condition evaluation logic
				return true
			}
			return true
		}
	}
	return false
}

func (a *Agent) Start(ctx context.Context) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if a.ctx != nil {
		return fmt.Errorf("agent %s is already running", a.ID)
	}

	a.ctx, a.cancel = context.WithCancel(ctx)
	a.Status = StatusIdle
	a.UpdatedAt = time.Now()

	// Start agent goroutine
	go a.run()

	return nil
}

func (a *Agent) Stop() error {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
		a.ctx = nil
	}

	a.Status = StatusStopped
	a.UpdatedAt = time.Now()

	return nil
}

func (a *Agent) run() {
	for {
		// Get context safely
		a.mutex.RLock()
		ctx := a.ctx
		a.mutex.RUnlock()

		if ctx == nil {
			return
		}

		fmt.Println("Agent running:", a.ID, "Role:", a.Role, "Status:", a.Status)
		select {
		case <-ctx.Done():
			return
		default:
			// Agent main loop
			// TODO: Implement agent execution logic
			time.Sleep(1 * time.Second)
		}
	}
}

func generateAgentID(role string) string {
	return fmt.Sprintf("%s-%d", role, time.Now().UnixNano())
}
