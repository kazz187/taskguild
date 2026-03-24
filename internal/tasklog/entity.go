package tasklog

import "time"

type TaskLog struct {
	ID        string            `json:"id"`
	ProjectID string            `json:"project_id"`
	TaskID    string            `json:"task_id"`
	Level     int32             `json:"level"`
	Category  int32             `json:"category"`
	Message   string            `json:"message"`
	Metadata  map[string]string `json:"metadata"`
	CreatedAt time.Time         `json:"created_at"`
}
