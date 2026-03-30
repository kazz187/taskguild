package agentmanager

import (
	"sync"

	"github.com/kazz187/taskguild/internal/agent"
	"github.com/kazz187/taskguild/internal/claudesettings"
	"github.com/kazz187/taskguild/internal/eventbus"
	"github.com/kazz187/taskguild/internal/interaction"
	"github.com/kazz187/taskguild/internal/permission"
	"github.com/kazz187/taskguild/internal/project"
	"github.com/kazz187/taskguild/internal/script"
	scp "github.com/kazz187/taskguild/internal/singlecommandpermission"
	"github.com/kazz187/taskguild/internal/skill"
	"github.com/kazz187/taskguild/internal/task"
	"github.com/kazz187/taskguild/internal/tasklog"
	"github.com/kazz187/taskguild/internal/workflow"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

var _ taskguildv1connect.AgentManagerServiceHandler = (*Server)(nil)

type Server struct {
	registry        *Registry
	taskRepo        task.Repository
	workflowRepo    workflow.Repository
	agentRepo       agent.Repository
	interactionRepo interaction.Repository
	projectRepo     project.Repository
	skillRepo       skill.Repository
	scriptRepo      script.Repository
	taskLogRepo     tasklog.Repository
	permissionRepo     permission.Repository
	scpRepo            scp.Repository
	claudeSettingsRepo claudesettings.Repository
	eventBus           *eventbus.Bus

	// scriptBroker manages streaming script execution output.
	scriptBroker *script.ScriptExecutionBroker

	// worktreeClaimMu serialises ClaimTask calls for tasks with worktrees,
	// ensuring only one task per worktree can be ASSIGNED at a time.
	worktreeClaimMu sync.Mutex

	// worktreeCache stores the latest worktree list per project_id,
	// populated by ReportWorktreeList and read by GetWorktreeList.
	worktreeMu    sync.RWMutex
	worktreeCache map[string][]*taskguildv1.WorktreeInfo // project_id -> worktrees

	// scriptDiffCache stores the latest script comparison per project_id,
	// populated by ReportScriptComparison and read by GetScriptComparison.
	scriptDiffMu    sync.RWMutex
	scriptDiffCache map[string][]*taskguildv1.ScriptDiff // project_id -> diffs

	// agentDiffCache stores the latest agent comparison per project_id,
	// populated by ReportAgentComparison and read by GetAgentComparison.
	agentDiffMu    sync.RWMutex
	agentDiffCache map[string][]*taskguildv1.AgentDiff // project_id -> diffs

	// skillDiffCache stores the latest skill comparison per project_id,
	// populated by ReportSkillComparison and read by GetSkillComparison.
	skillDiffMu    sync.RWMutex
	skillDiffCache map[string][]*taskguildv1.SkillDiff // project_id -> diffs
}

func NewServer(registry *Registry, taskRepo task.Repository, workflowRepo workflow.Repository, agentRepo agent.Repository, interactionRepo interaction.Repository, projectRepo project.Repository, skillRepo skill.Repository, scriptRepo script.Repository, taskLogRepo tasklog.Repository, permissionRepo permission.Repository, scpRepo scp.Repository, claudeSettingsRepo claudesettings.Repository, eventBus *eventbus.Bus, scriptBroker *script.ScriptExecutionBroker) *Server {
	return &Server{
		registry:           registry,
		taskRepo:           taskRepo,
		workflowRepo:       workflowRepo,
		agentRepo:          agentRepo,
		interactionRepo:    interactionRepo,
		projectRepo:        projectRepo,
		skillRepo:          skillRepo,
		scriptRepo:         scriptRepo,
		taskLogRepo:        taskLogRepo,
		permissionRepo:     permissionRepo,
		scpRepo:            scpRepo,
		claudeSettingsRepo: claudeSettingsRepo,
		eventBus:           eventBus,
		scriptBroker:    scriptBroker,
		worktreeCache:   make(map[string][]*taskguildv1.WorktreeInfo),
		scriptDiffCache: make(map[string][]*taskguildv1.ScriptDiff),
		agentDiffCache:  make(map[string][]*taskguildv1.AgentDiff),
		skillDiffCache:  make(map[string][]*taskguildv1.SkillDiff),
	}
}
