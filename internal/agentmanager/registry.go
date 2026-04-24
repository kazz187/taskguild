package agentmanager

import (
	"sync"
	"time"

	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

type connection struct {
	agentManagerID     string
	maxConcurrentTasks int32
	activeTasks        int32
	projectName        string
	workDir            string // absolute path to the agent's project root
	lastHeartbeat      time.Time
	commandCh          chan *taskguildv1.AgentCommand
}

type Registry struct {
	mu    sync.RWMutex
	conns map[string]*connection // keyed by agentManagerID
}

func NewRegistry() *Registry {
	return &Registry{
		conns: make(map[string]*connection),
	}
}

func (r *Registry) Register(agentManagerID string, maxConcurrentTasks int32, projectName string, workDir string) chan *taskguildv1.AgentCommand {
	ch := make(chan *taskguildv1.AgentCommand, 64)

	r.mu.Lock()
	// Close existing connection if re-registering.
	if old, ok := r.conns[agentManagerID]; ok {
		close(old.commandCh)
	}

	r.conns[agentManagerID] = &connection{
		agentManagerID:     agentManagerID,
		maxConcurrentTasks: maxConcurrentTasks,
		projectName:        projectName,
		workDir:            workDir,
		lastHeartbeat:      time.Now(),
		commandCh:          ch,
	}
	r.mu.Unlock()

	return ch
}

func (r *Registry) Unregister(agentManagerID string) {
	r.mu.Lock()
	if conn, ok := r.conns[agentManagerID]; ok {
		close(conn.commandCh)
		delete(r.conns, agentManagerID)
	}
	r.mu.Unlock()
}

func (r *Registry) UpdateHeartbeat(agentManagerID string, activeTasks int32) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	conn, ok := r.conns[agentManagerID]
	if !ok {
		return false
	}

	conn.lastHeartbeat = time.Now()
	conn.activeTasks = activeTasks

	return true
}

// SendCommand sends a command to a specific agent-manager. Returns false if not connected.
func (r *Registry) SendCommand(agentManagerID string, cmd *taskguildv1.AgentCommand) bool {
	r.mu.RLock()
	conn, ok := r.conns[agentManagerID]
	r.mu.RUnlock()

	if !ok {
		return false
	}

	select {
	case conn.commandCh <- cmd:
		return true
	default:
		return false // buffer full
	}
}

// FindAvailable returns the agent-manager ID with the least active tasks
// that still has capacity (activeTasks < maxConcurrentTasks).
// Returns ("", false) if no agent-manager is available.
func (r *Registry) FindAvailable() (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var (
		bestID     string
		bestActive int32 = -1
	)

	for _, conn := range r.conns {
		if conn.activeTasks >= conn.maxConcurrentTasks {
			continue
		}

		if bestActive < 0 || conn.activeTasks < bestActive {
			bestID = conn.agentManagerID
			bestActive = conn.activeTasks
		}
	}

	if bestActive < 0 {
		return "", false
	}

	return bestID, true
}

// BroadcastCommand sends a command to all connected agent-managers.
func (r *Registry) BroadcastCommand(cmd *taskguildv1.AgentCommand) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, conn := range r.conns {
		select {
		case conn.commandCh <- cmd:
		default:
		}
	}
}

// BroadcastCommandToProject sends a command only to agent-managers
// whose projectName matches. Agents with an empty projectName (legacy)
// also receive the command.
func (r *Registry) BroadcastCommandToProject(projectName string, cmd *taskguildv1.AgentCommand) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, conn := range r.conns {
		if conn.projectName != "" && conn.projectName != projectName {
			continue
		}

		select {
		case conn.commandCh <- cmd:
		default:
		}
	}
}

// UnregisterIfMatch removes the connection for agentManagerID only if the
// currently-registered command channel is the same as the one the caller holds.
// Returns true if the connection was removed (caller was the active handler),
// false if the connection belongs to a newer handler or was already removed.
// This prevents a superseded Subscribe handler from accidentally closing a
// newer handler's channel during deferred cleanup.
func (r *Registry) UnregisterIfMatch(agentManagerID string, ch chan *taskguildv1.AgentCommand) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	conn, ok := r.conns[agentManagerID]
	if !ok || conn.commandCh != ch {
		return false
	}

	close(conn.commandCh)
	delete(r.conns, agentManagerID)

	return true
}

// GetProjectName returns the project name for a connected agent-manager.
func (r *Registry) GetProjectName(agentManagerID string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	conn, ok := r.conns[agentManagerID]
	if !ok {
		return "", false
	}

	return conn.projectName, true
}

// HasConnectedAgentForProject returns true if at least one agent-manager is
// connected for the given project name. Agents with an empty projectName
// (legacy) are also considered matching.
func (r *Registry) HasConnectedAgentForProject(projectName string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, conn := range r.conns {
		if conn.projectName == "" || conn.projectName == projectName {
			return true
		}
	}

	return false
}

// GetWorkDirForProject returns the work_dir from the first connected agent
// for the given project name. Returns ("", false) if no agent is connected
// or none has a work_dir set.
func (r *Registry) GetWorkDirForProject(projectName string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, conn := range r.conns {
		if conn.projectName == projectName && conn.workDir != "" {
			return conn.workDir, true
		}
	}

	return "", false
}
