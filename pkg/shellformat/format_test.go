package shellformat

import (
	"strings"
	"testing"

	"mvdan.cc/sh/v3/syntax"
)

func TestFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		opts     []Option
	}{
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
		{
			name:     "whitespace only",
			input:    "   \n\t  ",
			expected: "",
		},
		{
			name:     "simple command stays on one line",
			input:    "echo hello",
			expected: "echo hello",
		},
		{
			name:     "short 2-element && chain stays on one line",
			input:    "echo a && echo b",
			expected: "echo a && echo b",
		},
		{
			name:     "short 2-element pipe stays on one line",
			input:    "cat file | grep foo",
			expected: "cat file | grep foo",
		},
		{
			name:  "long 2-element && chain breaks into lines",
			input: "docker compose build --no-cache --pull --progress=plain 2>&1 && docker compose up -d --remove-orphans --force-recreate",
			expected: `docker compose build --no-cache --pull --progress=plain 2>&1 \
  && docker compose up -d --remove-orphans --force-recreate`,
		},
		{
			name:  "3+ element && chain always breaks",
			input: "echo a && echo b && echo c",
			expected: `echo a \
  && echo b \
  && echo c`,
		},
		{
			name:  "4-element mixed && and || chain",
			input: `echo hello && cd /tmp && ls -la || echo "failed"`,
			expected: `echo hello \
  && cd /tmp \
  && ls -la \
  || echo "failed"`,
		},
		{
			name:  "5-element pipe chain",
			input: "cat file | grep foo | sort | uniq -c | sort -rn",
			expected: `cat file \
  | grep foo \
  | sort \
  | uniq -c \
  | sort -rn`,
		},
		{
			name:  "semicolon separated statements",
			input: "cd /tmp; ls -la; echo done",
			expected: `cd /tmp
ls -la
echo done`,
		},
		{
			name:  "if statement",
			input: `if [ -f /tmp/foo ]; then echo exists; else echo missing; fi`,
			expected: `if [ -f /tmp/foo ]; then
  echo exists
else
  echo missing
fi`,
		},
		{
			name:  "for loop",
			input: "for i in 1 2 3; do echo $i; done",
			expected: `for i in 1 2 3; do
  echo $i
done`,
		},
		{
			name:  "while loop",
			input: `while read line; do echo "$line"; done < input.txt`,
			expected: `while read line; do
  echo "$line"
done < input.txt`,
		},
		{
			name:  "case statement",
			input: `case "$1" in start) echo starting;; stop) echo stopping;; *) echo "usage: $0 {start|stop}";; esac`,
			expected: `case "$1" in
start)
  echo starting
  ;;
stop)
  echo stopping
  ;;
*)
  echo "usage: $0 {start|stop}"
  ;;
esac`,
		},
		{
			name:     "short command substitution stays inline",
			input:    `echo "Hello $(whoami)"`,
			expected: `echo "Hello $(whoami)"`,
		},
		{
			// CmdSubst inside Word is rendered by the standard printer (inline).
			// Expanding CmdSubst with pipes is a future enhancement.
			name:     "command substitution with pipe stays inline for now",
			input:    `echo "$(curl -s https://example.com | jq .name)"`,
			expected: `echo "$(curl -s https://example.com | jq .name)"`,
		},
		{
			name:     "redirect",
			input:    "echo hello > /tmp/out 2>&1",
			expected: "echo hello > /tmp/out 2>&1",
		},
		{
			name:     "here string",
			input:    `cat <<< "hello world"`,
			expected: `cat <<< "hello world"`,
		},
		{
			name:     "process substitution",
			input:    "diff <(sort file1) <(sort file2)",
			expected: "diff <(sort file1) <(sort file2)",
		},
		{
			name:  "function definition",
			input: "foo() { echo hello; echo world; }; foo",
			expected: `foo() {
  echo hello
  echo world
}
foo`,
		},
		{
			name:     "variable assignment with command",
			input:    "ENV=production APP=myapp docker compose up -d",
			expected: "ENV=production APP=myapp docker compose up -d",
		},
		{
			name:  "complex compound command with subshell",
			input: `cd /app && docker compose build --no-cache && docker compose up -d && echo "Deploy done" || (echo "Deploy failed" && exit 1)`,
			expected: `cd /app \
  && docker compose build --no-cache \
  && docker compose up -d \
  && echo "Deploy done" \
  || (echo "Deploy failed" && exit 1)`,
		},
		{
			name:  "complex nested if with command substitution",
			input: `if curl -sf "https://api.example.com/health" > /dev/null 2>&1; then echo "$(date): API is up" >> /var/log/health.log; else echo "$(date): API is DOWN" | mail -s "Alert" admin@example.com && echo "Alert sent"; fi`,
			expected: `if curl -sf "https://api.example.com/health" > /dev/null 2>&1; then
  echo "$(date): API is up" >> /var/log/health.log
else
  echo "$(date): API is DOWN" \
    | mail -s "Alert" admin@example.com \
    && echo "Alert sent"
fi`,
		},
		{
			// The standard printer converts backticks to $() form.
			name:     "backtick command substitution normalized to dollar-paren",
			input:    "echo `date +%Y-%m-%d`",
			expected: "echo $(date +%Y-%m-%d)",
		},
		{
			name:  "array and for loop",
			input: `arr=(1 2 3); for i in "${arr[@]}"; do echo $i; done`,
			expected: `arr=(1 2 3)
for i in "${arr[@]}"; do
  echo $i
done`,
		},
		{
			name:     "parse error returns original",
			input:    `echo "unclosed string`,
			expected: `echo "unclosed string`,
		},
		{
			name:  "with custom indent width 4",
			input: "echo a && echo b && echo c",
			opts:  []Option{WithIndent(4)},
			expected: `echo a \
    && echo b \
    && echo c`,
		},
		{
			name:     "negated command",
			input:    "! test -f /tmp/foo && echo missing",
			expected: "! test -f /tmp/foo && echo missing",
		},
		{
			name:     "background command",
			input:    "sleep 10 & echo started",
			expected: "sleep 10 &\necho started",
		},
		{
			name:  "deeply nested binary chain",
			input: "a && b && c && d && e && f",
			expected: `a \
  && b \
  && c \
  && d \
  && e \
  && f`,
		},
		{
			name:  "mixed pipe and && chain",
			input: "cat file | grep pattern | awk '{print $1}' && echo done || echo failed",
			expected: `cat file \
  | grep pattern \
  | awk '{print $1}' \
  && echo done \
  || echo failed`,
		},
		{
			name:  "if with elif",
			input: `if [ "$1" = "a" ]; then echo A; elif [ "$1" = "b" ]; then echo B; else echo other; fi`,
			expected: `if [ "$1" = "a" ]; then
  echo A
elif [ "$1" = "b" ]; then
  echo B
else
  echo other
fi`,
		},
		{
			name:  "2-element || with subshell always expands",
			input: `which buf || (cat Makefile 2> /dev/null | head -50)`,
			expected: `which buf \
  || (cat Makefile 2> /dev/null | head -50)`,
		},
		{
			name:  "long command with redirect piped to head",
			input: `ls /home/ubuntu/taskguild/taskguild/.claude/worktrees/mb7frh_setup-script-execution/proto/node_modules/.bin/ 2> /dev/null | head -20`,
			expected: `ls /home/ubuntu/taskguild/taskguild/.claude/worktrees/mb7frh_setup-script-execution/proto/node_modules/.bin/ 2> /dev/null \
  | head -20`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Format(tt.input, tt.opts...)
			if err != nil {
				t.Fatalf("Format() returned error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("Format(%q)\n  got:\n%s\n\n  expected:\n%s", tt.input, got, tt.expected)
			}
		})
	}
}

// TestFormatOutputIsValidBash verifies that the formatted output
// can be re-parsed by the shell parser (roundtrip check).
func TestFormatOutputIsValidBash(t *testing.T) {
	inputs := []string{
		"echo hello && cd /tmp && ls -la || echo failed",
		"cat file | grep foo | sort | uniq -c | sort -rn",
		"cd /tmp; ls -la; echo done",
		`if [ -f /tmp/foo ]; then echo exists; else echo missing; fi`,
		"for i in 1 2 3; do echo $i; done",
		`while read line; do echo "$line"; done < input.txt`,
		`case "$1" in start) echo starting;; stop) echo stopping;; esac`,
		`cd /app && docker compose build --no-cache && docker compose up -d && echo "Deploy done" || (echo "Deploy failed" && exit 1)`,
		`foo() { echo hello; echo world; }; foo`,
		`ENV=production APP=myapp docker compose up -d`,
		`echo "$(curl -s https://example.com | jq .name)" > output.txt`,
		`diff <(sort file1) <(sort file2)`,
		`if curl -sf "https://api.example.com/health" > /dev/null 2>&1; then echo "$(date): API is up" >> /var/log/health.log; else echo "$(date): API is DOWN" | mail -s "Alert" admin@example.com && echo "Alert sent"; fi`,
		`a && b && c && d && e && f`,
		`cat file | grep pattern | awk '{print $1}' && echo done || echo failed`,
		`arr=(1 2 3); for i in "${arr[@]}"; do echo $i; done`,
		`which buf || (cat Makefile 2> /dev/null | head -50)`,
		`ls /home/ubuntu/taskguild/taskguild/.claude/worktrees/mb7frh_setup-script-execution/proto/node_modules/.bin/ 2> /dev/null | head -20`,
	}

	parser := syntax.NewParser(syntax.KeepComments(true))

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			formatted, err := Format(input)
			if err != nil {
				t.Fatalf("Format() returned error: %v", err)
			}

			// Verify the output can be re-parsed.
			_, err = parser.Parse(strings.NewReader(formatted), "")
			if err != nil {
				t.Errorf("Formatted output is not valid bash:\n%s\n\nParse error: %v", formatted, err)
			}
		})
	}
}
