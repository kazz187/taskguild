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

func (r *Registry) Register(agentManagerID string, maxConcurrentTasks int32) chan *taskguildv1.AgentCommand {
	ch := make(chan *taskguildv1.AgentCommand, 64)
	r.mu.Lock()
	// Close existing connection if re-registering.
	if old, ok := r.conns[agentManagerID]; ok {
		close(old.commandCh)
	}
	r.conns[agentManagerID] = &connection{
		agentManagerID:     agentManagerID,
		maxConcurrentTasks: maxConcurrentTasks,
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

	var bestID string
	var bestActive int32 = -1
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
