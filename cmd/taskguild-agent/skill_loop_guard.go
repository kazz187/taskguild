package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
)

// skillInvocationCap is the per-task limit for identical (skill, args) Skill
// invocations. Once a Skill has been invoked this many times in the same task
// with the same arguments, subsequent invocations are auto-denied to prevent
// runaway token consumption from buggy AI-side recursion loops.
const skillInvocationCap = 2

// skillLoopGuard tracks active Skill invocations per task to detect recursive
// calls (the same skill name running twice concurrently) and to cap repeated
// identical calls (same skill + same arguments invoked too many times).
//
// Lifetime: per task. The guard is created in runTask and discarded when the
// task completes; nothing in it persists across tasks. This keeps false
// positives scoped to a single task even if the legitimate use of a skill
// repeats heavily across tasks.
//
// All methods are safe for concurrent use.
type skillLoopGuard struct {
	mu              sync.Mutex
	activeSkills    map[string]string // tool_use_id → skill name (currently executing)
	invocationCount map[string]int    // skill_name + ":" + args_hash → cumulative count
}

// newSkillLoopGuard creates an empty guard for a single task's lifetime.
func newSkillLoopGuard() *skillLoopGuard {
	return &skillLoopGuard{
		activeSkills:    make(map[string]string),
		invocationCount: make(map[string]int),
	}
}

// CheckAndRegister inspects an incoming Skill invocation. Returns (block,
// reason). If block is true, the caller MUST NOT proceed to execute the tool
// — the reason string explains why and is intended to be surfaced to the
// model so it stops retrying. On non-block, the call is registered as active
// and counted; the caller must subsequently invoke Release(toolUseID) once
// the tool finishes (success or failure).
func (g *skillLoopGuard) CheckAndRegister(toolUseID, skillName string, input map[string]any) (block bool, reason string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// A. Recursion: any OTHER tool_use_id already executing the same skill?
	for activeID, activeName := range g.activeSkills {
		if activeID != toolUseID && activeName == skillName {
			return true, fmt.Sprintf(
				"Skill loop guard: %q is already running (active tool_use_id=%s). Recursive same-skill invocation blocked. Do not retry this skill in this task — pursue an alternative approach.",
				skillName, activeID,
			)
		}
	}

	// B. Cap: same (skill, args) invoked >= cap times in this task?
	argsHash := hashArgs(input)
	key := skillName + ":" + argsHash

	if g.invocationCount[key] >= skillInvocationCap {
		return true, fmt.Sprintf(
			"Skill loop guard: %q has been invoked %d times with identical arguments in this task (cap=%d). Auto-denied to prevent runaway token consumption. Do not retry this skill in this task — pursue an alternative approach.",
			skillName, g.invocationCount[key], skillInvocationCap,
		)
	}

	// Register.
	g.activeSkills[toolUseID] = skillName
	g.invocationCount[key]++

	return false, ""
}

// Release marks a Skill invocation as finished (called on PostToolUse and
// PostToolUseFail). The cumulative invocation count is intentionally NOT
// decremented — the cap covers the entire task lifetime.
func (g *skillLoopGuard) Release(toolUseID string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	delete(g.activeSkills, toolUseID)
}

// hashArgs returns a stable SHA256 hex of the input map. encoding/json sorts
// map keys, so the digest is deterministic regardless of the original key
// insertion order.
func hashArgs(input map[string]any) string {
	data, err := json.Marshal(input)
	if err != nil {
		return ""
	}

	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}
