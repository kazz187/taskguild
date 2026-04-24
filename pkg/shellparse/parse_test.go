package shellparse

import (
	"testing"
)

func TestParse_SimpleCommand(t *testing.T) {
	result := Parse("git status")

	if len(result.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(result.Commands))
	}

	cmd := result.Commands[0]
	if cmd.Executable != "git" {
		t.Errorf("expected executable 'git', got %q", cmd.Executable)
	}

	if len(cmd.Args) != 1 || cmd.Args[0] != "status" {
		t.Errorf("expected args [status], got %v", cmd.Args)
	}

	if cmd.Raw != "git status" {
		t.Errorf("expected raw 'git status', got %q", cmd.Raw)
	}
}

func TestParse_AndOperator(t *testing.T) {
	result := Parse("cd /home && git status && npm test")

	if len(result.Commands) != 3 {
		t.Fatalf("expected 3 commands, got %d", len(result.Commands))
	}

	expected := []struct {
		executable string
		raw        string
	}{
		{"cd", "cd /home"},
		{"git", "git status"},
		{"npm", "npm test"},
	}

	for i, exp := range expected {
		if result.Commands[i].Executable != exp.executable {
			t.Errorf("command[%d]: expected executable %q, got %q", i, exp.executable, result.Commands[i].Executable)
		}

		if result.Commands[i].Raw != exp.raw {
			t.Errorf("command[%d]: expected raw %q, got %q", i, exp.raw, result.Commands[i].Raw)
		}
	}
}

func TestParse_OrOperator(t *testing.T) {
	result := Parse("test -f file.txt || echo 'not found'")

	if len(result.Commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(result.Commands))
	}

	if result.Commands[0].Executable != "test" {
		t.Errorf("expected 'test', got %q", result.Commands[0].Executable)
	}

	if result.Commands[1].Executable != "echo" {
		t.Errorf("expected 'echo', got %q", result.Commands[1].Executable)
	}
}

func TestParse_Semicolon(t *testing.T) {
	result := Parse("echo hello; echo world")

	if len(result.Commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(result.Commands))
	}

	if result.Commands[0].Executable != "echo" {
		t.Errorf("expected 'echo', got %q", result.Commands[0].Executable)
	}

	if result.Commands[1].Executable != "echo" {
		t.Errorf("expected 'echo', got %q", result.Commands[1].Executable)
	}
}

func TestParse_Pipe(t *testing.T) {
	result := Parse("cat file.txt | grep pattern | wc -l")

	if len(result.Commands) != 3 {
		t.Fatalf("expected 3 commands, got %d", len(result.Commands))
	}

	executables := []string{"cat", "grep", "wc"}
	for i, exp := range executables {
		if result.Commands[i].Executable != exp {
			t.Errorf("command[%d]: expected %q, got %q", i, exp, result.Commands[i].Executable)
		}
	}
}

func TestParse_Background(t *testing.T) {
	result := Parse("sleep 10 & echo done")

	if len(result.Commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(result.Commands))
	}

	if result.Commands[0].Executable != "sleep" {
		t.Errorf("expected 'sleep', got %q", result.Commands[0].Executable)
	}

	if result.Commands[1].Executable != "echo" {
		t.Errorf("expected 'echo', got %q", result.Commands[1].Executable)
	}
}

func TestParse_Redirect(t *testing.T) {
	result := Parse("echo hello > /dev/null")

	if len(result.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(result.Commands))
	}

	cmd := result.Commands[0]
	if cmd.Executable != "echo" {
		t.Errorf("expected 'echo', got %q", cmd.Executable)
	}

	if len(cmd.Redirects) != 1 {
		t.Fatalf("expected 1 redirect, got %d", len(cmd.Redirects))
	}

	if cmd.Redirects[0].Op != ">" {
		t.Errorf("expected redirect op '>', got %q", cmd.Redirects[0].Op)
	}

	if cmd.Redirects[0].Path != "/dev/null" {
		t.Errorf("expected redirect path '/dev/null', got %q", cmd.Redirects[0].Path)
	}
}

func TestParse_RedirectAppend(t *testing.T) {
	result := Parse("echo hello >> ./output.txt")

	if len(result.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(result.Commands))
	}

	cmd := result.Commands[0]
	if len(cmd.Redirects) != 1 {
		t.Fatalf("expected 1 redirect, got %d", len(cmd.Redirects))
	}

	if cmd.Redirects[0].Op != ">>" {
		t.Errorf("expected redirect op '>>', got %q", cmd.Redirects[0].Op)
	}

	if cmd.Redirects[0].Path != "./output.txt" {
		t.Errorf("expected redirect path './output.txt', got %q", cmd.Redirects[0].Path)
	}
}

func TestParse_StderrRedirect(t *testing.T) {
	result := Parse("make build 2>/dev/null")

	if len(result.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(result.Commands))
	}

	cmd := result.Commands[0]
	if len(cmd.Redirects) != 1 {
		t.Fatalf("expected 1 redirect, got %d", len(cmd.Redirects))
	}

	if cmd.Redirects[0].Path != "/dev/null" {
		t.Errorf("expected redirect path '/dev/null', got %q", cmd.Redirects[0].Path)
	}
}

func TestParse_CommandSubstitution(t *testing.T) {
	result := Parse("echo $(git rev-parse HEAD)")

	// Should find both "echo $(git rev-parse HEAD)" and "git rev-parse HEAD"
	if len(result.Commands) < 2 {
		t.Fatalf("expected at least 2 commands, got %d: %+v", len(result.Commands), result.Commands)
	}

	foundEcho := false
	foundGit := false

	for _, cmd := range result.Commands {
		if cmd.Executable == "echo" {
			foundEcho = true
		}

		if cmd.Executable == "git" {
			foundGit = true
		}
	}

	if !foundEcho {
		t.Error("expected to find 'echo' command")
	}

	if !foundGit {
		t.Error("expected to find 'git' command from command substitution")
	}
}

func TestParse_BacktickSubstitution(t *testing.T) {
	result := Parse("echo `whoami`")

	if len(result.Commands) < 2 {
		t.Fatalf("expected at least 2 commands, got %d: %+v", len(result.Commands), result.Commands)
	}

	foundWhoami := false

	for _, cmd := range result.Commands {
		if cmd.Executable == "whoami" {
			foundWhoami = true
		}
	}

	if !foundWhoami {
		t.Error("expected to find 'whoami' command from backtick substitution")
	}
}

func TestParse_ForLoop(t *testing.T) {
	result := Parse("for f in *.go; do echo $f; done")

	foundEcho := false

	for _, cmd := range result.Commands {
		if cmd.Executable == "echo" {
			foundEcho = true
		}
	}

	if !foundEcho {
		t.Error("expected to find 'echo' command inside for loop")
	}
}

func TestParse_WhileLoop(t *testing.T) {
	result := Parse("while true; do echo running; sleep 1; done")

	executables := map[string]bool{}
	for _, cmd := range result.Commands {
		executables[cmd.Executable] = true
	}

	for _, exp := range []string{"true", "echo", "sleep"} {
		if !executables[exp] {
			t.Errorf("expected to find %q command inside while loop", exp)
		}
	}
}

func TestParse_IfClause(t *testing.T) {
	result := Parse("if test -f foo; then echo exists; else echo missing; fi")

	executables := map[string]bool{}
	for _, cmd := range result.Commands {
		executables[cmd.Executable] = true
	}

	for _, exp := range []string{"test", "echo"} {
		if !executables[exp] {
			t.Errorf("expected to find %q command inside if clause", exp)
		}
	}
}

func TestParse_Subshell(t *testing.T) {
	result := Parse("(cd /tmp && ls)")

	if len(result.Commands) != 2 {
		t.Fatalf("expected 2 commands, got %d: %+v", len(result.Commands), result.Commands)
	}

	if result.Commands[0].Executable != "cd" {
		t.Errorf("expected 'cd', got %q", result.Commands[0].Executable)
	}

	if result.Commands[1].Executable != "ls" {
		t.Errorf("expected 'ls', got %q", result.Commands[1].Executable)
	}
}

func TestParse_Block(t *testing.T) {
	result := Parse("{ echo a; echo b; }")

	if len(result.Commands) != 2 {
		t.Fatalf("expected 2 commands, got %d: %+v", len(result.Commands), result.Commands)
	}
}

func TestParse_ComplexOneLiner(t *testing.T) {
	result := Parse("cd /home/user/project && git status && npm test > /dev/null 2>&1")

	if len(result.Commands) < 3 {
		t.Fatalf("expected at least 3 commands, got %d: %+v", len(result.Commands), result.Commands)
	}

	if result.Commands[0].Executable != "cd" {
		t.Errorf("expected 'cd', got %q", result.Commands[0].Executable)
	}

	if result.Commands[1].Executable != "git" {
		t.Errorf("expected 'git', got %q", result.Commands[1].Executable)
	}

	if result.Commands[2].Executable != "npm" {
		t.Errorf("expected 'npm', got %q", result.Commands[2].Executable)
	}
	// npm test should have redirects
	if len(result.Commands[2].Redirects) < 1 {
		t.Errorf("expected redirects on 'npm test', got %d", len(result.Commands[2].Redirects))
	}
}

func TestParse_CaseClause(t *testing.T) {
	result := Parse("case $1 in start) echo starting;; stop) echo stopping;; esac")

	foundEcho := false

	for _, cmd := range result.Commands {
		if cmd.Executable == "echo" {
			foundEcho = true
		}
	}

	if !foundEcho {
		t.Error("expected to find 'echo' command inside case clause")
	}
}

func TestParse_EmptyInput(t *testing.T) {
	result := Parse("")
	if len(result.Commands) != 0 {
		t.Errorf("expected 0 commands for empty input, got %d", len(result.Commands))
	}
}

func TestParse_WhitespaceOnly(t *testing.T) {
	result := Parse("   \t\n  ")
	if len(result.Commands) != 0 {
		t.Errorf("expected 0 commands for whitespace input, got %d", len(result.Commands))
	}
}

func TestParse_Original(t *testing.T) {
	input := "git status && git diff"

	result := Parse(input)
	if result.Original != input {
		t.Errorf("expected original %q, got %q", input, result.Original)
	}
}

func TestParse_VariableAssignment(t *testing.T) {
	result := Parse("FOO=bar echo hello")

	if len(result.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(result.Commands))
	}

	if result.Commands[0].Executable != "echo" {
		t.Errorf("expected executable 'echo', got %q", result.Commands[0].Executable)
	}
}

func TestParse_Export(t *testing.T) {
	result := Parse("export PATH=/usr/bin:$PATH")

	if len(result.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(result.Commands))
	}

	if result.Commands[0].Executable != "export" {
		t.Errorf("expected executable 'export', got %q", result.Commands[0].Executable)
	}
}

func TestParse_MixedOperators(t *testing.T) {
	result := Parse("echo a && echo b || echo c; echo d")

	if len(result.Commands) != 4 {
		t.Fatalf("expected 4 commands, got %d: %+v", len(result.Commands), result.Commands)
	}

	for _, cmd := range result.Commands {
		if cmd.Executable != "echo" {
			t.Errorf("expected all commands to be 'echo', got %q", cmd.Executable)
		}
	}
}

func TestParse_NestedCommandSubstitution(t *testing.T) {
	result := Parse("echo $(cat $(find . -name '*.txt'))")

	executables := map[string]bool{}
	for _, cmd := range result.Commands {
		executables[cmd.Executable] = true
	}

	for _, exp := range []string{"echo", "cat", "find"} {
		if !executables[exp] {
			t.Errorf("expected to find %q in nested command substitution", exp)
		}
	}
}

func TestParse_PipeWithRedirect(t *testing.T) {
	result := Parse("cat file | sort > sorted.txt")

	if len(result.Commands) != 2 {
		t.Fatalf("expected 2 commands, got %d: %+v", len(result.Commands), result.Commands)
	}

	if result.Commands[0].Executable != "cat" {
		t.Errorf("expected 'cat', got %q", result.Commands[0].Executable)
	}

	if result.Commands[1].Executable != "sort" {
		t.Errorf("expected 'sort', got %q", result.Commands[1].Executable)
	}
}

func TestParse_MultipleRedirects(t *testing.T) {
	result := Parse("./build.sh > /dev/null 2>&1")

	if len(result.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(result.Commands))
	}

	if len(result.Commands[0].Redirects) != 2 {
		t.Errorf("expected 2 redirects, got %d: %+v", len(result.Commands[0].Redirects), result.Commands[0].Redirects)
	}
}

func TestParse_HereString(t *testing.T) {
	result := Parse("cat <<< 'hello world'")

	if len(result.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(result.Commands))
	}

	if result.Commands[0].Executable != "cat" {
		t.Errorf("expected 'cat', got %q", result.Commands[0].Executable)
	}
}

func TestParse_ProcessSubstitution(t *testing.T) {
	result := Parse("diff <(echo a) <(echo b)")

	// Should find diff and the echo commands inside process substitution
	foundDiff := false

	for _, cmd := range result.Commands {
		if cmd.Executable == "diff" {
			foundDiff = true
		}
	}

	if !foundDiff {
		t.Error("expected to find 'diff' command")
	}
}
