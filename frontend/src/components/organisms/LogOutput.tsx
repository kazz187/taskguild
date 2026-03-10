import { useMemo } from 'react'
import { AutoScrollPre } from './AutoScrollPre'
import { groupLogEntries } from './ScriptListUtils'
import type { LogEntry } from './ScriptListUtils'

/** Renders interleaved log entries with stderr in red, grouped for performance */
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
          {group.text}
        </span>
      ))}
    </AutoScrollPre>
  )
}
