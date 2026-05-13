package schedule

import (
	"testing"
	"time"
)

func TestExpandTemplate(t *testing.T) {
	at := time.Date(2026, 5, 13, 9, 30, 0, 0, time.UTC)

	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"no placeholders", "no placeholders"},
		{"[scheduled] {{date}}", "[scheduled] 2026-05-13"},
		{"{{datetime}}", "2026-05-13 09:30"},
		{"{{year}}-{{month}}-{{day}}", "2026-05-13"},
		{"at {{time}}", "at 09:30"},
		{"{{date}} - {{date}}", "2026-05-13 - 2026-05-13"},
		{"{{unknown}}", "{{unknown}}"},
		{"prefix {{datetime}} suffix", "prefix 2026-05-13 09:30 suffix"},
	}
	for _, c := range cases {
		got := ExpandTemplate(c.in, at)
		if got != c.want {
			t.Errorf("ExpandTemplate(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestSupportedPlaceholders(t *testing.T) {
	got := SupportedPlaceholders()
	if len(got) == 0 {
		t.Fatal("expected at least one placeholder")
	}
}
