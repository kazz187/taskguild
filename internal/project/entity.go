package project

import "time"

type Project struct {
	ID                string    `yaml:"id"`
	Name              string    `yaml:"name"`
	Description       string    `yaml:"description"`
	RepositoryURL     string    `yaml:"repository_url"`
	DefaultBranch     string    `yaml:"default_branch"`
	Order             int32     `yaml:"order"`
	HiddenFromSidebar bool      `yaml:"hidden_from_sidebar"`
	CreatedAt         time.Time `yaml:"created_at"`
	UpdatedAt         time.Time `yaml:"updated_at"`
}
