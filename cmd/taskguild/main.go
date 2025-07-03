package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/kazz187/taskguild/internal/task"
)

var (
	app = kingpin.New("taskguild", "AI agent orchestration tool for software development")

	// Task commands
	createCmd = app.Command("create", "Create a new task")
	createTitle = createCmd.Arg("title", "Task title").Required().String()
	createType = createCmd.Flag("type", "Task type").Default("task").String()

	listCmd = app.Command("list", "List all tasks")

	updateCmd = app.Command("update", "Update task status")
	updateID = updateCmd.Arg("id", "Task ID").Required().String()
	updateStatus = updateCmd.Arg("status", "New status").Required().String()

	closeCmd = app.Command("close", "Close a task")
	closeID = closeCmd.Arg("id", "Task ID").Required().String()

	showCmd = app.Command("show", "Show task details")
	showID = showCmd.Arg("id", "Task ID").Required().String()
)

func main() {
	command := kingpin.MustParse(app.Parse(os.Args[1:]))

	// Initialize task service
	taskFilePath := filepath.Join(".taskguild", "task.yaml")
	repository := task.NewYAMLRepository(taskFilePath)
	service := task.NewTaskService(repository)

	switch command {
	case createCmd.FullCommand():
		handleCreateTask(service, *createTitle, *createType)
	case listCmd.FullCommand():
		handleListTasks(service)
	case updateCmd.FullCommand():
		handleUpdateTask(service, *updateID, *updateStatus)
	case closeCmd.FullCommand():
		handleCloseTask(service, *closeID)
	case showCmd.FullCommand():
		handleShowTask(service, *showID)
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