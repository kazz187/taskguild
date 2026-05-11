package pushnotification

import "testing"

func TestNormalizeVAPIDSubscriber(t *testing.T) {
	tests := []struct {
		name    string
		contact string
		want    string
	}{
		{
			name:    "bare email",
			contact: "admin@taskguild.dev",
			want:    "admin@taskguild.dev",
		},
		{
			name:    "mailto email",
			contact: "mailto:admin@taskguild.dev",
			want:    "admin@taskguild.dev",
		},
		{
			name:    "https url",
			contact: "https://taskguild.dev/contact",
			want:    "https://taskguild.dev/contact",
		},
		{
			name:    "trim whitespace",
			contact: " mailto:admin@taskguild.dev ",
			want:    "admin@taskguild.dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeVAPIDSubscriber(tt.contact); got != tt.want {
				t.Fatalf("normalizeVAPIDSubscriber() = %q, want %q", got, tt.want)
			}
		})
	}
}
