package main

import (
	"testing"

	v1 "github.com/kazz187/taskguild/gen/proto/taskguild/v1"
	"github.com/kazz187/taskguild/pkg/shellparse"
)

func TestWildcardToRegex(t *testing.T) {
	tests := []struct {
		pattern  string
		input    string
		expected bool
	}{
		// Exact match
		{"git status", "git status", true},
		{"git status", "git push", false},
		// Trailing wildcard
		{"git *", "git status", true},
		{"git *", "git commit -m hello", true},
		{"git *", "git", false}, // * matches zero or more chars after "git "
		{"cd *", "cd /home/user/project", true},
		// Middle wildcard
		{"docker * build", "docker compose build", true},
		{"docker * build", "docker build", false}, // "docker " + "" + " build" = "docker  build"
		// Multiple wildcards
		{"git * --* *", "git commit --amend foo", true},
		// Wildcard only
		{"*", "anything at all", true},
		{"*", "", true},
		// Special regex characters in pattern are escaped
		{"npm test (coverage)", "npm test (coverage)", true},
		{"file.txt", "fileTtxt", false}, // dot is literal, not regex dot
		{"a+b", "a+b", true},
		{"a+b", "aab", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_vs_"+tt.input, func(t *testing.T) {
			re, err := compileWildcard(tt.pattern)
			if err != nil {
				t.Fatalf("compileWildcard(%q) error: %v", tt.pattern, err)
			}
			matched := re.MatchString(tt.input)
			if matched != tt.expected {
				t.Errorf("compileWildcard(%q).MatchString(%q) = %v, want %v", tt.pattern, tt.input, matched, tt.expected)
			}
		})
	}
}

func TestCompileWildcard_Empty(t *testing.T) {
	_, err := compileWildcard("")
	if err == nil {
		t.Error("expected error for empty pattern")
	}
}

func TestSingleCommandPermissionCache_CheckCommand(t *testing.T) {
	cache := newSingleCommandPermissionCache("test-project", nil)
	cache.Update([]*v1.SingleCommandPermission{
		{Id: "1", Pattern: "git status", Type: "command"},
		{Id: "2", Pattern: "cd *", Type: "command"},
		{Id: "3", Pattern: "npm test *", Type: "command"},
	})

	tests := []struct {
		name    string
		command string
		matched bool
	}{
		{"exact match", "git status", true},
		{"no match", "git push", false},
		{"cd any path", "cd /home/user/project", true},
		{"cd no path", "cd", false},
		{"npm test", "npm test ", true},
		{"npm test with args", "npm test -- --verbose", true},
		{"npm install", "npm install", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched, _ := cache.CheckCommand(tt.command)
			if matched != tt.matched {
				t.Errorf("CheckCommand(%q) = %v, want %v", tt.command, matched, tt.matched)
			}
		})
	}
}

func TestSingleCommandPermissionCache_CheckRedirect(t *testing.T) {
	cache := newSingleCommandPermissionCache("test-project", nil)
	cache.Update([]*v1.SingleCommandPermission{
		{Id: "1", Pattern: "/dev/null", Type: "redirect"},
		{Id: "2", Pattern: "./*", Type: "redirect"},
		{Id: "3", Pattern: "../*", Type: "redirect"},
	})

	tests := []struct {
		name    string
		path    string
		matched bool
	}{
		{"dev null", "/dev/null", true},
		{"relative path", "./output.txt", true},
		{"parent relative", "../output.txt", true},
		{"absolute path", "/etc/passwd", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched, _ := cache.CheckRedirect(tt.path)
			if matched != tt.matched {
				t.Errorf("CheckRedirect(%q) = %v, want %v", tt.path, matched, tt.matched)
			}
		})
	}
}

func TestSingleCommandPermissionCache_CheckAllCommands(t *testing.T) {
	cache := newSingleCommandPermissionCache("test-project", nil)
	cache.Update([]*v1.SingleCommandPermission{
		{Id: "1", Pattern: "cd *", Type: "command"},
		{Id: "2", Pattern: "git status", Type: "command"},
		{Id: "3", Pattern: "/dev/null", Type: "redirect"},
	})

	t.Run("all matched", func(t *testing.T) {
		parsed := shellparse.Parse("cd /home/user && git status")
		allMatched, meta := cache.CheckAllCommands(parsed)
		if !allMatched {
			t.Error("expected all commands to match")
		}
		if len(meta.ParsedCommands) != 2 {
			t.Errorf("expected 2 parsed commands, got %d", len(meta.ParsedCommands))
		}
	})

	t.Run("partial match", func(t *testing.T) {
		parsed := shellparse.Parse("cd /home/user && npm test")
		allMatched, meta := cache.CheckAllCommands(parsed)
		if allMatched {
			t.Error("expected not all commands to match")
		}
		if len(meta.ParsedCommands) < 2 {
			t.Fatalf("expected at least 2 commands, got %d", len(meta.ParsedCommands))
		}
		// First (cd) should match, second (npm test) should not
		if !meta.ParsedCommands[0].Matched {
			t.Error("expected first command (cd) to match")
		}
		if meta.ParsedCommands[1].Matched {
			t.Error("expected second command (npm test) to not match")
		}
		if meta.ParsedCommands[1].SuggestedPattern == "" {
			t.Error("expected suggested pattern for unmatched command")
		}
	})

	t.Run("with redirect", func(t *testing.T) {
		parsed := shellparse.Parse("cd /home/user && git status > /dev/null")
		allMatched, meta := cache.CheckAllCommands(parsed)
		if !allMatched {
			t.Error("expected all to match (including redirect)")
		}
		if len(meta.Redirects) < 1 {
			t.Error("expected at least 1 redirect check")
		}
	})

	t.Run("unmatched redirect", func(t *testing.T) {
		parsed := shellparse.Parse("echo hello > /etc/output.txt")
		allMatched, meta := cache.CheckAllCommands(parsed)
		if allMatched {
			t.Error("expected not all to match (unknown redirect)")
		}
		hasUnmatchedRedirect := false
		for _, r := range meta.Redirects {
			if !r.Matched {
				hasUnmatchedRedirect = true
			}
		}
		if !hasUnmatchedRedirect {
			t.Error("expected at least one unmatched redirect")
		}
	})
}

func TestSuggestCommandPattern(t *testing.T) {
	tests := []struct {
		name     string
		cmd      shellparse.ParsedCommand
		expected string
	}{
		{
			name:     "returns raw command as-is",
			cmd:      shellparse.ParsedCommand{Raw: "git status", Executable: "git", Args: []string{"status"}},
			expected: "git status",
		},
		{
			name:     "returns full command with args",
			cmd:      shellparse.ParsedCommand{Raw: "git checkout -b feature", Executable: "git", Args: []string{"checkout", "-b", "feature"}},
			expected: "git checkout -b feature",
		},
		{
			name:     "npm test with coverage",
			cmd:      shellparse.ParsedCommand{Raw: "npm test --coverage", Executable: "npm", Args: []string{"test", "--coverage"}},
			expected: "npm test --coverage",
		},
		{
			name:     "no args",
			cmd:      shellparse.ParsedCommand{Raw: "ls", Executable: "ls"},
			expected: "ls",
		},
		{
			name:     "unknown command with args",
			cmd:      shellparse.ParsedCommand{Raw: "myapp serve --port 8080", Executable: "myapp", Args: []string{"serve", "--port", "8080"}},
			expected: "myapp serve --port 8080",
		},
		{
			name:     "empty executable falls back to raw",
			cmd:      shellparse.ParsedCommand{Raw: "some raw command"},
			expected: "some raw command",
		},
		{
			name:     "command with command substitution",
			cmd:      shellparse.ParsedCommand{Raw: "cd $(git rev-parse --show-toplevel)/.claude/worktrees/foo", Executable: "cd", Args: []string{"$(git rev-parse --show-toplevel)/.claude/worktrees/foo"}},
			expected: "cd $(git rev-parse --show-toplevel)/.claude/worktrees/foo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SuggestCommandPattern(tt.cmd)
			if result != tt.expected {
				t.Errorf("SuggestCommandPattern() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestSuggestRedirectPattern(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/dev/null", "/dev/null"},
		{"./output.txt", "./output.txt"},
		{"../output.txt", "../output.txt"},
		{"/tmp/foo", "/tmp/foo"},
		{"/etc/passwd", "/etc/passwd"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := SuggestRedirectPattern(tt.path)
			if result != tt.expected {
				t.Errorf("SuggestRedirectPattern(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestSingleCommandPermissionCache_EmptyPattern(t *testing.T) {
	cache := newSingleCommandPermissionCache("test-project", nil)
	cache.Update([]*v1.SingleCommandPermission{
		{Id: "1", Pattern: "", Type: "command"},     // empty - should be skipped
		{Id: "2", Pattern: "git *", Type: "command"}, // valid
	})

	// Should only have 1 valid pattern.
	cache.mu.RLock()
	defer cache.mu.RUnlock()
	if len(cache.patterns) != 1 {
		t.Errorf("expected 1 valid pattern, got %d", len(cache.patterns))
	}
}
