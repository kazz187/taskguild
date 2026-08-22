package claudemd_test

import (
	"reflect"
	"testing"

	"github.com/kazz187/taskguild/internal/claudemd"
)

func TestParseSkill_BlockScalarLiteral(t *testing.T) {
	t.Parallel()

	content := `---
name: gopls-explorer
description: |
  Use gopls (Go Language Server) CLI commands.
  Prefer gopls over grep/glob for Go code.
---

Skill body here.
`

	got := claudemd.ParseSkill(content)

	want := "Use gopls (Go Language Server) CLI commands.\nPrefer gopls over grep/glob for Go code."
	if got.Description != want {
		t.Errorf("description mismatch\ngot:  %q\nwant: %q", got.Description, want)
	}

	if got.Name != "gopls-explorer" {
		t.Errorf("name = %q, want %q", got.Name, "gopls-explorer")
	}

	if got.Content != "Skill body here." {
		t.Errorf("content = %q, want %q", got.Content, "Skill body here.")
	}
}

func TestParseSkill_BlockScalarFollowedByOtherFields(t *testing.T) {
	t.Parallel()

	content := `---
name: my-skill
description: |
  Line one.
  Line two.
model: sonnet
argument-hint: <path>
---

Body.
`

	got := claudemd.ParseSkill(content)

	if got.Description != "Line one.\nLine two." {
		t.Errorf("description = %q", got.Description)
	}

	if got.Model != "sonnet" {
		t.Errorf("model = %q, want %q", got.Model, "sonnet")
	}

	if got.ArgumentHint != "<path>" {
		t.Errorf("argumentHint = %q, want %q", got.ArgumentHint, "<path>")
	}
}

func TestParseSkill_BlockScalarAsLastField(t *testing.T) {
	t.Parallel()

	content := `---
name: last-block
description: |
  Only line.
---
Body.
`

	got := claudemd.ParseSkill(content)

	if got.Description != "Only line." {
		t.Errorf("description = %q, want %q", got.Description, "Only line.")
	}
}

func TestParseSkill_EmptyBlockScalar(t *testing.T) {
	t.Parallel()

	content := `---
name: empty-block
description: |
model: opus
---
Body.
`

	got := claudemd.ParseSkill(content)

	if got.Description != "" {
		t.Errorf("description = %q, want empty", got.Description)
	}

	if got.Model != "opus" {
		t.Errorf("model = %q, want %q", got.Model, "opus")
	}
}

func TestParseSkill_BlockScalarWithBlankLines(t *testing.T) {
	t.Parallel()

	content := `---
name: blank-lines
description: |
  First paragraph.

  Second paragraph.
---
Body.
`

	got := claudemd.ParseSkill(content)

	want := "First paragraph.\n\nSecond paragraph."
	if got.Description != want {
		t.Errorf("description mismatch\ngot:  %q\nwant: %q", got.Description, want)
	}
}

func TestParseSkill_FoldedBlockScalar(t *testing.T) {
	t.Parallel()

	content := `---
name: folded
description: >
  Folded line one.
  Folded line two.
---
Body.
`

	got := claudemd.ParseSkill(content)

	want := "Folded line one.\nFolded line two."
	if got.Description != want {
		t.Errorf("description mismatch\ngot:  %q\nwant: %q", got.Description, want)
	}
}

func TestParseSkill_BlockScalarThenList(t *testing.T) {
	t.Parallel()

	content := `---
name: block-then-list
description: |
  Multi
  line.
allowed-tools:
  - Read
  - Write
  - Bash
---
Body.
`

	got := claudemd.ParseSkill(content)

	if got.Description != "Multi\nline." {
		t.Errorf("description = %q", got.Description)
	}

	if want := []string{"Read", "Write", "Bash"}; !reflect.DeepEqual(got.AllowedTools, want) {
		t.Errorf("allowedTools = %v, want %v", got.AllowedTools, want)
	}
}

func TestParseSkill_AllowedToolsInline(t *testing.T) {
	t.Parallel()

	content := `---
name: inline-tools
allowed-tools: Read, Write , Bash
---
Body.
`

	got := claudemd.ParseSkill(content)

	if want := []string{"Read", "Write", "Bash"}; !reflect.DeepEqual(got.AllowedTools, want) {
		t.Errorf("allowedTools = %v, want %v", got.AllowedTools, want)
	}
}

func TestParseSkill_SingleLineDescription(t *testing.T) {
	t.Parallel()

	content := `---
name: single-line
description: A one line description.
disable-model-invocation: true
user-invocable: false
context: repo
agent: reviewer
---

Body text.
`

	got := claudemd.ParseSkill(content)

	if got.Description != "A one line description." {
		t.Errorf("description = %q", got.Description)
	}

	if !got.DisableModelInvocation {
		t.Error("disableModelInvocation = false, want true")
	}

	if got.UserInvocable {
		t.Error("userInvocable = true, want false")
	}

	if got.Context != "repo" {
		t.Errorf("context = %q, want %q", got.Context, "repo")
	}

	if got.Agent != "reviewer" {
		t.Errorf("agent = %q, want %q", got.Agent, "reviewer")
	}
}

func TestParseSkill_UserInvocableDefaultsTrue(t *testing.T) {
	t.Parallel()

	got := claudemd.ParseSkill("---\nname: defaults\n---\nBody.\n")

	if !got.UserInvocable {
		t.Error("userInvocable = false, want true (skill spec default)")
	}

	if got.DisableModelInvocation {
		t.Error("disableModelInvocation = true, want false")
	}
}

func TestParseSkill_EmptyNameStaysEmpty(t *testing.T) {
	t.Parallel()

	got := claudemd.ParseSkill("---\nname:\ndescription: d\n---\nBody.\n")

	if got.Name != "" {
		t.Errorf("name = %q, want empty (caller supplies the fallback)", got.Name)
	}
}

func TestParseSkill_NoFrontmatter(t *testing.T) {
	t.Parallel()

	content := "# Heading\n\nJust a body, no frontmatter.\n"

	got := claudemd.ParseSkill(content)

	if got.Content != "# Heading\n\nJust a body, no frontmatter." {
		t.Errorf("content = %q", got.Content)
	}

	if got.Name != "" || got.Description != "" {
		t.Errorf("expected no frontmatter fields, got name=%q description=%q", got.Name, got.Description)
	}

	if !got.UserInvocable {
		t.Error("userInvocable = false, want true")
	}
}

func TestParseSkill_UnclosedFrontmatter(t *testing.T) {
	t.Parallel()

	content := "---\nname: broken\ndescription: no closing fence\n"

	got := claudemd.ParseSkill(content)

	if got.Name != "" {
		t.Errorf("name = %q, want empty (malformed frontmatter is treated as body)", got.Name)
	}

	if got.Content != "---\nname: broken\ndescription: no closing fence" {
		t.Errorf("content = %q", got.Content)
	}
}

func TestParseSkill_Empty(t *testing.T) {
	t.Parallel()

	got := claudemd.ParseSkill("")

	if got.Content != "" || got.Name != "" {
		t.Errorf("got %+v, want zero-valued skill", got)
	}
}

func TestParseAgent_BlockScalarLiteral(t *testing.T) {
	t.Parallel()

	content := `---
name: gopls-agent
description: |
  Use gopls for precise Go code exploration.
  Prefer gopls over grep/glob for Go code.
---

Agent prompt here.
`

	got := claudemd.ParseAgent(content)

	want := "Use gopls for precise Go code exploration.\nPrefer gopls over grep/glob for Go code."
	if got.Description != want {
		t.Errorf("description mismatch\ngot:  %q\nwant: %q", got.Description, want)
	}

	if got.Name != "gopls-agent" {
		t.Errorf("name = %q, want %q", got.Name, "gopls-agent")
	}

	if got.Prompt != "Agent prompt here." {
		t.Errorf("prompt = %q, want %q", got.Prompt, "Agent prompt here.")
	}
}

func TestParseAgent_BlockScalarFollowedByOtherFields(t *testing.T) {
	t.Parallel()

	content := `---
name: my-agent
description: |
  Line one.
  Line two.
model: sonnet
permissionMode: acceptEdits
memory: project
---

Prompt.
`

	got := claudemd.ParseAgent(content)

	if got.Description != "Line one.\nLine two." {
		t.Errorf("description = %q", got.Description)
	}

	if got.Model != "sonnet" {
		t.Errorf("model = %q, want %q", got.Model, "sonnet")
	}

	if got.PermissionMode != "acceptEdits" {
		t.Errorf("permissionMode = %q, want %q", got.PermissionMode, "acceptEdits")
	}

	if got.Memory != "project" {
		t.Errorf("memory = %q, want %q", got.Memory, "project")
	}
}

func TestParseAgent_BlockScalarAsLastField(t *testing.T) {
	t.Parallel()

	got := claudemd.ParseAgent("---\nname: last\ndescription: |\n  Only line.\n---\nPrompt.\n")

	if got.Description != "Only line." {
		t.Errorf("description = %q, want %q", got.Description, "Only line.")
	}
}

func TestParseAgent_EmptyBlockScalar(t *testing.T) {
	t.Parallel()

	got := claudemd.ParseAgent("---\nname: empty\ndescription: |\nmodel: opus\n---\nPrompt.\n")

	if got.Description != "" {
		t.Errorf("description = %q, want empty", got.Description)
	}

	if got.Model != "opus" {
		t.Errorf("model = %q, want %q", got.Model, "opus")
	}
}

func TestParseAgent_BlockScalarThenList(t *testing.T) {
	t.Parallel()

	content := `---
name: block-then-list
description: |
  Multi
  line.
skills:
  - alpha
  - beta
---
Prompt.
`

	got := claudemd.ParseAgent(content)

	if got.Description != "Multi\nline." {
		t.Errorf("description = %q", got.Description)
	}

	if want := []string{"alpha", "beta"}; !reflect.DeepEqual(got.Skills, want) {
		t.Errorf("skills = %v, want %v", got.Skills, want)
	}
}

func TestParseAgent_SingleLineDescription(t *testing.T) {
	t.Parallel()

	got := claudemd.ParseAgent("---\nname: single\ndescription: A one line description.\n---\n\nPrompt body.\n")

	if got.Description != "A one line description." {
		t.Errorf("description = %q", got.Description)
	}

	if got.Prompt != "Prompt body." {
		t.Errorf("prompt = %q", got.Prompt)
	}
}

func TestParseAgent_ToolsInlineAndList(t *testing.T) {
	t.Parallel()

	content := `---
name: tooled
tools: Read, Write , Bash
disallowedTools:
  - WebFetch
  - WebSearch
skills: alpha,beta
---
Prompt.
`

	got := claudemd.ParseAgent(content)

	if want := []string{"Read", "Write", "Bash"}; !reflect.DeepEqual(got.Tools, want) {
		t.Errorf("tools = %v, want %v", got.Tools, want)
	}

	if want := []string{"WebFetch", "WebSearch"}; !reflect.DeepEqual(got.DisallowedTools, want) {
		t.Errorf("disallowedTools = %v, want %v", got.DisallowedTools, want)
	}

	if want := []string{"alpha", "beta"}; !reflect.DeepEqual(got.Skills, want) {
		t.Errorf("skills = %v, want %v", got.Skills, want)
	}
}

func TestParseAgent_IndentedKeys(t *testing.T) {
	t.Parallel()

	got := claudemd.ParseAgent("---\n  name: indented\n  model: opus\n---\nPrompt.\n")

	if got.Name != "indented" {
		t.Errorf("name = %q, want %q", got.Name, "indented")
	}

	if got.Model != "opus" {
		t.Errorf("model = %q, want %q", got.Model, "opus")
	}
}

func TestParseAgent_NoFrontmatter(t *testing.T) {
	t.Parallel()

	got := claudemd.ParseAgent("Just a prompt, no frontmatter.\n")

	if got.Prompt != "Just a prompt, no frontmatter." {
		t.Errorf("prompt = %q", got.Prompt)
	}

	if got.Name != "" {
		t.Errorf("name = %q, want empty", got.Name)
	}
}

func TestParseAgent_UnclosedFrontmatter(t *testing.T) {
	t.Parallel()

	content := "---\nname: broken\nmodel: opus\n"

	got := claudemd.ParseAgent(content)

	if got.Name != "" {
		t.Errorf("name = %q, want empty (malformed frontmatter is treated as prompt)", got.Name)
	}

	if got.Prompt != "---\nname: broken\nmodel: opus" {
		t.Errorf("prompt = %q", got.Prompt)
	}
}

func TestParseAgent_EmptyNameStaysEmpty(t *testing.T) {
	t.Parallel()

	got := claudemd.ParseAgent("---\nname:\nmodel: opus\n---\nPrompt.\n")

	if got.Name != "" {
		t.Errorf("name = %q, want empty (caller supplies the fallback)", got.Name)
	}
}

func TestSplit_CRLF(t *testing.T) {
	t.Parallel()

	got := claudemd.ParseSkill("---\r\nname: crlf\r\n---\r\nBody.\r\n")

	if got.Name != "crlf" {
		t.Errorf("name = %q, want %q", got.Name, "crlf")
	}

	if got.Content != "Body." {
		t.Errorf("content = %q, want %q", got.Content, "Body.")
	}
}
