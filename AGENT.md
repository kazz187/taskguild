# AGENT.md

## Lessons Learned

- Background goroutines that outlive the calling function must have their own context/logger lifecycle; relying on a parent's `defer Close()` causes silent failures when the parent returns first.
- When a background process produces observable side effects (e.g., file changes), capture before/after state and log the diff so that success or failure is never silent.
