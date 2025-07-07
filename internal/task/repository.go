package task

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// YAMLRepository implements Repository interface with YAML file persistence
type YAMLRepository struct {
	filePath string
}

// NewYAMLRepository creates a new YAML repository instance
func NewYAMLRepository(filePath string) *YAMLRepository {
	return &YAMLRepository{
		filePath: filePath,
	}
}

// TaskData represents the structure of the YAML file
type TaskData struct {
	Tasks []*Task `yaml:"tasks"`
}

// Create adds a new task to the YAML file
func (r *YAMLRepository) Create(task *Task) error {
	data, err := r.loadData()
	if err != nil {
		return fmt.Errorf("failed to load task data: %w", err)
	}

	data.Tasks = append(data.Tasks, task)

	return r.saveData(data)
}

// GetByID retrieves a task by its ID
func (r *YAMLRepository) GetByID(id string) (*Task, error) {
	data, err := r.loadData()
	if err != nil {
		return nil, fmt.Errorf("failed to load task data: %w", err)
	}

	for _, task := range data.Tasks {
		if task.ID == id {
			return task, nil
		}
	}

	return nil, fmt.Errorf("task with ID %s not found", id)
}

// GetAll retrieves all tasks
func (r *YAMLRepository) GetAll() ([]*Task, error) {
	data, err := r.loadData()
	if err != nil {
		return nil, fmt.Errorf("failed to load task data: %w", err)
	}

	return data.Tasks, nil
}

// Update modifies an existing task
func (r *YAMLRepository) Update(task *Task) error {
	data, err := r.loadData()
	if err != nil {
		return fmt.Errorf("failed to load task data: %w", err)
	}

	for i, existingTask := range data.Tasks {
		if existingTask.ID == task.ID {
			data.Tasks[i] = task
			return r.saveData(data)
		}
	}

	return fmt.Errorf("task with ID %s not found", task.ID)
}

// Delete removes a task by its ID
func (r *YAMLRepository) Delete(id string) error {
	data, err := r.loadData()
	if err != nil {
		return fmt.Errorf("failed to load task data: %w", err)
	}

	for i, task := range data.Tasks {
		if task.ID == id {
			data.Tasks = append(data.Tasks[:i], data.Tasks[i+1:]...)
			return r.saveData(data)
		}
	}

	return fmt.Errorf("task with ID %s not found", id)
}

// loadData loads task data from the YAML file
func (r *YAMLRepository) loadData() (*TaskData, error) {
	// Create directory if it doesn't exist
	dir := filepath.Dir(r.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Return empty data if file doesn't exist
	if _, err := os.Stat(r.filePath); os.IsNotExist(err) {
		return &TaskData{Tasks: []*Task{}}, nil
	}

	content, err := os.ReadFile(r.filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var data TaskData
	if err := yaml.Unmarshal(content, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal YAML: %w", err)
	}

	return &data, nil
}

// saveData saves task data to the YAML file
func (r *YAMLRepository) saveData(data *TaskData) error {
	// Create directory if it doesn't exist
	dir := filepath.Dir(r.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	content, err := yaml.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal YAML: %w", err)
	}

	if err := os.WriteFile(r.filePath, content, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}
