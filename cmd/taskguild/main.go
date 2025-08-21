package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/mattn/go-runewidth"

	"github.com/kazz187/taskguild/internal/client"
	"github.com/kazz187/taskguild/internal/daemon"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

var (
	app = kingpin.New("taskguild", "AI agent orchestration tool for software development")

	// Daemon commands
	startCmd  = app.Command("start", "Start the TaskGuild daemon")
	startAddr = startCmd.Flag("addr", "Address to bind to").Default("localhost").String()
	startPort = startCmd.Flag("port", "Port to bind to").Default("8080").Int()

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

	switch command {
	case startCmd.FullCommand():
		handleStartDaemon(*startAddr, *startPort)
	case agentListCmd.FullCommand():
		handleAgentList()
	case agentStartCmd.FullCommand():
		handleAgentStart(*agentStartID)
	case agentStopCmd.FullCommand():
		handleAgentStop(*agentStopID)
	case agentStatusCmd.FullCommand():
		if agentStatusID != nil && *agentStatusID != "" {
			handleAgentStatus(*agentStatusID)
		} else {
			handleAgentList()
		}
	case agentScaleCmd.FullCommand():
		handleAgentScale(*agentScaleRole, *agentScaleCount)
	case createCmd.FullCommand():
		handleTaskCreate(*createTitle, *createType)
	case listCmd.FullCommand():
		handleTaskList()
	case updateCmd.FullCommand():
		handleTaskUpdate(*updateID, *updateStatus)
	case closeCmd.FullCommand():
		handleTaskClose(*closeID)
	case showCmd.FullCommand():
		handleTaskShow(*showID)
	default:
		fmt.Fprintf(os.Stderr, "Command not yet implemented in daemon mode. Please start daemon first with 'taskguild start'\n")
		os.Exit(1)
	}
}

func handleStartDaemon(addr string, port int) {
	// Create daemon configuration
	config := &daemon.Config{
		Address: addr,
		Port:    port,
	}

	// Create daemon instance
	d, err := daemon.New(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating daemon: %v\n", err)
		os.Exit(1)
	}

	// Setup context with signal notification for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		// Wait for context cancellation (signal received)
		<-ctx.Done()
		fmt.Println("Received shutdown signal, stopping daemon...")
	}()
	// Start daemon - this will block until context is cancelled or error occurs
	// The new design uses pool.WithCancelOnError() so Start() will return when signal is received
	if err := d.Start(ctx); err != nil {
		if ctx.Err() != nil {
			// Context was cancelled (signal received)
			fmt.Println("Daemon stopped gracefully")
		} else {
			// Actual error occurred
			fmt.Fprintf(os.Stderr, "Error running daemon: %v\n", err)
			os.Exit(1)
		}
	}
}

func createAgentClient() *client.AgentClient {
	// TODO: Make this configurable
	baseURL := "http://localhost:8080"
	return client.NewAgentClient(baseURL)
}

func createTaskClient() *client.TaskClient {
	// TODO: Make this configurable
	baseURL := "http://localhost:8080"
	return client.NewTaskClient(baseURL)
}

func handleAgentList() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	agentClient := createAgentClient()
	agents, err := agentClient.ListAgents(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing agents: %v\n", err)
		os.Exit(1)
	}

	if len(agents) == 0 {
		fmt.Println("No agents found")
		return
	}

	fmt.Printf("%-30s %-15s %-15s %-10s %-20s %-20s\n", "ID", "NAME", "TYPE", "STATUS", "TASK_ID", "WORKTREE_PATH")
	fmt.Printf("%-30s %-15s %-15s %-10s %-20s %-20s\n",
		"------------------------------",
		"---------------",
		"---------------",
		"----------",
		"--------------------",
		"--------------------")

	for _, agent := range agents {
		status := getAgentStatusString(agent.Status)
		taskID := agent.TaskId
		if taskID == "" {
			taskID = "-"
		}
		worktreePath := agent.WorktreePath
		if worktreePath == "" {
			worktreePath = "-"
		}

		fmt.Printf("%-30s %-15s %-15s %-10s %-20s %-20s\n",
			agent.Id,
			agent.Name,
			agent.Type,
			status,
			taskID,
			worktreePath)
	}
}

func handleAgentStart(agentID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	agentClient := createAgentClient()
	agent, err := agentClient.StartAgent(ctx, agentID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error starting agent: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Agent %s started successfully. Status: %s\n", agent.Id, getAgentStatusString(agent.Status))
}

func handleAgentStop(agentID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	agentClient := createAgentClient()
	agent, err := agentClient.StopAgent(ctx, agentID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error stopping agent: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Agent %s stopped successfully. Status: %s\n", agent.Id, getAgentStatusString(agent.Status))
}

func handleAgentStatus(agentID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	agentClient := createAgentClient()
	agent, err := agentClient.GetAgentStatus(ctx, agentID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting agent status: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Agent: %s\n", agent.Id)
	fmt.Printf("Name: %s\n", agent.Name)
	fmt.Printf("Type: %s\n", agent.Type)
	fmt.Printf("Status: %s\n", getAgentStatusString(agent.Status))
	if agent.Description != "" {
		fmt.Printf("Description: %s\n", agent.Description)
	}
	if agent.Version != "" {
		fmt.Printf("Version: %s\n", agent.Version)
	}
	if agent.Instructions != "" {
		fmt.Printf("Instructions: %s\n", agent.Instructions)
	}
	if agent.TaskId != "" {
		fmt.Printf("Task ID: %s\n", agent.TaskId)
	}
	if agent.WorktreePath != "" {
		fmt.Printf("Worktree Path: %s\n", agent.WorktreePath)
	}
	fmt.Printf("Created At: %s\n", agent.CreatedAt.AsTime().Format(time.RFC3339))
	fmt.Printf("Updated At: %s\n", agent.UpdatedAt.AsTime().Format(time.RFC3339))
}

func handleAgentScale(role string, count int) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	agentClient := createAgentClient()
	agents, err := agentClient.ScaleAgent(ctx, role, int32(count))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error scaling agents: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Scaled agents for role '%s' to %d instances:\n", role, count)
	for _, agent := range agents {
		fmt.Printf("  - %s (%s)\n", agent.Id, getAgentStatusString(agent.Status))
	}
}

func getAgentStatusString(status taskguildv1.AgentStatus) string {
	switch status {
	case taskguildv1.AgentStatus_AGENT_STATUS_IDLE:
		return "IDLE"
	case taskguildv1.AgentStatus_AGENT_STATUS_BUSY:
		return "BUSY"
	case taskguildv1.AgentStatus_AGENT_STATUS_WAITING:
		return "WAITING"
	case taskguildv1.AgentStatus_AGENT_STATUS_ERROR:
		return "ERROR"
	case taskguildv1.AgentStatus_AGENT_STATUS_STOPPED:
		return "STOPPED"
	default:
		return "UNKNOWN"
	}
}

func getTaskStatusString(status taskguildv1.TaskStatus) string {
	switch status {
	case taskguildv1.TaskStatus_TASK_STATUS_CREATED:
		return "CREATED"
	case taskguildv1.TaskStatus_TASK_STATUS_ANALYZING:
		return "ANALYZING"
	case taskguildv1.TaskStatus_TASK_STATUS_DESIGNED:
		return "DESIGNED"
	case taskguildv1.TaskStatus_TASK_STATUS_IN_PROGRESS:
		return "IN_PROGRESS"
	case taskguildv1.TaskStatus_TASK_STATUS_REVIEW_READY:
		return "REVIEW_READY"
	case taskguildv1.TaskStatus_TASK_STATUS_QA_READY:
		return "QA_READY"
	case taskguildv1.TaskStatus_TASK_STATUS_CLOSED:
		return "CLOSED"
	case taskguildv1.TaskStatus_TASK_STATUS_CANCELLED:
		return "CANCELLED"
	default:
		return "UNSPECIFIED"
	}
}

func getTaskStatusEnum(status string) taskguildv1.TaskStatus {
	switch status {
	case "CREATED":
		return taskguildv1.TaskStatus_TASK_STATUS_CREATED
	case "ANALYZING":
		return taskguildv1.TaskStatus_TASK_STATUS_ANALYZING
	case "DESIGNED":
		return taskguildv1.TaskStatus_TASK_STATUS_DESIGNED
	case "IN_PROGRESS":
		return taskguildv1.TaskStatus_TASK_STATUS_IN_PROGRESS
	case "REVIEW_READY":
		return taskguildv1.TaskStatus_TASK_STATUS_REVIEW_READY
	case "QA_READY":
		return taskguildv1.TaskStatus_TASK_STATUS_QA_READY
	case "CLOSED":
		return taskguildv1.TaskStatus_TASK_STATUS_CLOSED
	case "CANCELLED":
		return taskguildv1.TaskStatus_TASK_STATUS_CANCELLED
	default:
		return taskguildv1.TaskStatus_TASK_STATUS_UNSPECIFIED
	}
}

func handleTaskCreate(title, taskType string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	taskClient := createTaskClient()
	task, err := taskClient.CreateTask(ctx, title, "", taskType)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating task: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Task created successfully:\n")
	fmt.Printf("ID: %s\n", task.Id)
	fmt.Printf("Title: %s\n", task.Title)
	fmt.Printf("Type: %s\n", task.Type)
	fmt.Printf("Status: %s\n", getTaskStatusString(task.Status))
	fmt.Printf("Created At: %s\n", task.CreatedAt.AsTime().Format(time.RFC3339))
}

func handleTaskList() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	taskClient := createTaskClient()
	tasks, err := taskClient.ListTasks(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing tasks: %v\n", err)
		os.Exit(1)
	}

	if len(tasks) == 0 {
		fmt.Println("No tasks found")
		return
	}

	fmt.Printf("%-10s %-40s %-10s %-15s %-20s\n", "ID", "TITLE", "TYPE", "STATUS", "CREATED_AT")
	fmt.Printf("%-10s %-40s %-10s %-15s %-20s\n",
		"----------",
		"----------------------------------------",
		"----------",
		"---------------",
		"--------------------")

	for _, task := range tasks {
		// Truncate title to fit in 40 character width properly handling full-width chars
		title := runewidth.Truncate(task.Title, 40, "...")

		// Fill title to exactly 40 width for proper column alignment
		titleFilled := runewidth.FillRight(title, 40)

		fmt.Printf("%-10s %s %-10s %-15s %-20s\n",
			task.Id,
			titleFilled,
			task.Type,
			getTaskStatusString(task.Status),
			task.CreatedAt.AsTime().Format("2006-01-02 15:04:05"))
	}
}

func handleTaskUpdate(taskID, status string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Convert status string to enum
	statusEnum := getTaskStatusEnum(status)
	if statusEnum == taskguildv1.TaskStatus_TASK_STATUS_UNSPECIFIED {
		fmt.Fprintf(os.Stderr, "Error: Invalid status '%s'\n", status)
		fmt.Fprintf(os.Stderr, "Valid statuses: CREATED, ANALYZING, DESIGNED, IN_PROGRESS, REVIEW_READY, QA_READY, CLOSED, CANCELLED\n")
		os.Exit(1)
	}

	taskClient := createTaskClient()
	task, err := taskClient.UpdateTask(ctx, taskID, statusEnum)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error updating task: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Task %s updated successfully. New status: %s\n", task.Id, getTaskStatusString(task.Status))
}

func handleTaskClose(taskID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	taskClient := createTaskClient()
	task, err := taskClient.CloseTask(ctx, taskID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error closing task: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Task %s closed successfully. Status: %s\n", task.Id, getTaskStatusString(task.Status))
}

func handleTaskShow(taskID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	taskClient := createTaskClient()
	task, err := taskClient.GetTask(ctx, taskID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting task: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Task Details:\n")
	fmt.Printf("ID: %s\n", task.Id)
	fmt.Printf("Title: %s\n", task.Title)
	if task.Description != "" {
		fmt.Printf("Description: %s\n", task.Description)
	}
	fmt.Printf("Type: %s\n", task.Type)
	fmt.Printf("Status: %s\n", getTaskStatusString(task.Status))
	if task.AssignedTo != "" {
		fmt.Printf("Assigned To: %s\n", task.AssignedTo)
	}
	if len(task.Metadata) > 0 {
		fmt.Printf("Metadata:\n")
		for k, v := range task.Metadata {
			fmt.Printf("  %s: %s\n", k, v)
		}
	}
	fmt.Printf("Created At: %s\n", task.CreatedAt.AsTime().Format(time.RFC3339))
	fmt.Printf("Updated At: %s\n", task.UpdatedAt.AsTime().Format(time.RFC3339))
}
