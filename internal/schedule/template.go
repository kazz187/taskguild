package schedule

import (
	"fmt"
	"strings"
	"time"
)

// ExpandTemplate replaces supported placeholders in s with the firing time t.
//
// Supported placeholders:
//
//	{{date}}     -> 2006-01-02
//	{{datetime}} -> 2006-01-02 15:04
//	{{year}}     -> 2006
//	{{month}}    -> 01
//	{{day}}      -> 02
//	{{time}}     -> 15:04
//
// Unknown placeholders are left untouched.
func ExpandTemplate(s string, t time.Time) string {
	if s == "" {
		return s
	}

	replacer := strings.NewReplacer(
		"{{date}}", t.Format("2006-01-02"),
		"{{datetime}}", t.Format("2006-01-02 15:04"),
		"{{year}}", t.Format("2006"),
		"{{month}}", t.Format("01"),
		"{{day}}", t.Format("02"),
		"{{time}}", t.Format("15:04"),
	)

	return replacer.Replace(s)
}

// SupportedPlaceholders returns the list of supported placeholder tokens.
// Useful for surfacing to the UI as a hint.
func SupportedPlaceholders() []string {
	return []string{
		"{{date}}",
		"{{datetime}}",
		"{{year}}",
		"{{month}}",
		"{{day}}",
		"{{time}}",
	}
}

// FormatExample renders a sample of every placeholder for the given time.
// Used in tests and as documentation.
func FormatExample(t time.Time) string {
	var b strings.Builder
	for _, p := range SupportedPlaceholders() {
		fmt.Fprintf(&b, "%s=%s ", p, ExpandTemplate(p, t))
	}

	return strings.TrimSpace(b.String())
}
