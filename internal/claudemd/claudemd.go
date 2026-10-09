// Package claudemd parses the YAML frontmatter of Claude Code markdown
// definition files (.claude/skills/*/SKILL.md and .claude/agents/*.md).
//
// It is the string-based counterpart of internal/claudesettings, which handles
// .claude/settings.json. Callers that read from disk should read the whole file
// and hand the content to ParseSkill / ParseAgent.
package claudemd

import "strings"

// Delimiter is the YAML frontmatter fence used by Claude Code markdown files.
const Delimiter = "---"

// YAML frontmatter keys used by .claude/skills/*/SKILL.md and .claude/agents/*.md.
const (
	keyName                   = "name"
	keyDescription            = "description"
	keyModel                  = "model"
	keyContext                = "context"
	keyAgent                  = "agent"
	keyArgumentHint           = "argument-hint"
	keyAllowedTools           = "allowed-tools"
	keyDisableModelInvocation = "disable-model-invocation"
	keyUserInvocable          = "user-invocable"
	keyTools                  = "tools"
	keyDisallowedTools        = "disallowedTools"
	keySkills                 = "skills"
	keyPermissionMode         = "permissionMode"
	keyMemory                 = "memory"
)

// YAML syntax markers recognized inside the frontmatter block.
const (
	blockScalarLiteral = "|"
	blockScalarFolded  = ">"
	listItemPrefix     = "- "
	boolTrue           = "true"
)

// Skill holds data extracted from a SKILL.md file.
type Skill struct {
	Name                   string
	Description            string
	Content                string
	Model                  string
	Context                string
	Agent                  string
	ArgumentHint           string
	AllowedTools           []string
	DisableModelInvocation bool
	UserInvocable          bool
}

// Agent holds data extracted from a .claude/agents/*.md file.
type Agent struct {
	Name            string
	Description     string
	Prompt          string
	Model           string
	PermissionMode  string
	Memory          string
	Tools           []string
	DisallowedTools []string
	Skills          []string
}

// ParseSkill parses a SKILL.md document. Name is left empty when the frontmatter
// carries no name: the caller decides the fallback (usually the skill directory
// name). UserInvocable defaults to true per the skill spec.
func ParseSkill(content string) *Skill {
	front, body := split(content)
	result := &Skill{
		Content:       body,
		UserInvocable: true,
	}

	walk(front, map[string]bool{keyAllowedTools: true},
		func(key, value string) {
			switch key {
			case keyName:
				result.Name = value
			case keyDescription:
				result.Description = value
			case keyDisableModelInvocation:
				result.DisableModelInvocation = strings.EqualFold(value, boolTrue)
			case keyUserInvocable:
				result.UserInvocable = strings.EqualFold(value, boolTrue)
			case keyModel:
				result.Model = value
			case keyContext:
				result.Context = value
			case keyAgent:
				result.Agent = value
			case keyArgumentHint:
				result.ArgumentHint = value
			}
		},
		func(key, item string) {
			if key == keyAllowedTools {
				result.AllowedTools = append(result.AllowedTools, item)
			}
		},
	)

	return result
}

// ParseAgent parses a .claude/agents/*.md document. Name is left empty when the
// frontmatter carries no name: the caller decides the fallback (usually the file
// name without its extension).
func ParseAgent(content string) *Agent {
	front, body := split(content)
	result := &Agent{Prompt: body}

	listKeys := map[string]bool{
		keyTools:           true,
		keyDisallowedTools: true,
		keySkills:          true,
	}

	walk(front, listKeys,
		func(key, value string) {
			switch key {
			case keyName:
				result.Name = value
			case keyDescription:
				result.Description = value
			case keyModel:
				result.Model = value
			case keyPermissionMode:
				result.PermissionMode = value
			case keyMemory:
				result.Memory = value
			}
		},
		func(key, item string) {
			switch key {
			case keyTools:
				result.Tools = append(result.Tools, item)
			case keyDisallowedTools:
				result.DisallowedTools = append(result.DisallowedTools, item)
			case keySkills:
				result.Skills = append(result.Skills, item)
			}
		},
	)

	return result
}

// split separates the YAML frontmatter lines from the body. When the content has
// no frontmatter, or the opening fence is never closed, front is nil and body is
// the whole (trimmed) content.
func split(content string) ([]string, string) {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSuffix(line, "\r")
	}

	whole := strings.TrimSpace(strings.Join(lines, "\n"))

	if strings.TrimSpace(lines[0]) != Delimiter {
		return nil, whole
	}

	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == Delimiter {
			return lines[1:i], strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
		}
	}

	return nil, whole
}

// blockScalar accumulates the indented lines of a YAML block scalar (| or >).
type blockScalar struct {
	key    string
	lines  []string
	indent int
}

// start begins accumulating a block scalar for key.
func (b *blockScalar) start(key string) {
	b.key = key
	b.lines = nil
	b.indent = 0
}

// consume reports whether line continues the open block scalar, appending it
// when it does. Indented lines are de-indented by the first line's indent.
func (b *blockScalar) consume(line, trimmed string) bool {
	if b.key == "" {
		return false
	}

	if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
		if len(b.lines) == 0 {
			b.indent = len(line) - len(strings.TrimLeft(line, " \t"))
		}

		stripped := line
		if len(line) >= b.indent {
			stripped = line[b.indent:]
		}

		b.lines = append(b.lines, stripped)

		return true
	}

	if trimmed == "" {
		b.lines = append(b.lines, "")
		return true
	}

	return false
}

// flush emits the accumulated value and resets the accumulator. It is a no-op
// when no block scalar is open.
func (b *blockScalar) flush(scalar func(key, value string)) {
	if b.key == "" {
		return
	}

	scalar(b.key, strings.TrimRight(strings.Join(b.lines, "\n"), "\n "))
	b.key = ""
	b.lines = nil
}

// emitList handles a list-valued key. An empty value opens a multi-line list and
// the key is returned so that following "- item" lines attach to it; otherwise
// the comma-separated inline items are emitted immediately.
func emitList(key, value string, list func(key, item string)) string {
	if value == "" {
		return key
	}

	for p := range strings.SplitSeq(value, ",") {
		if p = strings.TrimSpace(p); p != "" {
			list(key, p)
		}
	}

	return ""
}

// walk iterates frontmatter lines and dispatches each entry:
//   - scalar(key, value) for "key: value" and for block scalars (| and >)
//   - list(key, item) for "  - item" continuations and for the comma-separated
//     inline form of keys present in listKeys
func walk(lines []string, listKeys map[string]bool, scalar func(key, value string), list func(key, item string)) {
	var (
		currentListKey string
		block          blockScalar
	)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if block.consume(line, trimmed) {
			continue
		}
		// Any other line terminates an open block scalar, then is processed below.
		block.flush(scalar)

		// YAML list item (e.g. "  - Read") continuing the previous list key.
		if item, ok := strings.CutPrefix(trimmed, listItemPrefix); ok && currentListKey != "" {
			if item = strings.TrimSpace(item); item != "" {
				list(currentListKey, item)
			}

			continue
		}

		idx := strings.Index(line, ":")
		if idx <= 0 {
			continue
		}

		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		currentListKey = "" // Reset list context.

		switch {
		case value == blockScalarLiteral || value == blockScalarFolded:
			block.start(key)
		case listKeys[key]:
			currentListKey = emitList(key, value, list)
		default:
			scalar(key, value)
		}
	}

	block.flush(scalar)
}
