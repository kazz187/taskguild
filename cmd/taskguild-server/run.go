package main

import (
	"context"
	"log/slog"
	"net/http"
	_ "net/http/pprof" // registers /debug/pprof/* handlers on DefaultServeMux
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sourcegraph/conc"

	"github.com/kazz187/taskguild/internal/agent"
	agentrepo "github.com/kazz187/taskguild/internal/agent/repositoryimpl"
	"github.com/kazz187/taskguild/internal/agentmanager"
	"github.com/kazz187/taskguild/internal/chatnotifier"
	"github.com/kazz187/taskguild/internal/claudesettings"
	claudesettingsrepo "github.com/kazz187/taskguild/internal/claudesettings/repositoryimpl"
	"github.com/kazz187/taskguild/internal/config"
	"github.com/kazz187/taskguild/internal/event"
	"github.com/kazz187/taskguild/internal/eventbus"
	"github.com/kazz187/taskguild/internal/interaction"
	interactionrepo "github.com/kazz187/taskguild/internal/interaction/repositoryimpl"
	"github.com/kazz187/taskguild/internal/orchestrator"
	"github.com/kazz187/taskguild/internal/permission"
	permissionrepo "github.com/kazz187/taskguild/internal/permission/repositoryimpl"
	"github.com/kazz187/taskguild/internal/singlecommandpermission"
	scprepo "github.com/kazz187/taskguild/internal/singlecommandpermission/repositoryimpl"
	"github.com/kazz187/taskguild/internal/project"
	projectrepo "github.com/kazz187/taskguild/internal/project/repositoryimpl"
	"github.com/kazz187/taskguild/internal/pushnotification"
	pushsubrepo "github.com/kazz187/taskguild/internal/pushsubscription/repositoryimpl"
	"github.com/kazz187/taskguild/internal/script"
	scriptrepo "github.com/kazz187/taskguild/internal/script/repositoryimpl"
	"github.com/kazz187/taskguild/internal/skill"
	skillrepo "github.com/kazz187/taskguild/internal/skill/repositoryimpl"
	"github.com/kazz187/taskguild/internal/task"
	tmpl "github.com/kazz187/taskguild/internal/template"
	tmplrepo "github.com/kazz187/taskguild/internal/template/repositoryimpl"
	taskrepo "github.com/kazz187/taskguild/internal/task/repositoryimpl"
	"github.com/kazz187/taskguild/internal/tasklog"
	tasklogrepo "github.com/kazz187/taskguild/internal/tasklog/repositoryimpl"
	"github.com/kazz187/taskguild/internal/version"
	"github.com/kazz187/taskguild/internal/workflow"
	workflowrepo "github.com/kazz187/taskguild/internal/workflow/repositoryimpl"
	"github.com/kazz187/taskguild/pkg/clog"
	"github.com/kazz187/taskguild/pkg/storage"

	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"

	server "github.com/kazz187/taskguild/internal"
)

// agentChangeNotifier implements agent.ChangeNotifier by broadcasting
// a SyncAgentsCommand to connected agents in the same project.
type agentChangeNotifier struct {
	registry    *agentmanager.Registry
	projectRepo project.Repository
}

func (n *agentChangeNotifier) NotifyAgentChange(projectID string) {
	p, err := n.projectRepo.Get(context.Background(), projectID)
	if err != nil {
		slog.Error("failed to look up project for agent change notification", "project_id", projectID, "error", err)
		return
	}
	n.registry.BroadcastCommandToProject(p.Name, &taskguildv1.AgentCommand{
		Command: &taskguildv1.AgentCommand_SyncAgents{
			SyncAgents: &taskguildv1.SyncAgentsCommand{},
		},
	})
}

// permissionChangeNotifier implements permission.ChangeNotifier by broadcasting
// a SyncPermissionsCommand to connected agents in the same project.
type permissionChangeNotifier struct {
	registry    *agentmanager.Registry
	projectRepo project.Repository
}

func (n *permissionChangeNotifier) NotifyPermissionChange(projectID string) {
	p, err := n.projectRepo.Get(context.Background(), projectID)
	if err != nil {
		slog.Error("failed to look up project for permission change notification", "project_id", projectID, "error", err)
		return
	}
	n.registry.BroadcastCommandToProject(p.Name, &taskguildv1.AgentCommand{
		Command: &taskguildv1.AgentCommand_SyncPermissions{
			SyncPermissions: &taskguildv1.SyncPermissionsCommand{},
		},
	})
}

// scpChangeNotifier implements singlecommandpermission.ChangeNotifier by
// broadcasting a SyncPermissionsCommand to connected agents in the same project.
// This reuses the existing sync mechanism so agents refresh all permission caches.
type scpChangeNotifier struct {
	registry    *agentmanager.Registry
	projectRepo project.Repository
}

func (n *scpChangeNotifier) NotifySingleCommandPermissionChange(projectID string) {
	p, err := n.projectRepo.Get(context.Background(), projectID)
	if err != nil {
		slog.Error("failed to look up project for single command permission change notification", "project_id", projectID, "error", err)
		return
	}
	n.registry.BroadcastCommandToProject(p.Name, &taskguildv1.AgentCommand{
		Command: &taskguildv1.AgentCommand_SyncPermissions{
			SyncPermissions: &taskguildv1.SyncPermissionsCommand{},
		},
	})
}

// skillChangeNotifier implements skill.ChangeNotifier by broadcasting
// a SyncSkillsCommand to connected agents in the same project.
type skillChangeNotifier struct {
	registry    *agentmanager.Registry
	projectRepo project.Repository
}

// claudeSettingsChangeNotifier implements claudesettings.ChangeNotifier by broadcasting
// a SyncClaudeSettingsCommand to connected agents in the same project.
type claudeSettingsChangeNotifier struct {
	registry    *agentmanager.Registry
	projectRepo project.Repository
}

func (n *claudeSettingsChangeNotifier) NotifyClaudeSettingsChange(projectID string) {
	p, err := n.projectRepo.Get(context.Background(), projectID)
	if err != nil {
		slog.Error("failed to look up project for claude settings change notification", "project_id", projectID, "error", err)
		return
	}
	n.registry.BroadcastCommandToProject(p.Name, &taskguildv1.AgentCommand{
		Command: &taskguildv1.AgentCommand_SyncClaudeSettings{
			SyncClaudeSettings: &taskguildv1.SyncClaudeSettingsCommand{},
		},
	})
}

func (n *skillChangeNotifier) NotifySkillChange(projectID string) {
	p, err := n.projectRepo.Get(context.Background(), projectID)
	if err != nil {
		slog.Error("failed to look up project for skill change notification", "project_id", projectID, "error", err)
		return
	}
	n.registry.BroadcastCommandToProject(p.Name, &taskguildv1.AgentCommand{
		Command: &taskguildv1.AgentCommand_SyncSkills{
			SyncSkills: &taskguildv1.SyncSkillsCommand{},
		},
	})
}

func runServer() {
	env, err := config.LoadEnv()
	if err != nil {
		slog.Error("failed to load env", "error", err)
		os.Exit(1)
	}

	// Setup logger
	level := env.SlogLevel()
	var handler slog.Handler
	if env.Env == "local" {
		handler = clog.NewConnectTextHandler(os.Stderr, clog.WithLevel(level))
	} else {
		handler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	}
	slog.SetDefault(slog.New(clog.NewAttributesHandler(handler)))

	slog.Info("server starting", "version", version.Short(), "env", env.Env)

	// Setup storage
	var store storage.Storage
	switch env.StorageEnv.Type {
	case "s3":
		store, err = storage.NewS3Storage(context.Background(), env.StorageEnv.S3Bucket, env.StorageEnv.S3Prefix, env.StorageEnv.S3Region)
		if err != nil {
			slog.Error("failed to create S3 storage", "error", err)
			os.Exit(1)
		}
	default:
		store, err = storage.NewLocalStorage(env.StorageEnv.BaseDir)
		if err != nil {
			slog.Error("failed to create local storage", "error", err)
			os.Exit(1)
		}
	}

	// Setup event bus
	bus := eventbus.New()

	// Setup repositories
	projectRepo := projectrepo.NewYAMLRepository(store)
	workflowRepo := workflowrepo.NewYAMLRepository(store)
	taskRepo := taskrepo.NewYAMLRepository(store)
	interactionRepo := interactionrepo.NewYAMLRepository(store)
	agentRepo := agentrepo.NewYAMLRepository(store)
	skillRepo := skillrepo.NewYAMLRepository(store)
	scriptRepo := scriptrepo.NewYAMLRepository(store)
	taskLogRepo := tasklogrepo.NewYAMLRepository(store)
	pushSubRepo := pushsubrepo.NewYAMLRepository(store)
	permissionRepo := permissionrepo.NewYAMLRepository(store)
	scpRepo := scprepo.NewYAMLRepository(store)
	templateRepo := tmplrepo.NewYAMLRepository(store)
	claudeSettingsRepo := claudesettingsrepo.NewYAMLRepository(store)

	// Setup agent-manager registry
	agentManagerRegistry := agentmanager.NewRegistry()

	// Setup script execution broker for real-time streaming.
	// StartCleanup is called later after the context is created.
	scriptBroker := script.NewScriptExecutionBroker()

	// Setup servers
	projectSeeder := project.NewSeeder(workflowRepo, agentRepo, skillRepo)
	projectServer := project.NewServer(projectRepo, projectSeeder)
	workflowServer := workflow.NewServer(workflowRepo)
	agentManagerServer := agentmanager.NewServer(agentManagerRegistry, taskRepo, workflowRepo, agentRepo, interactionRepo, projectRepo, skillRepo, scriptRepo, taskLogRepo, permissionRepo, scpRepo, claudeSettingsRepo, bus, scriptBroker)
	taskServer := task.NewServer(taskRepo, workflowRepo, bus, agentManagerServer, agentManagerServer, taskLogRepo, interactionRepo)
	interactionServer := interaction.NewServer(interactionRepo, taskRepo, bus)
	agentChangeNotifier := &agentChangeNotifier{
		registry:    agentManagerRegistry,
		projectRepo: projectRepo,
	}
	agentServer := agent.NewServer(agentRepo, agentChangeNotifier)
	skillChangeNotifier := &skillChangeNotifier{
		registry:    agentManagerRegistry,
		projectRepo: projectRepo,
	}
	skillServer := skill.NewServer(skillRepo, skillChangeNotifier)
	scriptServer := script.NewServer(scriptRepo, agentManagerServer, scriptBroker)
	taskLogServer := tasklog.NewServer(taskLogRepo, taskRepo)
	eventServer := event.NewServer(bus)
	permissionChangeNotifier := &permissionChangeNotifier{
		registry:    agentManagerRegistry,
		projectRepo: projectRepo,
	}
	permissionServer := permission.NewServer(permissionRepo, permissionChangeNotifier)
	scpChangeNotifier := &scpChangeNotifier{
		registry:    agentManagerRegistry,
		projectRepo: projectRepo,
	}
	scpServer := singlecommandpermission.NewServer(scpRepo, scpChangeNotifier)
	templateServer := tmpl.NewServer(templateRepo, agentRepo, skillRepo, scriptRepo)
	csChangeNotifier := &claudeSettingsChangeNotifier{
		registry:    agentManagerRegistry,
		projectRepo: projectRepo,
	}
	claudeSettingsServer := claudesettings.NewServer(claudeSettingsRepo, csChangeNotifier)

	// Setup push notification
	vapidEnv := config.VAPIDEnvFromEnv(env)
	pushSender := pushnotification.NewSender(vapidEnv, pushSubRepo)
	pushNotificationServer := pushnotification.NewServer(vapidEnv, pushSubRepo, pushSender)
	baseEnv := config.BaseEnvFromEnv(env)
	pushDispatcher := pushnotification.NewDispatcher(bus, interactionRepo, taskRepo, pushSender, baseEnv)

	srv := server.NewServer(
		env,
		projectServer,
		workflowServer,
		taskServer,
		interactionServer,
		agentManagerServer,
		agentServer,
		skillServer,
		scriptServer,
		eventServer,
		taskLogServer,
		pushNotificationServer,
		permissionServer,
		scpServer,
		templateServer,
		claudeSettingsServer,
	)

	// Setup orchestrator
	orch := orchestrator.New(bus, taskRepo, workflowRepo, projectRepo, agentManagerRegistry)

	// Setup chat notifier (creates notification interactions on task status changes)
	chatNotifier := chatnotifier.New(bus, interactionRepo, taskRepo, workflowRepo)

	// Graceful shutdown context: responds to SIGTERM and SIGINT.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	// Start broker cleanup goroutine for expired executions (TTL-based).
	scriptBroker.StartCleanup(ctx)

	// Cleanup old task logs on startup to prevent unbounded file accumulation
	// that causes massive disk IO on first access.
	if cleaned, err := taskLogRepo.CleanupOlderThan(ctx, 7*24*time.Hour); err != nil {
		slog.Error("task log cleanup failed", "error", err)
	} else if cleaned > 0 {
		slog.Info("startup task log cleanup", "deleted", cleaned)
	}

	// Handle SIGUSR1 for graceful hot-reload.
	// When the sentinel detects a binary update it sends SIGUSR1 instead of
	// SIGTERM. This handler stops accepting new script executions and waits
	// for active ones to complete before triggering the normal shutdown flow.
	usr1Ch := make(chan os.Signal, 1)
	signal.Notify(usr1Ch, syscall.SIGUSR1)
	var sigWg conc.WaitGroup
	sigWg.Go(func() {
		select {
		case <-usr1Ch:
			slog.Info("received SIGUSR1 (hot reload), waiting for active script executions to complete...")
			scriptBroker.SetDraining(true)
			// Use a timeout shorter than the sentinel's ScriptWaitTimeout (6 min)
			// so we shut down gracefully instead of being force-killed.
			drainCtx, drainCancel := context.WithTimeout(context.Background(), 5*time.Minute+30*time.Second)
			defer drainCancel()
			if err := scriptBroker.Drain(drainCtx); err != nil {
				slog.Warn("drain timed out, forcing shutdown", "active_executions", scriptBroker.ActiveCount())
			} else {
				slog.Info("all script executions completed, shutting down for hot reload")
			}
			cancel()
		case <-ctx.Done():
		}
	})

	// Start pprof server on a separate port for profiling (only when --prof is set).
	var svcWg conc.WaitGroup
	if *runProf {
		svcWg.Go(func() {
			pprofAddr := ":6060"
			slog.Info("starting pprof server", "addr", pprofAddr)
			if err := http.ListenAndServe(pprofAddr, nil); err != nil {
				slog.Error("pprof server error", "error", err)
			}
		})
	}

	svcWg.Go(func() { orch.Start(ctx) })
	svcWg.Go(func() { pushDispatcher.Start(ctx) })
	svcWg.Go(func() { chatNotifier.Start(ctx) })

	// Periodic task log cleanup every 6 hours.
	svcWg.Go(func() {
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if cleaned, err := taskLogRepo.CleanupOlderThan(ctx, 7*24*time.Hour); err != nil {
					slog.Error("periodic task log cleanup failed", "error", err)
				} else if cleaned > 0 {
					slog.Info("periodic task log cleanup", "deleted", cleaned)
				}
			}
		}
	})

	svcWg.Go(func() {
		if err := srv.ListenAndServe(ctx); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			cancel()
		}
	})

	<-ctx.Done()
	slog.Info("shutting down server")

	// Give active connections time to finish after stream contexts are cancelled.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "error", err)
	}

	sigWg.Wait()
}
