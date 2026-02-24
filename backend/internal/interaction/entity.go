package interaction

import "time"

type InteractionType int32

const (
	TypeUnspecified       InteractionType = 0
	TypePermissionRequest InteractionType = 1
	TypeQuestion          InteractionType = 2
	TypeNotification      InteractionType = 3
	TypeUserMessage       InteractionType = 4
)

type InteractionStatus int32

const (
	StatusUnspecified InteractionStatus = 0
	StatusPending     InteractionStatus = 1
	StatusResponded   InteractionStatus = 2
	StatusExpired     InteractionStatus = 3
)

type Interaction struct {
	ID          string            `yaml:"id"`
	TaskID      string            `yaml:"task_id"`
	AgentID     string            `yaml:"agent_id"`
	Type        InteractionType   `yaml:"type"`
	Status      InteractionStatus `yaml:"status"`
	Title       string            `yaml:"title"`
	Description string            `yaml:"description"`
	Options     []Option          `yaml:"options"`
	Response    string            `yaml:"response"`
	CreatedAt   time.Time         `yaml:"created_at"`
	RespondedAt *time.Time        `yaml:"responded_at"`
}

type Option struct {
	Label       string `yaml:"label"`
	Value       string `yaml:"value"`
	Description string `yaml:"description"`
}
