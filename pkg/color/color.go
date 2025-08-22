package color

import (
	"fmt"
	"hash/fnv"
	"os"
	"strings"
)

// ANSI color codes
const (
	Reset  = "\033[0m"
	Bold   = "\033[1m"
	Dim    = "\033[2m"
	Italic = "\033[3m"
	Under  = "\033[4m"
)

// Foreground colors
const (
	Black   = "\033[30m"
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	White   = "\033[37m"
)

// Bright foreground colors
const (
	BrightBlack   = "\033[90m"
	BrightRed     = "\033[91m"
	BrightGreen   = "\033[92m"
	BrightYellow  = "\033[93m"
	BrightBlue    = "\033[94m"
	BrightMagenta = "\033[95m"
	BrightCyan    = "\033[96m"
	BrightWhite   = "\033[97m"
)

// 256-color support
func Color256(code int) string {
	return fmt.Sprintf("\033[38;5;%dm", code)
}

// Predefined color palette for agents
var agentColors = []string{
	BrightRed,
	BrightGreen,
	BrightYellow,
	BrightBlue,
	BrightMagenta,
	BrightCyan,
	Red,
	Green,
	Yellow,
	Blue,
	Magenta,
	Cyan,
}

// isColorSupported checks if the terminal supports color output
func isColorSupported() bool {
	// Check NO_COLOR environment variable (https://no-color.org/)
	if os.Getenv("NO_COLOR") != "" {
		return false
	}

	// Check FORCE_COLOR environment variable
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}

	// Check if stderr is a terminal
	term := os.Getenv("TERM")
	if term == "" || term == "dumb" {
		return false
	}

	// Check if we're in a CI environment
	if os.Getenv("CI") != "" {
		return false
	}

	// Check common color support indicators
	colorTerm := os.Getenv("COLORTERM")
	if colorTerm == "truecolor" || colorTerm == "24bit" {
		return true
	}

	// Basic terminal color support check
	if strings.Contains(term, "color") ||
		strings.Contains(term, "ansi") ||
		strings.Contains(term, "xterm") ||
		strings.Contains(term, "screen") {
		return true
	}

	return false
}

// Colorize applies color to text
func Colorize(text, color string) string {
	if !isColorSupported() {
		return text
	}
	return color + text + Reset
}

// GetAgentColor returns a consistent color for the given agent ID
func GetAgentColor(agentID string) string {
	if !isColorSupported() {
		return ""
	}

	// Use hash to consistently assign colors
	h := fnv.New32a()
	h.Write([]byte(agentID))
	hash := h.Sum32()

	colorIndex := int(hash) % len(agentColors)
	return agentColors[colorIndex]
}

// FormatAgentPrefix formats the agent prefix with color
func FormatAgentPrefix(agentID string) string {
	color := GetAgentColor(agentID)
	prefix := fmt.Sprintf("[%s]", agentID)

	if color == "" {
		return prefix
	}

	return Colorize(prefix, color)
}

// ColoredPrintf prints formatted text with a colored agent prefix
func ColoredPrintf(agentID, format string, args ...interface{}) {
	prefix := FormatAgentPrefix(agentID)
	message := fmt.Sprintf(format, args...)
	fmt.Printf("%s %s", prefix, message)
}

// ColoredPrintln prints text with a colored agent prefix and newline
func ColoredPrintln(agentID, text string) {
	prefix := FormatAgentPrefix(agentID)
	fmt.Printf("%s %s\n", prefix, text)
}
