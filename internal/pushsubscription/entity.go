package pushsubscription

import "time"

type Subscription struct {
	ID        string    `yaml:"id"`
	Endpoint  string    `yaml:"endpoint"`
	P256dhKey string    `yaml:"p256dh_key"`
	AuthKey   string    `yaml:"auth_key"`
	CreatedAt time.Time `yaml:"created_at"`
}
