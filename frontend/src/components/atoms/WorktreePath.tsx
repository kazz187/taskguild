import { parseWorktreePaths } from '../../lib/worktreePath.ts'

/**
 * Renders text that may contain worktree paths with shortened display and hover tooltips.
 * Worktree path prefixes are replaced with "$worktree" and hovering shows the full path.
 */
export function WorktreePath({ text, className }: { text: string; className?: string }) {
  const segments = parseWorktreePaths(text)

  // Fast path: no worktree paths found — render plain text.
  if (segments.length === 1 && segments[0].kind === 'text') {
    return <span className={className}>{text}</span>
  }

  return (
    <span className={className}>
      {segments.map((seg, i) =>
        seg.kind === 'text' ? (
          <span key={i}>{seg.value}</span>
        ) : (
          <span key={i} title={seg.fullPath} className="cursor-help">
            <span className="text-cyan-500/70">$worktree</span>
            {seg.shortened.slice('$worktree'.length)}
          </span>
        ),
      )}
    </span>
  )
}
