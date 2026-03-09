package skill

import "time"

type Skill struct {
	ID                     string    `yaml:"id"`
	ProjectID              string    `yaml:"project_id"`
	Name                   string    `yaml:"name"`
	Description            string    `yaml:"description"`
	Content                string    `yaml:"content"`
	DisableModelInvocation bool      `yaml:"disable_model_invocation"`
	UserInvocable          bool      `yaml:"user_invocable"`
	AllowedTools           []string  `yaml:"allowed_tools"`
	Model                  string    `yaml:"model"`
	Context                string    `yaml:"context"`
	Agent                  string    `yaml:"agent"`
	ArgumentHint           string    `yaml:"argument_hint"`
	IsSynced               bool      `yaml:"is_synced"`
	CreatedAt              time.Time `yaml:"created_at"`
	UpdatedAt              time.Time `yaml:"updated_at"`
}
