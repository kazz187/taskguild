package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempSkillMD(t *testing.T, content string) (filePath string, dirName string) {
	t.Helper()
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "test-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fp := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(fp, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return fp, "test-skill"
}

func TestParseSkillMDFile_BlockScalarLiteral(t *testing.T) {
	content := `---
name: gopls-explorer
description: |
  Use gopls for precise Go code exploration.
  Prefer gopls over grep/glob for Go code.
---

Body content here.
`
	fp, dirName := writeTempSkillMD(t, content)
	parsed, err := parseSkillMDFile(fp, dirName)
	if err != nil {
		t.Fatal(err)
	}

	expected := "Use gopls for precise Go code exploration.\nPrefer gopls over grep/glob for Go code."
	if parsed.Description != expected {
		t.Errorf("description mismatch\ngot:  %q\nwant: %q", parsed.Description, expected)
	}
	if parsed.Name != "gopls-explorer" {
		t.Errorf("name = %q, want %q", parsed.Name, "gopls-explorer")
	}
	if parsed.Content != "Body content here." {
		t.Errorf("content = %q, want %q", parsed.Content, "Body content here.")
	}
}

func TestParseSkillMDFile_BlockScalarFollowedByOtherFields(t *testing.T) {
	content := `---
name: my-skill
description: |
  Line one.
  Line two.
  Line three.
model: sonnet
---
`
	fp, dirName := writeTempSkillMD(t, content)
	parsed, err := parseSkillMDFile(fp, dirName)
	if err != nil {
		t.Fatal(err)
	}

	expected := "Line one.\nLine two.\nLine three."
	if parsed.Description != expected {
		t.Errorf("description mismatch\ngot:  %q\nwant: %q", parsed.Description, expected)
	}
	if parsed.Model != "sonnet" {
		t.Errorf("model = %q, want %q", parsed.Model, "sonnet")
	}
}

func TestParseSkillMDFile_BlockScalarAsLastField(t *testing.T) {
	content := `---
name: my-skill
description: |
  This is the last field.
  It should be parsed correctly.
---
`
	fp, dirName := writeTempSkillMD(t, content)
	parsed, err := parseSkillMDFile(fp, dirName)
	if err != nil {
		t.Fatal(err)
	}

	expected := "This is the last field.\nIt should be parsed correctly."
	if parsed.Description != expected {
		t.Errorf("description mismatch\ngot:  %q\nwant: %q", parsed.Description, expected)
	}
}

func TestParseSkillMDFile_SingleLineDescription(t *testing.T) {
	content := `---
name: simple-skill
description: A simple one-line description
model: opus
---

Some body.
`
	fp, dirName := writeTempSkillMD(t, content)
	parsed, err := parseSkillMDFile(fp, dirName)
	if err != nil {
		t.Fatal(err)
	}

	if parsed.Description != "A simple one-line description" {
		t.Errorf("description = %q, want %q", parsed.Description, "A simple one-line description")
	}
	if parsed.Model != "opus" {
		t.Errorf("model = %q, want %q", parsed.Model, "opus")
	}
}

func TestParseSkillMDFile_BlockScalarThenList(t *testing.T) {
	content := `---
name: my-skill
description: |
  Multi-line desc.
  Second line.
allowed-tools:
  - Read
  - Write
---
`
	fp, dirName := writeTempSkillMD(t, content)
	parsed, err := parseSkillMDFile(fp, dirName)
	if err != nil {
		t.Fatal(err)
	}

	expected := "Multi-line desc.\nSecond line."
	if parsed.Description != expected {
		t.Errorf("description mismatch\ngot:  %q\nwant: %q", parsed.Description, expected)
	}
	if len(parsed.AllowedTools) != 2 || parsed.AllowedTools[0] != "Read" || parsed.AllowedTools[1] != "Write" {
		t.Errorf("allowed-tools = %v, want [Read, Write]", parsed.AllowedTools)
	}
}

func TestParseSkillMDFile_EmptyBlockScalar(t *testing.T) {
	content := `---
name: my-skill
description: |
model: sonnet
---
`
	fp, dirName := writeTempSkillMD(t, content)
	parsed, err := parseSkillMDFile(fp, dirName)
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

func TestParseSkillMDFile_BlockScalarWithBlankLines(t *testing.T) {
	content := `---
name: my-skill
description: |
  First paragraph.

  Second paragraph.
---
`
	fp, dirName := writeTempSkillMD(t, content)
	parsed, err := parseSkillMDFile(fp, dirName)
	if err != nil {
		t.Fatal(err)
	}

	expected := "First paragraph.\n\nSecond paragraph."
	if parsed.Description != expected {
		t.Errorf("description mismatch\ngot:  %q\nwant: %q", parsed.Description, expected)
	}
}
