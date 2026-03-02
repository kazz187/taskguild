package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"connectrpc.com/connect"
	claudeagent "github.com/kazz187/claude-agent-sdk-go"
	v1 "github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1"
	"github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1/taskguildv1connect"
)

// permissionCache maintains an in-memory cache of project-level allow rules.
// It is shared across all tasks within the same agent-manager, providing
// immediate permission checks without backend round-trips. When new rules are
// added (via "Always Allow"), they are persisted to the backend and broadcast
// to all connected agent-managers.
type permissionCache struct {
	mu          sync.RWMutex
	allowRules  []string
	projectName string
	client      taskguildv1connect.AgentManagerServiceClient
}

// newPermissionCache creates a new permission cache.
func newPermissionCache(projectName string, client taskguildv1connect.AgentManagerServiceClient) *permissionCache {
	return &permissionCache{
		projectName: projectName,
		client:      client,
	}
}

// Update replaces the cached allow rules (called after backend sync).
func (c *permissionCache) Update(rules []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.allowRules = make([]string, len(rules))
	copy(c.allowRules, rules)
	log.Printf("permission cache updated: %d allow rules", len(rules))
}

// Check returns true if the given tool call is allowed by any cached rule.
func (c *permissionCache) Check(toolName string, input map[string]any) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, rule := range c.allowRules {
		if matchPermissionRule(rule, toolName, input) {
			return true
		}
	}
	return false
}

// AddAndSync adds new permission rules to the cache and synchronises them
// with the backend via the SyncPermissions RPC (union merge). The resulting
// merged allow list from the backend replaces the local cache.
func (c *permissionCache) AddAndSync(ctx context.Context, newRules []string) {
	if len(newRules) == 0 {
		return
	}

	// Optimistically add to cache first for immediate effect.
	c.mu.Lock()
	c.allowRules = unionDedupRules(c.allowRules, newRules)
	c.mu.Unlock()

	log.Printf("permission cache: added %d rule(s), syncing to backend", len(newRules))

	// Sync to backend – send only the new rules; the backend merges them.
	resp, err := c.client.SyncPermissions(ctx, connect.NewRequest(&v1.SyncPermissionsRequest{
		ProjectName: c.projectName,
		LocalAllow:  newRules,
	}))
	if err != nil {
		log.Printf("permission cache: failed to sync rules to backend: %v", err)
		return
	}

	// Replace cache with the backend's authoritative merged list.
	merged := resp.Msg.GetPermissions()
	c.mu.Lock()
	c.allowRules = merged.GetAllow()
	c.mu.Unlock()

	log.Printf("permission cache: backend sync complete, allow=%d", len(merged.GetAllow()))
}

// extractRuleStrings converts PermissionUpdate objects into human-readable
// rule strings matching the Claude Code settings format.
//
// Examples:
//   - PermissionRuleValue{ToolName: "Read"}             → "Read"
//   - PermissionRuleValue{ToolName: "Bash", RuleContent: "git *"} → "Bash(git *)"
func extractRuleStrings(updates []*claudeagent.PermissionUpdate) []string {
	var rules []string
	for _, u := range updates {
		for _, rule := range u.Rules {
			rules = append(rules, ruleValueToString(rule))
		}
	}
	return rules
}

// ruleValueToString converts a single PermissionRuleValue to the string format
// used in .claude/settings.json: "ToolName" or "ToolName(ruleContent)".
func ruleValueToString(rule *claudeagent.PermissionRuleValue) string {
	if rule.RuleContent == "" {
		return rule.ToolName
	}
	return fmt.Sprintf("%s(%s)", rule.ToolName, rule.RuleContent)
}

// unionDedupRules merges two string slices, removing duplicates while preserving order.
func unionDedupRules(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	result := make([]string, 0, len(a)+len(b))
	for _, s := range a {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	for _, s := range b {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

// matchPermissionRule checks whether a permission rule (e.g. "Read",
// "Bash(git *)") matches the given tool call.
func matchPermissionRule(rule string, toolName string, input map[string]any) bool {
	// Parse rule: "ToolName" or "ToolName(pattern)".
	rTool, rPattern, hasPattern := parsePermissionRule(rule)

	if rTool != toolName {
		return false
	}

	// No pattern → tool name match is sufficient (allow all invocations).
	if !hasPattern {
		return true
	}

	// For Bash tools, match the command input against the pattern.
	if toolName == "Bash" {
		cmd, _ := input["command"].(string)
		return matchGlob(rPattern, cmd)
	}

	// For other tools with a pattern, attempt a generic match against a
	// well-known input field (file_path for Read/Write/Edit, pattern for Glob, etc.).
	for _, key := range []string{"file_path", "pattern", "path", "query", "url"} {
		if val, ok := input[key].(string); ok {
			if matchGlob(rPattern, val) {
				return true
			}
		}
	}

	return false
}

// parsePermissionRule splits a rule string into its tool name, optional
// pattern, and whether a pattern was present.
//
//	"Read"           → ("Read", "", false)
//	"Bash(git *)"    → ("Bash", "git *", true)
func parsePermissionRule(rule string) (toolName, pattern string, hasPattern bool) {
	idx := strings.Index(rule, "(")
	if idx < 0 {
		return rule, "", false
	}
	// Ensure it ends with ")".
	if !strings.HasSuffix(rule, ")") {
		return rule, "", false
	}
	return rule[:idx], rule[idx+1 : len(rule)-1], true
}

// matchGlob performs simple glob matching where "*" matches any sequence of
// characters. It supports multiple wildcards (e.g. "git * --*").
func matchGlob(pattern, value string) bool {
	// Fast paths.
	if pattern == "*" {
		return true
	}
	if pattern == "" {
		return value == ""
	}
	if !strings.Contains(pattern, "*") {
		return pattern == value
	}

	parts := strings.Split(pattern, "*")

	// First segment must match as a prefix.
	if !strings.HasPrefix(value, parts[0]) {
		return false
	}
	remaining := value[len(parts[0]):]

	// Middle segments must appear in order.
	for i := 1; i < len(parts)-1; i++ {
		idx := strings.Index(remaining, parts[i])
		if idx < 0 {
			return false
		}
		remaining = remaining[idx+len(parts[i]):]
	}

	// Last segment must match as a suffix.
	last := parts[len(parts)-1]
	return strings.HasSuffix(remaining, last)
}
