package main

import (
	"strings"
	"testing"
)

func TestIsClaudeInternalPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{".claude/agents/foo.md", true},
		{".claude/settings.json", true},
		{".claude", true},
		{"claude/agents/foo.md", false},
		{"src/main.go", false},
		{"package-lock.json", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isClaudeInternalPath(tt.path); got != tt.want {
			t.Errorf("isClaudeInternalPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

// TestPorcelainParsing verifies that git status --porcelain lines are parsed correctly,
// preserving the leading dot in ".claude/" paths.
func TestPorcelainParsing(t *testing.T) {
	// Simulate git status --porcelain output.
	// Format: XY filename, where X=index status, Y=worktree status, position 2=space.
	porcelainOutput := strings.Join([]string{
		" M .claude/agents/backend-guide.md",
		" M .claude/settings.json",
		"M  src/main.go",
		"?? newfile.txt",
		" M package-lock.json",
	}, "\n")

	lines := strings.Split(strings.TrimRight(porcelainOutput, "\n"), "\n")

	var changedFiles []string

	for _, line := range lines {
		if len(line) < 4 {
			continue
		}

		filePath := line[3:]
		if isClaudeInternalPath(filePath) {
			continue
		}

		changedFiles = append(changedFiles, filePath)
	}

	// .claude/ files should be filtered out; only non-.claude files remain.
	expected := []string{"src/main.go", "newfile.txt", "package-lock.json"}
	if len(changedFiles) != len(expected) {
		t.Fatalf("got %d changed files %v, want %d %v", len(changedFiles), changedFiles, len(expected), expected)
	}

	for i, f := range changedFiles {
		if f != expected[i] {
			t.Errorf("changedFiles[%d] = %q, want %q", i, f, expected[i])
		}
	}
}
