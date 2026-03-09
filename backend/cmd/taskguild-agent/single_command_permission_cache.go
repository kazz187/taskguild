package main

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"

	"connectrpc.com/connect"
	v1 "github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1"
	"github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1/taskguildv1connect"
	"github.com/kazz187/taskguild/backend/pkg/shellparse"
)

// wildcardToRegex converts a wildcard (glob-like) pattern to a Go regular expression.
// The only wildcard character is `*`, which matches zero or more arbitrary characters.
// All other characters are escaped so they are treated literally.
func wildcardToRegex(pattern string) string {
	parts := strings.Split(pattern, "*")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	return "^" + strings.Join(parts, ".*") + "$"
}

// compileWildcard converts a wildcard pattern string to a compiled *regexp.Regexp.
// Returns an error if the pattern is empty or the resulting regex is invalid.
func compileWildcard(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, fmt.Errorf("empty pattern")
	}
	return regexp.Compile(wildcardToRegex(pattern))
}

// compiledPattern holds a pre-compiled wildcard pattern with its metadata.
type compiledPattern struct {
	id    string
	raw   string
	regex *regexp.Regexp
	ptype string // "command" or "redirect"
	label string
}

// singleCommandPermissionCache maintains an in-memory cache of wildcard-based
// permission rules for individual shell commands. It is used to check whether
// each command in a parsed one-liner is allowed.
type singleCommandPermissionCache struct {
	mu          sync.RWMutex
	patterns    []compiledPattern
	projectName string
	client      taskguildv1connect.AgentManagerServiceClient
}

// newSingleCommandPermissionCache creates a new single-command permission cache.
func newSingleCommandPermissionCache(projectName string, client taskguildv1connect.AgentManagerServiceClient) *singleCommandPermissionCache {
	return &singleCommandPermissionCache{
		projectName: projectName,
		client:      client,
	}
}

// Update replaces the cached patterns with a new set from the backend.
func (c *singleCommandPermissionCache) Update(perms []*v1.SingleCommandPermission) {
	c.mu.Lock()
	defer c.mu.Unlock()

	compiled := make([]compiledPattern, 0, len(perms))
	for _, p := range perms {
		re, err := compileWildcard(p.Pattern)
		if err != nil {
			slog.Warn("skipping invalid wildcard pattern", "pattern", p.Pattern, "error", err)
			continue
		}
		compiled = append(compiled, compiledPattern{
			id:    p.Id,
			raw:   p.Pattern,
			regex: re,
			ptype: p.Type,
			label: p.Label,
		})
	}
	c.patterns = compiled
	slog.Info("single command permission cache updated", "patterns", len(compiled))
}

// Sync fetches the latest single-command permissions from the backend and
// updates the local cache.
func (c *singleCommandPermissionCache) Sync(ctx context.Context) {
	resp, err := c.client.ListSingleCommandPermissions(ctx, connect.NewRequest(&v1.ListSingleCommandPermissionsAgentRequest{
		ProjectName: c.projectName,
	}))
	if err != nil {
		slog.Error("failed to sync single command permissions", "error", err)
		return
	}
	c.Update(resp.Msg.GetPermissions())
}

// commandCheckResult holds the result of checking a single command.
type commandCheckResult struct {
	Command          string `json:"command"`
	Matched          bool   `json:"matched"`
	MatchedPattern   string `json:"matched_pattern,omitempty"`
	SuggestedPattern string `json:"suggested_pattern,omitempty"`
}

// redirectCheckResult holds the result of checking a single redirect.
type redirectCheckResult struct {
	Operator         string `json:"operator"`
	Path             string `json:"path"`
	Matched          bool   `json:"matched"`
	MatchedPattern   string `json:"matched_pattern,omitempty"`
	SuggestedPattern string `json:"suggested_pattern,omitempty"`
}

// bashPermissionMetadata is the structured metadata attached to a Bash
// permission request interaction.
type bashPermissionMetadata struct {
	ParsedCommands []commandCheckResult  `json:"parsed_commands"`
	Redirects      []redirectCheckResult `json:"redirects"`
}

// CheckCommand checks a command string against all "command" type patterns.
// Returns whether it matched and the matching pattern string.
func (c *singleCommandPermissionCache) CheckCommand(command string) (matched bool, pattern string) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, p := range c.patterns {
		if p.ptype != "command" {
			continue
		}
		if p.regex.MatchString(command) {
			return true, p.raw
		}
	}
	return false, ""
}

// CheckRedirect checks a redirect path against all "redirect" type patterns.
func (c *singleCommandPermissionCache) CheckRedirect(path string) (matched bool, pattern string) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, p := range c.patterns {
		if p.ptype != "redirect" {
			continue
		}
		if p.regex.MatchString(path) {
			return true, p.raw
		}
	}
	return false, ""
}

// CheckAllCommands checks all parsed commands and their redirects.
// Returns true if ALL are matched (auto-allow), and the metadata for the
// interaction UI.
func (c *singleCommandPermissionCache) CheckAllCommands(parsed *shellparse.ParseResult) (allMatched bool, meta *bashPermissionMetadata) {
	meta = &bashPermissionMetadata{}
	allMatched = true

	for _, cmd := range parsed.Commands {
		matched, pattern := c.CheckCommand(cmd.Raw)
		result := commandCheckResult{
			Command: cmd.Raw,
			Matched: matched,
		}
		if matched {
			result.MatchedPattern = pattern
		} else {
			allMatched = false
			result.SuggestedPattern = SuggestCommandPattern(cmd)
		}
		meta.ParsedCommands = append(meta.ParsedCommands, result)

		// Check each redirect.
		for _, redir := range cmd.Redirects {
			if redir.Path == "" {
				continue
			}
			rMatched, rPattern := c.CheckRedirect(redir.Path)
			rResult := redirectCheckResult{
				Operator: redir.Op,
				Path:     redir.Path,
				Matched:  rMatched,
			}
			if rMatched {
				rResult.MatchedPattern = rPattern
			} else {
				allMatched = false
				rResult.SuggestedPattern = SuggestRedirectPattern(redir.Path)
			}
			meta.Redirects = append(meta.Redirects, rResult)
		}
	}

	return allMatched, meta
}

// SuggestCommandPattern generates a suggested wildcard pattern for a command.
// The pattern is broad enough to be useful but specific enough to be safe.
func SuggestCommandPattern(cmd shellparse.ParsedCommand) string {
	exe := cmd.Executable
	if exe == "" {
		return cmd.Raw
	}

	// For commands with no arguments, match exactly.
	if len(cmd.Args) == 0 {
		return exe
	}

	// For common commands that take subcommands, include the subcommand.
	switch exe {
	case "git", "npm", "pnpm", "yarn", "bun", "make", "docker", "docker-compose", "kubectl":
		if len(cmd.Args) >= 1 {
			subCmd := cmd.Args[0]
			if len(cmd.Args) > 1 {
				return fmt.Sprintf("%s %s *", exe, subCmd)
			}
			return fmt.Sprintf("%s %s", exe, subCmd)
		}
		return fmt.Sprintf("%s *", exe)
	default:
		// Default: command name + wildcard
		return fmt.Sprintf("%s *", exe)
	}
}

// SuggestRedirectPattern generates a suggested wildcard pattern for a redirect path.
func SuggestRedirectPattern(path string) string {
	switch {
	case path == "/dev/null":
		return "/dev/null"
	case strings.HasPrefix(path, "./"):
		return "./*"
	case strings.HasPrefix(path, "../"):
		return "../*"
	case strings.HasPrefix(path, "/tmp/") || strings.HasPrefix(path, "/tmp"):
		return "/tmp/*"
	default:
		// Exact match by default
		return path
	}
}
