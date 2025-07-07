package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/kazz187/taskguild/internal/agent"
	"github.com/kazz187/taskguild/internal/event"
	"github.com/kazz187/taskguild/internal/task"
)

var (
	app = kingpin.New("taskguild", "AI agent orchestration tool for software development")

	// Task commands
	createCmd   = app.Command("create", "Create a new task")
	createTitle = createCmd.Arg("title", "Task title").Required().String()
	createType  = createCmd.Flag("type", "Task type").Default("task").String()

	listCmd = app.Command("list", "List all tasks")

	updateCmd    = app.Command("update", "Update task status")
	updateID     = updateCmd.Arg("id", "Task ID").Required().String()
	updateStatus = updateCmd.Arg("status", "New status").Required().String()

	closeCmd = app.Command("close", "Close a task")
	closeID  = closeCmd.Arg("id", "Task ID").Required().String()

	showCmd = app.Command("show", "Show task details")
	showID  = showCmd.Arg("id", "Task ID").Required().String()

	// Agent commands
	agentCmd = app.Command("agent", "Agent management commands")

	agentListCmd = agentCmd.Command("list", "List all agents")

	agentStartCmd = agentCmd.Command("start", "Start an agent")
	agentStartID  = agentStartCmd.Arg("id", "Agent ID").Required().String()

	agentStopCmd = agentCmd.Command("stop", "Stop an agent")
	agentStopID  = agentStopCmd.Arg("id", "Agent ID").Required().String()

	agentStatusCmd = agentCmd.Command("status", "Show agent status")
	agentStatusID  = agentStatusCmd.Arg("id", "Agent ID").String()

	agentScaleCmd   = agentCmd.Command("scale", "Scale agents")
	agentScaleRole  = agentScaleCmd.Arg("role", "Agent role").Required().String()
	agentScaleCount = agentScaleCmd.Arg("count", "Target count").Required().Int()
)

func main() {
	command := kingpin.MustParse(app.Parse(os.Args[1:]))

	// Initialize services
	taskFilePath := filepath.Join(".taskguild", "task.yaml")
	repository := task.NewYAMLRepository(taskFilePath)
	taskService := task.NewTaskService(repository)

	// Initialize event bus
	eventBus := event.NewEventBus()

	// Initialize agent service
	agentConfigPath := filepath.Join(".taskguild", "agent.yaml")
	agentService, err := agent.NewService(agentConfigPath, eventBus)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing agent service: %v\n", err)
		os.Exit(1)
	}

	switch command {
	case createCmd.FullCommand():
		handleCreateTask(taskService, *createTitle, *createType)
	case listCmd.FullCommand():
		handleListTasks(taskService)
	case updateCmd.FullCommand():
		handleUpdateTask(taskService, *updateID, *updateStatus)
	case closeCmd.FullCommand():
		handleCloseTask(taskService, *closeID)
	case showCmd.FullCommand():
		handleShowTask(taskService, *showID)
	case agentListCmd.FullCommand():
		initializeAgentService(agentService)
		handleAgentList(agentService)
	case agentStartCmd.FullCommand():
		initializeAgentService(agentService)
		handleAgentStart(agentService, *agentStartID)
	case agentStopCmd.FullCommand():
		initializeAgentService(agentService)
		handleAgentStop(agentService, *agentStopID)
	case agentStatusCmd.FullCommand():
		initializeAgentService(agentService)
		handleAgentStatus(agentService, *agentStatusID)
	case agentScaleCmd.FullCommand():
		initializeAgentService(agentService)
		handleAgentScale(agentService, *agentScaleRole, *agentScaleCount)
	}
}

func initializeAgentService(service *agent.Service) {
	// Start the agent service to initialize agents
	ctx := context.Background()
	if err := service.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to start agent service: %v\n", err)
	}
}

func handleCreateTask(service *task.TaskService, title, taskType string) {
	task, err := service.CreateTask(title, taskType)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating task: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Task created: %s (Status: %s)\n", task.ID, task.Status)
}

func handleListTasks(service *task.TaskService) {
	tasks, err := service.ListTasks()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing tasks: %v\n", err)
		os.Exit(1)
	}

	if len(tasks) == 0 {
		fmt.Println("No tasks found.")
		return
	}

	fmt.Printf("%-10s %-20s %-10s %-30s\n", "ID", "Status", "Type", "Title")
	fmt.Println(strings.Repeat("-", 72))
	for _, task := range tasks {
		fmt.Printf("%-10s %-20s %-10s %-30s\n", task.ID, task.Status, task.Type, task.Title)
	}
}

func handleUpdateTask(service *task.TaskService, id, status string) {
	if err := service.UpdateTaskStatus(id, status); err != nil {
		fmt.Fprintf(os.Stderr, "Error updating task: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Task %s updated to status: %s\n", id, status)
}

func handleCloseTask(service *task.TaskService, id string) {
	if err := service.CloseTask(id); err != nil {
		fmt.Fprintf(os.Stderr, "Error closing task: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Task %s closed\n", id)
}

func handleShowTask(service *task.TaskService, id string) {
	task, err := service.GetTask(id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting task: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Task ID: %s\n", task.ID)
	fmt.Printf("Title: %s\n", task.Title)
	fmt.Printf("Type: %s\n", task.Type)
	fmt.Printf("Status: %s\n", task.Status)
	fmt.Printf("Worktree: %s\n", task.Worktree)
	fmt.Printf("Branch: %s\n", task.Branch)
	fmt.Printf("Assigned Agents: %v\n", task.AssignedAgents)
	fmt.Printf("Created: %s\n", task.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("Updated: %s\n", task.UpdatedAt.Format("2006-01-02 15:04:05"))
}

func handleAgentList(service *agent.Service) {
	agents := service.ListAgents()

	if len(agents) == 0 {
		fmt.Println("No agents found.")
		return
	}

	fmt.Printf("%-20s %-15s %-10s %-15s %-20s\n", "ID", "Role", "Type", "Status", "Task")
	fmt.Println(strings.Repeat("-", 82))
	for _, agent := range agents {
		taskID := agent.TaskID
		if taskID == "" {
			taskID = "-"
		}
		fmt.Printf("%-20s %-15s %-10s %-15s %-20s\n", agent.ID, agent.Role, agent.Type, agent.Status, taskID)
	}
}

func handleAgentStart(service *agent.Service, agentID string) {
	agent, exists := service.GetAgent(agentID)
	if !exists {
		fmt.Fprintf(os.Stderr, "Agent %s not found\n", agentID)
		os.Exit(1)
	}

	if err := agent.Start(nil); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting agent: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Agent %s started\n", agentID)
}

func handleAgentStop(service *agent.Service, agentID string) {
	agent, exists := service.GetAgent(agentID)
	if !exists {
		fmt.Fprintf(os.Stderr, "Agent %s not found\n", agentID)
		os.Exit(1)
	}

	if err := agent.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "Error stopping agent: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Agent %s stopped\n", agentID)
}

func handleAgentStatus(service *agent.Service, agentID string) {
	if agentID == "" {
		handleAgentList(service)
		return
	}

	agent, exists := service.GetAgent(agentID)
	if !exists {
		fmt.Fprintf(os.Stderr, "Agent %s not found\n", agentID)
		os.Exit(1)
	}

	fmt.Printf("Agent ID: %s\n", agent.ID)
	fmt.Printf("Role: %s\n", agent.Role)
	fmt.Printf("Type: %s\n", agent.Type)
	fmt.Printf("Status: %s\n", agent.Status)
	fmt.Printf("Memory Path: %s\n", agent.MemoryPath)
	fmt.Printf("Task ID: %s\n", agent.TaskID)
	fmt.Printf("Worktree Path: %s\n", agent.WorktreePath)
	fmt.Printf("Created: %s\n", agent.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("Updated: %s\n", agent.UpdatedAt.Format("2006-01-02 15:04:05"))
}

func handleAgentScale(service *agent.Service, role string, targetCount int) {
	if err := service.ScaleAgents(role, targetCount); err != nil {
		fmt.Fprintf(os.Stderr, "Error scaling agents: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Agent role %s scaled to %d instances\n", role, targetCount)
}
