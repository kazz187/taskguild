// Worktree path shortening utilities.
// Detects paths like ".../.claude/worktrees/{name}/..." and shortens to "$worktree/...".

const WORKTREE_PATTERN = /(?:\/[^/]+)*\/\.claude\/worktrees\/[^/]+\//

/**
 * Shorten a single file path that starts with a worktree root.
 * Returns the shortened path and the full worktree prefix for tooltip display.
 */
export function shortenWorktreePath(path: string): { shortened: string; worktreePrefix: string | null } {
  const match = path.match(WORKTREE_PATTERN)
  if (!match) return { shortened: path, worktreePrefix: null }
  const prefix = match[0]
  const rest = path.slice(prefix.length)
  return { shortened: `$worktree/${rest}`, worktreePrefix: prefix.replace(/\/$/, '') }
}

/**
 * Find and shorten worktree paths within arbitrary text (e.g., log messages like "Read: /full/path/file.ts").
 * Returns segments that can be rendered with tooltips on the worktree portions.
 */
export type TextSegment =
  | { kind: 'text'; value: string }
  | { kind: 'worktree'; shortened: string; fullPath: string }

const WORKTREE_PATH_IN_TEXT = /((?:\/[^/\s]+)*\/\.claude\/worktrees\/[^/\s]+\/[^\s]*)/g

export function parseWorktreePaths(text: string): TextSegment[] {
  const segments: TextSegment[] = []
  let lastIndex = 0

  for (const match of text.matchAll(WORKTREE_PATH_IN_TEXT)) {
    const fullPath = match[1]
    const start = match.index!

    if (start > lastIndex) {
      segments.push({ kind: 'text', value: text.slice(lastIndex, start) })
    }

    const { shortened } = shortenWorktreePath(fullPath)
    segments.push({ kind: 'worktree', shortened, fullPath })
    lastIndex = start + fullPath.length
  }

  if (lastIndex < text.length) {
    segments.push({ kind: 'text', value: text.slice(lastIndex) })
  }

  // If no worktree paths found, return single text segment.
  if (segments.length === 0) {
    return [{ kind: 'text', value: text }]
  }

  return segments
}
