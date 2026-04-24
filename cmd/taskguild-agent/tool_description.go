package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kazz187/taskguild/pkg/shellformat"
)

// inferLanguageFromPath returns a code-fence language tag based on file extension.
func inferLanguageFromPath(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".rb":
		return "ruby"
	case ".java":
		return "java"
	case ".kt":
		return "kotlin"
	case ".sh", ".bash":
		return "bash"
	case ".sql":
		return "sql"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".md":
		return "markdown"
	case ".html", ".htm":
		return "html"
	case ".css":
		return "css"
	case ".proto":
		return "protobuf"
	case ".dockerfile":
		return "dockerfile"
	default:
		if strings.HasSuffix(path, "Dockerfile") {
			return "dockerfile"
		}

		return ""
	}
}

// codeBlockFence returns a backtick fence string that is safe to use around the
// given content. If the content itself contains triple-backtick fences, a longer
// fence is returned so the inner fences don't close the outer block.
func codeBlockFence(content string) string {
	maxRun := 0
	cur := 0

	for _, r := range content {
		if r == '`' {
			cur++
			if cur > maxRun {
				maxRun = cur
			}
		} else {
			cur = 0
		}
	}

	n := max(maxRun+1, 3)

	return strings.Repeat("`", n)
}

// formatToolDescription renders a structured markdown description for a tool invocation.
func formatToolDescription(toolName string, input map[string]any) string {
	str := func(key string) string {
		if v, ok := input[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}

		return ""
	}

	var sb strings.Builder

	switch toolName {
	case "Bash":
		sb.WriteString("**Tool:** `Bash`\n")

		if desc := str("description"); desc != "" {
			fmt.Fprintf(&sb, "**Description:** %s\n", desc)
		}

		if cmd := str("command"); cmd != "" {
			// Format the command for readability (breaks one-liners into
			// multi-line with proper indentation). Falls back to the
			// original command on parse error.
			formatted, _ := shellformat.Format(cmd)
			if formatted == "" {
				formatted = cmd
			}

			fence := codeBlockFence(formatted)
			fmt.Fprintf(&sb, "\n%sbash\n", fence)
			sb.WriteString(formatted)
			fmt.Fprintf(&sb, "\n%s\n", fence)
		}

	case "Edit":
		sb.WriteString("**Tool:** `Edit`\n")

		filePath := str("file_path")
		if filePath != "" {
			fmt.Fprintf(&sb, "**File:** `%s`\n", filePath)
		}

		oldStr := str("old_string")

		newStr := str("new_string")
		if oldStr != "" || newStr != "" {
			combined := oldStr + newStr
			fence := codeBlockFence(combined)
			fmt.Fprintf(&sb, "\n%sdiff\n", fence)

			for line := range strings.SplitSeq(oldStr, "\n") {
				sb.WriteString("- ")
				sb.WriteString(line)
				sb.WriteString("\n")
			}

			for line := range strings.SplitSeq(newStr, "\n") {
				sb.WriteString("+ ")
				sb.WriteString(line)
				sb.WriteString("\n")
			}

			sb.WriteString(fence + "\n")
		}

	case "Write":
		sb.WriteString("**Tool:** `Write`\n")

		filePath := str("file_path")
		if filePath != "" {
			fmt.Fprintf(&sb, "**File:** `%s`\n", filePath)
		}

		if content := str("content"); content != "" {
			lang := inferLanguageFromPath(filePath)
			fence := codeBlockFence(content)
			fmt.Fprintf(&sb, "\n%s%s\n", fence, lang)
			sb.WriteString(content)
			fmt.Fprintf(&sb, "\n%s\n", fence)
		}

	case "Read":
		sb.WriteString("**Tool:** `Read`\n")

		if filePath := str("file_path"); filePath != "" {
			fmt.Fprintf(&sb, "**File:** `%s`\n", filePath)
		}

	case "Glob":
		sb.WriteString("**Tool:** `Glob`\n")

		if pattern := str("pattern"); pattern != "" {
			fmt.Fprintf(&sb, "**Pattern:** `%s`\n", pattern)
		}

		if path := str("path"); path != "" {
			fmt.Fprintf(&sb, "**Path:** `%s`\n", path)
		}

	case "Grep":
		sb.WriteString("**Tool:** `Grep`\n")

		if pattern := str("pattern"); pattern != "" {
			fmt.Fprintf(&sb, "**Pattern:** `%s`\n", pattern)
		}

		if path := str("path"); path != "" {
			fmt.Fprintf(&sb, "**Path:** `%s`\n", path)
		}

	default:
		fmt.Fprintf(&sb, "**Tool:** `%s`\n", toolName)
		// Render remaining keys sorted, with multiline values in code blocks.
		keys := make([]string, 0, len(input))
		for k := range input {
			keys = append(keys, k)
		}

		sort.Strings(keys)

		for _, k := range keys {
			v := input[k]

			s := fmt.Sprintf("%v", v)
			if strings.Contains(s, "\n") {
				fence := codeBlockFence(s)
				fmt.Fprintf(&sb, "**%s:**\n\n%s\n%s\n%s\n", k, fence, s, fence)
			} else {
				fmt.Fprintf(&sb, "**%s:** `%s`\n", k, s)
			}
		}
	}

	return sb.String()
}
