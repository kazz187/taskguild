package main

import (
	"strings"
	"sync"
	"testing"
)

// TestRecursionDetected: registering the same skill name with a second
// tool_use_id while the first is still active must be blocked.
func TestRecursionDetected(t *testing.T) {
	g := newSkillLoopGuard()

	block, _ := g.CheckAndRegister("tu-1", "codex:rescue", map[string]any{"skill": "codex:rescue", "args": "first"})
	if block {
		t.Fatalf("first registration should not block")
	}

	block, reason := g.CheckAndRegister("tu-2", "codex:rescue", map[string]any{"skill": "codex:rescue", "args": "second"})
	if !block {
		t.Fatalf("recursive registration should block")
	}

	if !strings.Contains(reason, "Recursive") {
		t.Fatalf("recursion reason should mention 'Recursive', got: %s", reason)
	}
}

// TestCapEnforced: same (skill, args) past skillInvocationCap must be
// blocked. With cap=2, the third invocation is the first one denied.
func TestCapEnforced(t *testing.T) {
	g := newSkillLoopGuard()

	args := map[string]any{"skill": "codex:rescue", "task": "review"}

	// 1st: allow.
	if block, _ := g.CheckAndRegister("tu-1", "codex:rescue", args); block {
		t.Fatalf("1st invocation should not block")
	}

	g.Release("tu-1")

	// 2nd: allow (still within cap).
	if block, _ := g.CheckAndRegister("tu-2", "codex:rescue", args); block {
		t.Fatalf("2nd invocation should not block (cap=%d)", skillInvocationCap)
	}

	g.Release("tu-2")

	// 3rd: block (cap exceeded).
	block, reason := g.CheckAndRegister("tu-3", "codex:rescue", args)
	if !block {
		t.Fatalf("3rd invocation should block")
	}

	if !strings.Contains(reason, "cap=") {
		t.Fatalf("cap reason should mention 'cap=', got: %s", reason)
	}
}

// TestArgsDistinguished: same skill name with different args is counted
// separately and not subject to the cap of the other args.
func TestArgsDistinguished(t *testing.T) {
	g := newSkillLoopGuard()

	argsA := map[string]any{"skill": "codex:rescue", "task": "alpha"}
	argsB := map[string]any{"skill": "codex:rescue", "task": "beta"}

	// Two invocations of argsA — exhaust cap.
	g.CheckAndRegister("tu-1", "codex:rescue", argsA)
	g.Release("tu-1")
	g.CheckAndRegister("tu-2", "codex:rescue", argsA)
	g.Release("tu-2")

	// argsB should still be allowed twice.
	if block, _ := g.CheckAndRegister("tu-3", "codex:rescue", argsB); block {
		t.Fatalf("argsB 1st invocation should not block (separate args)")
	}

	g.Release("tu-3")

	if block, _ := g.CheckAndRegister("tu-4", "codex:rescue", argsB); block {
		t.Fatalf("argsB 2nd invocation should not block (separate args)")
	}
}

// TestReleaseAllowsReuse: after Release, the same skill name can be
// re-registered (provided the cap is not yet exceeded).
func TestReleaseAllowsReuse(t *testing.T) {
	g := newSkillLoopGuard()

	args := map[string]any{"skill": "dig:dig"}

	if block, _ := g.CheckAndRegister("tu-1", "dig:dig", args); block {
		t.Fatalf("1st invocation should not block")
	}

	g.Release("tu-1")

	// Re-registering with a different tool_use_id should be allowed (no
	// concurrent activity, cap not yet exceeded).
	if block, _ := g.CheckAndRegister("tu-2", "dig:dig", args); block {
		t.Fatalf("post-Release 2nd invocation should not block (sequential, within cap)")
	}
}

// TestRecursionPriorityOverCap: when both recursion and cap conditions could
// apply, the recursion message wins (it is checked first).
func TestRecursionPriorityOverCap(t *testing.T) {
	g := newSkillLoopGuard()

	args := map[string]any{"skill": "codex:rescue"}

	// Two prior allowed invocations (now released) bring count to cap.
	g.CheckAndRegister("tu-1", "codex:rescue", args)
	g.Release("tu-1")
	g.CheckAndRegister("tu-2", "codex:rescue", args)
	// Don't release tu-2 — keep it active so the next invocation triggers
	// recursion AND cap simultaneously.

	block, reason := g.CheckAndRegister("tu-3", "codex:rescue", args)
	if !block {
		t.Fatalf("3rd concurrent invocation should block")
	}

	// Recursion wins.
	if !strings.Contains(reason, "Recursive") {
		t.Fatalf("recursion should be reported first; got: %s", reason)
	}

	if strings.Contains(reason, "cap=") {
		t.Fatalf("cap message should NOT appear when recursion already detected; got: %s", reason)
	}
}

// TestConcurrent: parallel CheckAndRegister/Release calls must not race.
// Run with `go test -race` to validate.
func TestConcurrent(t *testing.T) {
	g := newSkillLoopGuard()

	const goroutines = 32

	const iterations = 50

	var wg sync.WaitGroup

	wg.Add(goroutines)

	for i := range goroutines {
		go func(workerID int) {
			defer wg.Done()

			for j := range iterations {
				toolUseID := makeID(workerID, j)
				skill := "skill-x"
				args := map[string]any{"i": j}

				_, _ = g.CheckAndRegister(toolUseID, skill, args)
				g.Release(toolUseID)
			}
		}(i)
	}

	wg.Wait()
}

// TestArgsHashStable: hashArgs must return the same hash for input maps that
// differ only in original key insertion order (encoding/json sorts keys).
func TestArgsHashStable(t *testing.T) {
	a := map[string]any{
		"skill": "codex:rescue",
		"args":  "review the diff",
		"flag":  true,
	}

	b := map[string]any{
		"flag":  true,
		"args":  "review the diff",
		"skill": "codex:rescue",
	}

	ha := hashArgs(a)
	hb := hashArgs(b)

	if ha == "" {
		t.Fatalf("hashArgs returned empty string")
	}

	if ha != hb {
		t.Fatalf("hashArgs should be stable across map key order; ha=%s hb=%s", ha, hb)
	}

	// Different content must produce a different hash.
	c := map[string]any{
		"skill": "codex:rescue",
		"args":  "review a different diff",
	}

	if hashArgs(c) == ha {
		t.Fatalf("hashArgs collision for different content")
	}
}

func makeID(worker, iter int) string {
	const digits = "0123456789"

	buf := make([]byte, 0, 16)
	buf = append(buf, 'w')
	buf = appendInt(buf, worker, digits)
	buf = append(buf, '-', 'i')
	buf = appendInt(buf, iter, digits)

	return string(buf)
}

func appendInt(buf []byte, n int, digits string) []byte {
	if n == 0 {
		return append(buf, '0')
	}

	var tmp [20]byte

	i := len(tmp)

	for n > 0 {
		i--
		tmp[i] = digits[n%10]
		n /= 10
	}

	return append(buf, tmp[i:]...)
}
