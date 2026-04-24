package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempAgentMD(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()

	fp := filepath.Join(dir, "test-agent.md")
	if err := os.WriteFile(fp, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	return fp
}

func TestParseAgentMDFile_BlockScalarLiteral(t *testing.T) {
	content := `---
name: gopls-agent
description: |
  Use gopls for precise Go code exploration.
  Prefer gopls over grep/glob for Go code.
---

Agent prompt here.
`
	fp := writeTempAgentMD(t, content)

	parsed, err := parseAgentMDFile(fp)
	if err != nil {
		t.Fatal(err)
	}

	expected := "Use gopls for precise Go code exploration.\nPrefer gopls over grep/glob for Go code."
	if parsed.Description != expected {
		t.Errorf("description mismatch\ngot:  %q\nwant: %q", parsed.Description, expected)
	}

	if parsed.Name != "gopls-agent" {
		t.Errorf("name = %q, want %q", parsed.Name, "gopls-agent")
	}

	if parsed.Prompt != "Agent prompt here." {
		t.Errorf("prompt = %q, want %q", parsed.Prompt, "Agent prompt here.")
	}
}

func TestParseAgentMDFile_BlockScalarFollowedByOtherFields(t *testing.T) {
	content := `---
name: my-agent
description: |
  Line one.
  Line two.
model: sonnet
---
`
	fp := writeTempAgentMD(t, content)

	parsed, err := parseAgentMDFile(fp)
	if err != nil {
		t.Fatal(err)
	}

	expected := "Line one.\nLine two."
	if parsed.Description != expected {
		t.Errorf("description mismatch\ngot:  %q\nwant: %q", parsed.Description, expected)
	}

	if parsed.Model != "sonnet" {
		t.Errorf("model = %q, want %q", parsed.Model, "sonnet")
	}
}

func TestParseAgentMDFile_BlockScalarAsLastField(t *testing.T) {
	content := `---
name: my-agent
description: |
  This is the last field.
  Parsed correctly.
---
`
	fp := writeTempAgentMD(t, content)

	parsed, err := parseAgentMDFile(fp)
	if err != nil {
		t.Fatal(err)
	}

	expected := "This is the last field.\nParsed correctly."
	if parsed.Description != expected {
		t.Errorf("description mismatch\ngot:  %q\nwant: %q", parsed.Description, expected)
	}
}

func TestParseAgentMDFile_SingleLineDescription(t *testing.T) {
	content := `---
name: simple-agent
description: A simple one-line description
model: opus
---
`
	fp := writeTempAgentMD(t, content)

	parsed, err := parseAgentMDFile(fp)
	if err != nil {
		t.Fatal(err)
	}

	if parsed.Description != "A simple one-line description" {
		t.Errorf("description = %q, want %q", parsed.Description, "A simple one-line description")
	}
}

func TestParseAgentMDFile_BlockScalarThenList(t *testing.T) {
	content := `---
name: my-agent
description: |
  Multi-line desc.
  Second line.
skills:
  - skill-a
  - skill-b
---
`
	fp := writeTempAgentMD(t, content)

	parsed, err := parseAgentMDFile(fp)
	if err != nil {
		t.Fatal(err)
	}

	expected := "Multi-line desc.\nSecond line."
	if parsed.Description != expected {
		t.Errorf("description mismatch\ngot:  %q\nwant: %q", parsed.Description, expected)
	}

	if len(parsed.Skills) != 2 || parsed.Skills[0] != "skill-a" || parsed.Skills[1] != "skill-b" {
		t.Errorf("skills = %v, want [skill-a, skill-b]", parsed.Skills)
	}
}

func TestParseAgentMDFile_EmptyBlockScalar(t *testing.T) {
	content := `---
name: my-agent
description: |
model: sonnet
---
`
	fp := writeTempAgentMD(t, content)

	parsed, err := parseAgentMDFile(fp)
	if err != nil {
		t.Fatal(err)
	}

	if parsed.Description != "" {
		t.Errorf("description = %q, want empty string", parsed.Description)
	}

	if parsed.Model != "sonnet" {
		t.Errorf("model = %q, want %q", parsed.Model, "sonnet")
	}
}
