import { useMemo, type CSSProperties } from 'react'
import { AutoScrollPre } from './AutoScrollPre'
import { groupLogEntries } from './ScriptListUtils'
import type { LogEntry } from './ScriptListUtils'
import type { AnsiSegment } from '@/lib/ansi'

/**
 * Builds an inline style object for an ANSI segment. Returns `undefined`
 * when no styling overrides are needed so the parent's text color (set via
 * the stream-based class) is inherited as before.
 */
function ansiSegmentStyle(seg: AnsiSegment): CSSProperties | undefined {
  if (!seg.fg && !seg.bg && !seg.bold && !seg.italic && !seg.underline) {
    return undefined
  }
  const style: CSSProperties = {}
  if (seg.fg) style.color = seg.fg
  if (seg.bg) style.backgroundColor = seg.bg
  if (seg.bold) style.fontWeight = 600
  if (seg.italic) style.fontStyle = 'italic'
  if (seg.underline) style.textDecoration = 'underline'
  return style
}

/**
 * Renders interleaved log entries with stderr in red, grouped for performance.
 * ANSI escape sequences inside the entries are parsed and rendered as
 * colored spans (overriding the stream-based default color when present).
 */
export function LogOutput({ entries, className }: { entries: LogEntry[]; className: string }) {
  const groups = useMemo(() => groupLogEntries(entries), [entries])
  return (
    <AutoScrollPre
      scrollKey={entries.length}
      className={className}
    >
      {groups.map((group, i) => (
        <span
          key={i}
          className={group.stream === 'stderr' ? 'text-red-400' : 'text-gray-300'}
        >
          {group.segments.map((seg, j) => {
            const style = ansiSegmentStyle(seg)
            return style ? (
              <span key={j} style={style}>
                {seg.content}
              </span>
            ) : (
              <span key={j}>{seg.content}</span>
            )
          })}
        </span>
      ))}
    </AutoScrollPre>
  )
}
