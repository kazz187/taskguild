package task

import (
	"fmt"
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

// ProcessStatus represents the status of a process
type ProcessStatus string

const (
	ProcessStatusPending    ProcessStatus = "pending"
	ProcessStatusInProgress ProcessStatus = "in_progress"
	ProcessStatusCompleted  ProcessStatus = "completed"
	ProcessStatusRejected   ProcessStatus = "rejected"
)

// ProcessDefinition defines a process in the task workflow
type ProcessDefinition struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description,omitempty"`
	DependsOn   []string `yaml:"depends_on,omitempty"`
}

// TaskDefinition represents the task workflow definition
type TaskDefinition struct {
	Processes     []*ProcessDefinition `yaml:"processes"`
	OnAllComplete string               `yaml:"on_all_complete,omitempty"` // e.g., "close"
	processMap    map[string]*ProcessDefinition
	dependentsMap map[string][]string // maps process -> processes that depend on it
}

// ProcessState represents the runtime state of a process
type ProcessState struct {
	Status     ProcessStatus `yaml:"status"`
	AssignedTo string        `yaml:"assigned_to,omitempty"`
}

// ProcessChangeEvent represents a process state change notification
type ProcessChangeEvent struct {
	TaskID      string
	ProcessName string
	OldStatus   ProcessStatus
	NewStatus   ProcessStatus
	ChangedBy   string
}

// ProcessWatcher provides channel-based notifications for process changes
type ProcessWatcher struct {
	taskID      string
	processName string
	ch          chan ProcessChangeEvent
}

// LoadTaskDefinition loads the task definition from a YAML file
func LoadTaskDefinition(path string) (*TaskDefinition, error) {
	if path == "" {
		path = ".taskguild/task-definition.yaml"
	}

	// Check if file exists, if not create default
	if _, err := os.Stat(path); os.IsNotExist(err) {
		def := DefaultTaskDefinition()
		if err := def.Save(path); err != nil {
			return nil, fmt.Errorf("failed to save default task definition: %w", err)
		}
		return def, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read task definition: %w", err)
	}

	var def TaskDefinition
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("failed to parse task definition: %w", err)
	}

	// Build internal maps
	def.buildMaps()

	// Validate
	if err := def.Validate(); err != nil {
		return nil, fmt.Errorf("invalid task definition: %w", err)
	}

	return &def, nil
}

// DefaultTaskDefinition creates a default task definition
func DefaultTaskDefinition() *TaskDefinition {
	def := &TaskDefinition{
		Processes: []*ProcessDefinition{
			{
				Name:        "implement",
				Description: "Feature implementation",
			},
			{
				Name:        "review",
				Description: "Code review",
				DependsOn:   []string{"implement"},
			},
			{
				Name:        "qa",
				Description: "QA validation",
				DependsOn:   []string{"implement"},
			},
		},
		OnAllComplete: "close",
	}
	def.buildMaps()
	return def
}

// buildMaps builds internal lookup maps
func (d *TaskDefinition) buildMaps() {
	d.processMap = make(map[string]*ProcessDefinition)
	d.dependentsMap = make(map[string][]string)

	for _, p := range d.Processes {
		d.processMap[p.Name] = p
		for _, dep := range p.DependsOn {
			d.dependentsMap[dep] = append(d.dependentsMap[dep], p.Name)
		}
	}
}

// Save saves the task definition to a YAML file
func (d *TaskDefinition) Save(path string) error {
	data, err := yaml.Marshal(d)
	if err != nil {
		return fmt.Errorf("failed to marshal task definition: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write task definition: %w", err)
	}

	return nil
}

// Validate validates the task definition
func (d *TaskDefinition) Validate() error {
	if len(d.Processes) == 0 {
		return fmt.Errorf("no processes defined")
	}

	names := make(map[string]bool)
	for _, p := range d.Processes {
		if p.Name == "" {
			return fmt.Errorf("process name cannot be empty")
		}
		if names[p.Name] {
			return fmt.Errorf("duplicate process name: %s", p.Name)
		}
		names[p.Name] = true
	}

	// Validate dependencies exist
	for _, p := range d.Processes {
		for _, dep := range p.DependsOn {
			if !names[dep] {
				return fmt.Errorf("process %s depends on unknown process: %s", p.Name, dep)
			}
		}
	}

	// Check for circular dependencies
	if err := d.detectCycles(); err != nil {
		return err
	}

	return nil
}

// detectCycles detects circular dependencies using DFS
func (d *TaskDefinition) detectCycles() error {
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	var visit func(name string) error
	visit = func(name string) error {
		visited[name] = true
		recStack[name] = true

		p := d.processMap[name]
		if p != nil {
			for _, dep := range p.DependsOn {
				if !visited[dep] {
					if err := visit(dep); err != nil {
						return err
					}
				} else if recStack[dep] {
					return fmt.Errorf("circular dependency detected: %s -> %s", name, dep)
				}
			}
		}

		recStack[name] = false
		return nil
	}

	for _, p := range d.Processes {
		if !visited[p.Name] {
			if err := visit(p.Name); err != nil {
				return err
			}
		}
	}

	return nil
}

// GetProcess returns a process definition by name
func (d *TaskDefinition) GetProcess(name string) (*ProcessDefinition, bool) {
	p, ok := d.processMap[name]
	return p, ok
}

// GetDependents returns processes that depend on the given process
func (d *TaskDefinition) GetDependents(name string) []string {
	return d.dependentsMap[name]
}

// GetAllDependents returns all processes that transitively depend on the given process
func (d *TaskDefinition) GetAllDependents(name string) []string {
	result := make(map[string]bool)
	var collect func(n string)
	collect = func(n string) {
		for _, dep := range d.dependentsMap[n] {
			if !result[dep] {
				result[dep] = true
				collect(dep)
			}
		}
	}
	collect(name)

	var dependents []string
	for dep := range result {
		dependents = append(dependents, dep)
	}
	return dependents
}

// CanStart checks if a process can be started based on its dependencies
func (d *TaskDefinition) CanStart(processName string, states map[string]*ProcessState) bool {
	p, ok := d.processMap[processName]
	if !ok {
		return false
	}

	// Check if already in progress or completed
	if state, ok := states[processName]; ok {
		if state.Status == ProcessStatusInProgress || state.Status == ProcessStatusCompleted {
			return false
		}
	}

	// Check all dependencies are completed
	for _, dep := range p.DependsOn {
		state, ok := states[dep]
		if !ok || state.Status != ProcessStatusCompleted {
			return false
		}
	}

	return true
}

// CreateInitialProcessStates creates the initial process states for a new task
func (d *TaskDefinition) CreateInitialProcessStates() map[string]*ProcessState {
	states := make(map[string]*ProcessState)
	for _, p := range d.Processes {
		states[p.Name] = &ProcessState{
			Status: ProcessStatusPending,
		}
	}
	return states
}

// ProcessEventBus manages process change notifications
type ProcessEventBus struct {
	mutex    sync.RWMutex
	watchers map[string][]*ProcessWatcher // key: "taskID:processName" or "taskID:*" for all
}

// NewProcessEventBus creates a new process event bus
func NewProcessEventBus() *ProcessEventBus {
	return &ProcessEventBus{
		watchers: make(map[string][]*ProcessWatcher),
	}
}

// Watch creates a watcher for process changes
// Use processName = "*" to watch all processes for a task
func (b *ProcessEventBus) Watch(taskID, processName string) <-chan ProcessChangeEvent {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	ch := make(chan ProcessChangeEvent, 10)
	watcher := &ProcessWatcher{
		taskID:      taskID,
		processName: processName,
		ch:          ch,
	}

	key := fmt.Sprintf("%s:%s", taskID, processName)
	b.watchers[key] = append(b.watchers[key], watcher)

	return ch
}

// Unwatch removes a watcher
func (b *ProcessEventBus) Unwatch(taskID, processName string, ch <-chan ProcessChangeEvent) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	key := fmt.Sprintf("%s:%s", taskID, processName)
	watchers := b.watchers[key]
	for i, w := range watchers {
		if w.ch == ch {
			// Close the channel
			close(w.ch)
			// Remove from slice
			b.watchers[key] = append(watchers[:i], watchers[i+1:]...)
			break
		}
	}
}

// Notify sends a process change event to all relevant watchers
func (b *ProcessEventBus) Notify(event ProcessChangeEvent) {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	// Notify specific process watchers
	specificKey := fmt.Sprintf("%s:%s", event.TaskID, event.ProcessName)
	for _, w := range b.watchers[specificKey] {
		select {
		case w.ch <- event:
		default:
			// Channel full, skip
		}
	}

	// Notify wildcard watchers for this task
	wildcardKey := fmt.Sprintf("%s:*", event.TaskID)
	for _, w := range b.watchers[wildcardKey] {
		select {
		case w.ch <- event:
		default:
			// Channel full, skip
		}
	}
}

// Close closes all watchers
func (b *ProcessEventBus) Close() {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	for _, watchers := range b.watchers {
		for _, w := range watchers {
			close(w.ch)
		}
	}
	b.watchers = make(map[string][]*ProcessWatcher)
}
