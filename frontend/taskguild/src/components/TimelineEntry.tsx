import { useState } from 'react'
import { InteractionType, InteractionStatus } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'
import type { Interaction } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'
import { TaskLogCategory, TaskLogLevel } from '@taskguild/proto/taskguild/v1/task_log_pb.ts'
import type { TaskLog } from '@taskguild/proto/taskguild/v1/task_log_pb.ts'
import {
  Shield, MessageSquare, Bell, Mail, Play, Square, RefreshCw, Anchor,
  Terminal, AlertTriangle, Cog, Wrench, FileText, Zap, ChevronRight, ChevronDown,
} from 'lucide-react'
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
      className={`flex items-start gap-2 px-2 py-1 rounded text-xs ${
        isPending ? 'bg-amber-500/5' : 'hover:bg-slate-800/50'
      }`}
    >
      <span className="text-[11px] text-gray-600 font-mono w-12 shrink-0 text-right mt-0.5">
        {interaction.createdAt ? formatTime(interaction.createdAt) : ''}
      </span>
      <span className="shrink-0 mt-0.5">{icon}</span>
      <span className="text-[11px] text-gray-500 w-16 shrink-0 mt-0.5">{typeLabel}</span>
      <span className="text-gray-300 flex-1 min-w-0 break-words">{interaction.title}</span>
      {statusBadge}
    </div>
  )
}

function LogEntry({ log }: { log: TaskLog }) {
  const isExpandable = isExpandableCategory(log.category)

  if (isExpandable) {
    return <ExpandableLogEntry log={log} />
  }

  return <SimpleLogEntry log={log} />
}

/** Non-expandable log entry (Turn Start/End, Status Change, Hook, Stderr, Error, System). */
function SimpleLogEntry({ log }: { log: TaskLog }) {
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

/** Expandable log entry for TOOL_USE, AGENT_OUTPUT, DIRECTIVE categories. */
function ExpandableLogEntry({ log }: { log: TaskLog }) {
  const [expanded, setExpanded] = useState(false)
  const icon = getCategoryIcon(log.category)
  const typeLabel = getCategoryLabel(log.category)
  const levelColor = getLevelColor(log.level)

  return (
    <div className="rounded text-xs hover:bg-slate-800/50">
      <div
        className="flex items-start gap-2 px-2 py-1 cursor-pointer select-none"
        onClick={() => setExpanded((prev) => !prev)}
      >
        <span className="text-[11px] text-gray-600 font-mono w-12 shrink-0 text-right mt-0.5">
          {log.createdAt ? formatTime(log.createdAt) : ''}
        </span>
        <span className="shrink-0 mt-0.5">{icon}</span>
        <span className={`text-[11px] w-16 shrink-0 mt-0.5 ${levelColor}`}>{typeLabel}</span>
        <span className={`truncate flex-1 min-w-0 ${levelColor}`}>{log.message}</span>
        <span className="shrink-0 mt-0.5 text-gray-600">
          {expanded ? (
            <ChevronDown className="w-3 h-3" />
          ) : (
            <ChevronRight className="w-3 h-3" />
          )}
        </span>
      </div>

      {expanded && (
        <div className="ml-[7.5rem] mr-2 mb-1">
          <ExpandedContent log={log} />
        </div>
      )}
    </div>
  )
}

/** Renders expanded detail content based on category. */
function ExpandedContent({ log }: { log: TaskLog }) {
  const metadata = log.metadata as Record<string, string>

  switch (log.category) {
    case TaskLogCategory.TOOL_USE:
      return <ToolUseDetail metadata={metadata} />
    case TaskLogCategory.AGENT_OUTPUT:
      return <AgentOutputDetail metadata={metadata} />
    case TaskLogCategory.DIRECTIVE:
      return <DirectiveDetail metadata={metadata} />
    default:
      return null
  }
}

/** Tool use expanded detail: shows input parameters and output result. */
function ToolUseDetail({ metadata }: { metadata: Record<string, string> }) {
  const toolInput = metadata['tool_input']
  const toolOutput = metadata['tool_output']
  const error = metadata['error']

  return (
    <div className="space-y-1.5">
      {toolInput && (
        <div>
          <div className="text-[10px] text-gray-500 font-medium mb-0.5">Input</div>
          <pre className="text-[11px] text-gray-400 font-mono whitespace-pre-wrap break-all bg-slate-900/50 rounded px-2 py-1 max-h-48 overflow-y-auto">
            {formatJsonSafe(toolInput)}
          </pre>
        </div>
      )}
      {toolOutput && (
        <div>
          <div className="text-[10px] text-gray-500 font-medium mb-0.5">Output</div>
          <pre className="text-[11px] text-gray-400 font-mono whitespace-pre-wrap break-all bg-slate-900/50 rounded px-2 py-1 max-h-48 overflow-y-auto">
            {formatJsonSafe(toolOutput)}
          </pre>
        </div>
      )}
      {error && (
        <div>
          <div className="text-[10px] text-red-400 font-medium mb-0.5">Error</div>
          <pre className="text-[11px] text-red-300 font-mono whitespace-pre-wrap break-all bg-red-900/20 rounded px-2 py-1 max-h-48 overflow-y-auto">
            {error}
          </pre>
        </div>
      )}
    </div>
  )
}

/** Agent output expanded detail: shows full text output. */
function AgentOutputDetail({ metadata }: { metadata: Record<string, string> }) {
  const fullText = metadata['full_text']
  if (!fullText) return null

  return (
    <pre className="text-[11px] text-gray-300 font-mono whitespace-pre-wrap break-words bg-slate-900/50 rounded px-2 py-1.5 max-h-64 overflow-y-auto leading-relaxed">
      {fullText}
    </pre>
  )
}

/** Directive expanded detail: shows directive-specific information. */
function DirectiveDetail({ metadata }: { metadata: Record<string, string> }) {
  const directiveType = metadata['directive_type']

  return (
    <div className="text-[11px] text-gray-400 bg-slate-900/50 rounded px-2 py-1 space-y-0.5">
      {directiveType && (
        <div>
          <span className="text-gray-500">Type: </span>
          <span className="text-yellow-300">{directiveType}</span>
        </div>
      )}
      {metadata['task_title'] && (
        <div>
          <span className="text-gray-500">Task: </span>
          <span className="text-gray-300">{metadata['task_title']}</span>
        </div>
      )}
      {metadata['next_status'] && (
        <div>
          <span className="text-gray-500">Status: </span>
          <span className="text-purple-300">{metadata['next_status']}</span>
        </div>
      )}
    </div>
  )
}

/** Check if a log category supports expand/collapse. */
function isExpandableCategory(category: TaskLogCategory): boolean {
  return (
    category === TaskLogCategory.TOOL_USE ||
    category === TaskLogCategory.AGENT_OUTPUT ||
    category === TaskLogCategory.DIRECTIVE
  )
}

/** Try to pretty-print JSON, fall back to raw string. */
function formatJsonSafe(raw: string): string {
  try {
    const parsed = JSON.parse(raw)
    return JSON.stringify(parsed, null, 2)
  } catch {
    return raw
  }
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
    case TaskLogCategory.TOOL_USE:
      return <Wrench className="w-3.5 h-3.5 text-blue-400" />
    case TaskLogCategory.AGENT_OUTPUT:
      return <FileText className="w-3.5 h-3.5 text-green-300" />
    case TaskLogCategory.DIRECTIVE:
      return <Zap className="w-3.5 h-3.5 text-yellow-400" />
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
    case TaskLogCategory.TOOL_USE:
      return 'Tool'
    case TaskLogCategory.AGENT_OUTPUT:
      return 'Output'
    case TaskLogCategory.DIRECTIVE:
      return 'Directive'
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
