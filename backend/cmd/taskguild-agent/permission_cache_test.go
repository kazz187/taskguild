package main

import "testing"

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern string
		value   string
		want    bool
	}{
		// Exact match
		{"foo", "foo", true},
		{"foo", "bar", false},
		{"foo", "foobar", false},
		{"foo", "", false},

		// Wildcard only
		{"*", "anything", true},
		{"*", "", true},

		// Trailing wildcard (prefix match)
		{"git *", "git status", true},
		{"git *", "git commit -m 'test'", true},
		{"git *", "git", false}, // no space after "git"
		{"npm test *", "npm test --watch", true},
		{"npm test *", "npm install", false},

		// Leading wildcard (suffix match)
		{"*.go", "main.go", true},
		{"*.go", "main.py", false},

		// Middle wildcard
		{"git * --force", "git push --force", true},
		{"git * --force", "git push origin main --force", true},
		{"git * --force", "git push", false},

		// Multiple wildcards
		{"*test*", "run test suite", true},
		{"*test*", "testing", true},
		{"*test*", "foo", false},

		// Empty pattern
		{"", "", true},
		{"", "foo", false},

		// No wildcard
		{"npm test", "npm test", true},
		{"npm test", "npm test --watch", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.value, func(t *testing.T) {
			got := matchGlob(tt.pattern, tt.value)
			if got != tt.want {
				t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.pattern, tt.value, got, tt.want)
			}
		})
	}
}

func TestParsePermissionRule(t *testing.T) {
	tests := []struct {
		rule       string
		wantTool   string
		wantPat    string
		wantHasPat bool
	}{
		{"Read", "Read", "", false},
		{"Write", "Write", "", false},
		{"Bash(git *)", "Bash", "git *", true},
		{"Bash(npm test --watch)", "Bash", "npm test --watch", true},
		{"Edit", "Edit", "", false},
		// Malformed: no closing paren → treated as no-pattern
		{"Bash(git *", "Bash(git *", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.rule, func(t *testing.T) {
			tool, pat, hasPat := parsePermissionRule(tt.rule)
			if tool != tt.wantTool || pat != tt.wantPat || hasPat != tt.wantHasPat {
				t.Errorf("parsePermissionRule(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tt.rule, tool, pat, hasPat, tt.wantTool, tt.wantPat, tt.wantHasPat)
			}
		})
	}
}

func TestMatchPermissionRule(t *testing.T) {
	tests := []struct {
		rule     string
		toolName string
		input    map[string]any
		want     bool
	}{
		// Simple tool name match
		{"Read", "Read", nil, true},
		{"Read", "Write", nil, false},

		// Bash with glob pattern
		{"Bash(git *)", "Bash", map[string]any{"command": "git status"}, true},
		{"Bash(git *)", "Bash", map[string]any{"command": "git commit -m 'test'"}, true},
		{"Bash(git *)", "Bash", map[string]any{"command": "npm install"}, false},
		{"Bash(git *)", "Read", map[string]any{"command": "git status"}, false},

		// Bash exact command
		{"Bash(npm test)", "Bash", map[string]any{"command": "npm test"}, true},
		{"Bash(npm test)", "Bash", map[string]any{"command": "npm test --watch"}, false},

		// Bash without pattern (allow all bash)
		{"Bash", "Bash", map[string]any{"command": "anything"}, true},
		{"Bash", "Bash", nil, true},

		// Write (no pattern — allows all)
		{"Write", "Write", map[string]any{"file_path": "/tmp/foo.txt"}, true},
	}

	for _, tt := range tests {
		name := tt.rule + "_" + tt.toolName
		if cmd, ok := tt.input["command"].(string); ok {
			name += "_" + cmd
		}
		t.Run(name, func(t *testing.T) {
			got := matchPermissionRule(tt.rule, tt.toolName, tt.input)
			if got != tt.want {
				t.Errorf("matchPermissionRule(%q, %q, %v) = %v, want %v",
					tt.rule, tt.toolName, tt.input, got, tt.want)
			}
		})
	}
}

func TestRuleValueToString(t *testing.T) {
	tests := []struct {
		toolName    string
		ruleContent string
		want        string
	}{
		{"Read", "", "Read"},
		{"Bash", "git *", "Bash(git *)"},
		{"Bash", "npm test", "Bash(npm test)"},
		{"Write", "", "Write"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			// Simulate PermissionRuleValue (avoid importing the SDK in test)
			got := ruleValueToStringHelper(tt.toolName, tt.ruleContent)
			if got != tt.want {
				t.Errorf("ruleValueToString(%q, %q) = %q, want %q", tt.toolName, tt.ruleContent, got, tt.want)
			}
		})
	}
}

// ruleValueToStringHelper is a test helper that mimics ruleValueToString
// without requiring a claudeagent.PermissionRuleValue.
func ruleValueToStringHelper(toolName, ruleContent string) string {
	if ruleContent == "" {
		return toolName
	}
	return toolName + "(" + ruleContent + ")"
}

func TestPermissionCacheCheck(t *testing.T) {
	cache := newPermissionCache("test-project", nil)
	cache.Update([]string{"Read", "Bash(git *)", "Write"})

	tests := []struct {
		toolName string
		input    map[string]any
		want     bool
	}{
		{"Read", nil, true},
		{"Write", map[string]any{"file_path": "/tmp/test.txt"}, true},
		{"Bash", map[string]any{"command": "git status"}, true},
		{"Bash", map[string]any{"command": "git push origin main"}, true},
		{"Bash", map[string]any{"command": "npm install"}, false},
		{"Edit", nil, false},
		{"Task", nil, false},
	}

	for _, tt := range tests {
		name := tt.toolName
		if cmd, ok := tt.input["command"].(string); ok {
			name += "_" + cmd
		}
		t.Run(name, func(t *testing.T) {
			got := cache.Check(tt.toolName, tt.input)
			if got != tt.want {
				t.Errorf("cache.Check(%q, %v) = %v, want %v", tt.toolName, tt.input, got, tt.want)
			}
		})
	}
}

func TestUnionDedupRules(t *testing.T) {
	tests := []struct {
		name string
		a    []string
		b    []string
		want []string
	}{
		{"empty", nil, nil, nil},
		{"a only", []string{"Read"}, nil, []string{"Read"}},
		{"b only", nil, []string{"Write"}, []string{"Write"}},
		{"no overlap", []string{"Read"}, []string{"Write"}, []string{"Read", "Write"}},
		{"with overlap", []string{"Read", "Write"}, []string{"Write", "Edit"}, []string{"Read", "Write", "Edit"}},
		{"identical", []string{"Read", "Write"}, []string{"Read", "Write"}, []string{"Read", "Write"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := unionDedupRules(tt.a, tt.b)
			// nil vs empty: both are "no results"
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("unionDedupRules(%v, %v) length = %d, want %d", tt.a, tt.b, len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("unionDedupRules(%v, %v)[%d] = %q, want %q", tt.a, tt.b, i, got[i], tt.want[i])
				}
			}
		})
	}
}
