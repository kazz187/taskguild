import { InteractionType, InteractionStatus } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'
import type { Interaction } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'
import { TaskLogCategory, TaskLogLevel } from '@taskguild/proto/taskguild/v1/task_log_pb.ts'
import type { TaskLog } from '@taskguild/proto/taskguild/v1/task_log_pb.ts'
import { Shield, MessageSquare, Bell, Mail, Play, Square, RefreshCw, Anchor, Terminal, AlertTriangle, Cog } from 'lucide-react'
import { formatTime } from './ChatBubble'

export type TimelineItem =
  | { kind: 'interaction'; interaction: Interaction }
  | { kind: 'log'; log: TaskLog }

export function TimelineEntry({ item }: { item: TimelineItem }) {
  if (item.kind === 'interaction') {
    return <InteractionEntry interaction={item.interaction} />
  }
  return <LogEntry log={item.log} />
}

function InteractionEntry({ interaction }: { interaction: Interaction }) {
  const isPending = interaction.status === InteractionStatus.PENDING
  const isResponded = interaction.status === InteractionStatus.RESPONDED
  const isExpired = interaction.status === InteractionStatus.EXPIRED

  const icon =
    interaction.type === InteractionType.PERMISSION_REQUEST ? (
      <Shield className="w-3.5 h-3.5 text-amber-400" />
    ) : interaction.type === InteractionType.QUESTION ? (
      <MessageSquare className="w-3.5 h-3.5 text-blue-400" />
    ) : interaction.type === InteractionType.USER_MESSAGE ? (
      <Mail className="w-3.5 h-3.5 text-cyan-400" />
    ) : (
      <Bell className="w-3.5 h-3.5 text-gray-400" />
    )

  const typeLabel =
    interaction.type === InteractionType.PERMISSION_REQUEST
      ? 'Permission'
      : interaction.type === InteractionType.QUESTION
        ? 'Question'
        : interaction.type === InteractionType.USER_MESSAGE
          ? 'You'
          : 'Notification'

  const statusBadge = isPending ? (
    <span className="text-[10px] px-1.5 py-0.5 rounded bg-amber-500/20 text-amber-400 font-medium shrink-0">
      Pending
    </span>
  ) : isResponded ? (
    <span className="text-[10px] px-1.5 py-0.5 rounded bg-green-500/20 text-green-400 font-medium shrink-0">
      Done
    </span>
  ) : isExpired ? (
    <span className="text-[10px] px-1.5 py-0.5 rounded bg-gray-500/20 text-gray-500 font-medium shrink-0">
      Expired
    </span>
  ) : null

  return (
    <div
      className={`flex items-center gap-2 px-2 py-1 rounded text-xs ${
        isPending ? 'bg-amber-500/5' : 'hover:bg-slate-800/50'
      }`}
    >
      <span className="text-[11px] text-gray-600 font-mono w-12 shrink-0 text-right">
        {interaction.createdAt ? formatTime(interaction.createdAt) : ''}
      </span>
      <span className="shrink-0">{icon}</span>
      <span className="text-[11px] text-gray-500 w-16 shrink-0">{typeLabel}</span>
      <span className="text-gray-300 truncate flex-1 min-w-0">{interaction.title}</span>
      {statusBadge}
    </div>
  )
}

function LogEntry({ log }: { log: TaskLog }) {
  const icon = getCategoryIcon(log.category)
  const typeLabel = getCategoryLabel(log.category)
  const levelColor = getLevelColor(log.level)
  const isStderr = log.category === TaskLogCategory.STDERR

  return (
    <div className="flex items-start gap-2 px-2 py-1 rounded text-xs hover:bg-slate-800/50">
      <span className="text-[11px] text-gray-600 font-mono w-12 shrink-0 text-right mt-0.5">
        {log.createdAt ? formatTime(log.createdAt) : ''}
      </span>
      <span className="shrink-0 mt-0.5">{icon}</span>
      <span className={`text-[11px] w-16 shrink-0 mt-0.5 ${levelColor}`}>{typeLabel}</span>
      {isStderr ? (
        <pre className="text-gray-400 font-mono text-[11px] whitespace-pre-wrap break-all flex-1 min-w-0 leading-relaxed">
          {log.message}
        </pre>
      ) : (
        <span className={`truncate flex-1 min-w-0 ${levelColor}`}>{log.message}</span>
      )}
    </div>
  )
}

function getCategoryIcon(category: TaskLogCategory) {
  switch (category) {
    case TaskLogCategory.TURN_START:
      return <Play className="w-3.5 h-3.5 text-green-400" />
    case TaskLogCategory.TURN_END:
      return <Square className="w-3.5 h-3.5 text-gray-400" />
    case TaskLogCategory.STATUS_CHANGE:
      return <RefreshCw className="w-3.5 h-3.5 text-purple-400" />
    case TaskLogCategory.HOOK:
      return <Anchor className="w-3.5 h-3.5 text-indigo-400" />
    case TaskLogCategory.STDERR:
      return <Terminal className="w-3.5 h-3.5 text-gray-500" />
    case TaskLogCategory.ERROR:
      return <AlertTriangle className="w-3.5 h-3.5 text-red-400" />
    case TaskLogCategory.SYSTEM:
      return <Cog className="w-3.5 h-3.5 text-slate-400" />
    default:
      return <Cog className="w-3.5 h-3.5 text-gray-500" />
  }
}

function getCategoryLabel(category: TaskLogCategory): string {
  switch (category) {
    case TaskLogCategory.TURN_START:
      return 'Turn Start'
    case TaskLogCategory.TURN_END:
      return 'Turn End'
    case TaskLogCategory.STATUS_CHANGE:
      return 'Status'
    case TaskLogCategory.HOOK:
      return 'Hook'
    case TaskLogCategory.STDERR:
      return 'stderr'
    case TaskLogCategory.ERROR:
      return 'Error'
    case TaskLogCategory.SYSTEM:
      return 'System'
    default:
      return 'Log'
  }
}

function getLevelColor(level: TaskLogLevel): string {
  switch (level) {
    case TaskLogLevel.ERROR:
      return 'text-red-400'
    case TaskLogLevel.WARN:
      return 'text-yellow-400'
    case TaskLogLevel.DEBUG:
      return 'text-gray-500'
    default:
      return 'text-gray-300'
  }
}
