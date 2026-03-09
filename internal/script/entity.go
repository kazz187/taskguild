package script

import "time"

type Script struct {
	ID          string    `yaml:"id"`
	ProjectID   string    `yaml:"project_id"`
	Name        string    `yaml:"name"`
	Description string    `yaml:"description"`
	Filename    string    `yaml:"filename"`
	Content     string    `yaml:"content"`
	IsSynced    bool      `yaml:"is_synced"`
	CreatedAt   time.Time `yaml:"created_at"`
	UpdatedAt   time.Time `yaml:"updated_at"`
}
