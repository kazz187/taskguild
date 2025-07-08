package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/kazz187/taskguild/internal/daemon"
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

	// Start daemon
	if err := d.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting daemon: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("TaskGuild daemon started on %s:%d\n", addr, port)
	fmt.Println("Press Ctrl+C to stop...")

	// Wait for context cancellation (signal received)
	<-ctx.Done()
	fmt.Println("\nReceived shutdown signal...")
	fmt.Printf("Context error: %v\n", ctx.Err())

	// Create a new context for graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Graceful shutdown
	fmt.Println("Stopping daemon...")
	if err := d.Stop(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "Error stopping daemon: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Daemon stopped")
}
