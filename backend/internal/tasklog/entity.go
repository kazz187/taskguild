package tasklog

import "time"

type TaskLog struct {
	ID        string            `yaml:"id"`
	TaskID    string            `yaml:"task_id"`
	Level     int32             `yaml:"level"`
	Category  int32             `yaml:"category"`
	Message   string            `yaml:"message"`
	Metadata  map[string]string `yaml:"metadata"`
	CreatedAt time.Time         `yaml:"created_at"`
}
