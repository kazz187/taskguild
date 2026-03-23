import { useState } from 'react'
import { InteractionType, InteractionStatus } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'
import type { Interaction } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'
import { TaskLogCategory, TaskLogLevel } from '@taskguild/proto/taskguild/v1/task_log_pb.ts'
import type { TaskLog } from '@taskguild/proto/taskguild/v1/task_log_pb.ts'
import {
  Shield, MessageSquare, Bell, Mail, Play, Square, RefreshCw, Anchor,
  Terminal, AlertTriangle, Cog, Wrench, FileText, Zap, ChevronRight, ChevronDown,
  CheckCircle,
} from 'lucide-react'
import { formatTime } from './InputBar.tsx'
import { Badge } from '../atoms/index.ts'
import { MarkdownDescription } from './MarkdownDescription.tsx'

export type TimelineItem =
  | { kind: 'interaction'; interaction: Interaction }
  | { kind: 'log'; log: TaskLog }

export function TimelineEntry({ item }: { item: TimelineItem }) {
  if (item.kind === 'interaction') {
    return <InteractionEntry interaction={item.interaction} />
  }
  return <LogEntry log={item.log} />
}

/** Check if an interaction has expandable details. */
function hasExpandableContent(interaction: Interaction): boolean {
  return !!(
    interaction.description ||
    interaction.options.length > 0 ||
    (interaction.response && interaction.status === InteractionStatus.RESPONDED)
  )
}

function InteractionEntry({ interaction }: { interaction: Interaction }) {
  const [expanded, setExpanded] = useState(false)
  const isPending = interaction.status === InteractionStatus.PENDING
  const isResponded = interaction.status === InteractionStatus.RESPONDED
  const isExpired = interaction.status === InteractionStatus.EXPIRED
  const expandable = hasExpandableContent(interaction)

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
    <Badge color="amber" size="xs">Pending</Badge>
  ) : isResponded ? (
    <Badge color="green" size="xs">Done</Badge>
  ) : isExpired ? (
    <Badge color="gray" size="xs">Expired</Badge>
  ) : null

  return (
    <div
      className={`rounded text-xs ${
        isPending ? 'bg-amber-500/5' : 'hover:bg-slate-800/50'
      }`}
    >
      <div
        className={`flex items-start gap-2 px-2 py-1 ${expandable ? 'cursor-pointer select-none' : ''}`}
        onClick={expandable ? () => setExpanded((prev) => !prev) : undefined}
      >
        <span className="text-[11px] text-gray-600 font-mono w-12 shrink-0 text-right mt-0.5">
          {interaction.createdAt ? formatTime(interaction.createdAt) : ''}
        </span>
        <span className="shrink-0 mt-0.5">{icon}</span>
        <span className="text-[11px] text-gray-500 w-16 shrink-0 mt-0.5">{typeLabel}</span>
        <span className={`flex-1 min-w-0 break-words ${isPending ? 'text-gray-300' : 'text-gray-300'}`}>
          {interaction.title}
        </span>
        {statusBadge}
        <span className="shrink-0 mt-0.5 text-gray-600">
          {expandable ? (
            expanded ? (
              <ChevronDown className="w-3 h-3" />
            ) : (
              <ChevronRight className="w-3 h-3" />
            )
          ) : (
            <span className="w-3 h-3 inline-block" />
          )}
        </span>
      </div>

      {expanded && (
        <div className="ml-[7.5rem] mr-2 mb-1">
          <InteractionExpandedContent interaction={interaction} />
        </div>
      )}
    </div>
  )
}

/** Renders expanded detail content for an interaction. */
function InteractionExpandedContent({ interaction }: { interaction: Interaction }) {
  const isResponded = interaction.status === InteractionStatus.RESPONDED
  const isExpired = interaction.status === InteractionStatus.EXPIRED

  /** Resolve the response value to a human-readable label. */
  function resolveResponseLabel(response: string): string {
    if (!interaction.options.length) return response
    const matched = interaction.options.find((opt) => opt.value === response)
    return matched ? matched.label : response
  }

  return (
    <div className="space-y-1.5">
      {/* Description */}
      {interaction.description && (
        <div>
          <div className="text-[10px] text-gray-500 font-medium mb-0.5">Description</div>
          <div className="bg-slate-900/50 rounded px-2 py-1 max-h-48 overflow-y-auto">
            <MarkdownDescription content={interaction.description} className="text-[11px]" />
          </div>
        </div>
      )}

      {/* Options */}
      {interaction.options.length > 0 && (
        <div>
          <div className="text-[10px] text-gray-500 font-medium mb-0.5">Options</div>
          <div className="bg-slate-900/50 rounded px-2 py-1 space-y-0.5">
            {interaction.options.map((opt) => {
              const isSelected = isResponded && interaction.response === opt.value
              return (
                <div
                  key={opt.value}
                  className={`flex items-start gap-1.5 text-[11px] ${
                    isSelected ? 'text-green-400' : 'text-gray-400'
                  }`}
                >
                  {isSelected && <CheckCircle className="w-3 h-3 shrink-0 mt-0.5" />}
                  <span className={isSelected ? 'font-medium' : ''}>
                    {opt.label}
                    {opt.description && (
                      <span className="text-gray-500 ml-1">— {opt.description}</span>
                    )}
                  </span>
                </div>
              )
            })}
          </div>
        </div>
      )}

      {/* Response */}
      {isResponded && interaction.response && (
        <div>
          <div className="text-[10px] text-gray-500 font-medium mb-0.5">Response</div>
          <div className="flex items-start gap-1.5 bg-slate-900/50 rounded px-2 py-1">
            <CheckCircle className="w-3 h-3 text-green-400 shrink-0 mt-0.5" />
            <span className="text-[11px] text-green-400 break-words min-w-0">
              {resolveResponseLabel(interaction.response)}
            </span>
            {interaction.respondedAt && (
              <span className="text-[10px] text-gray-600 ml-auto shrink-0">
                {formatTime(interaction.respondedAt)}
              </span>
            )}
          </div>
        </div>
      )}

      {/* Expired indicator */}
      {isExpired && (
        <div className="flex items-center gap-1.5 bg-slate-900/50 rounded px-2 py-1 text-[11px] text-gray-500">
          <span>Dismissed</span>
          {interaction.respondedAt && (
            <span className="text-[10px] text-gray-600 ml-auto">
              {formatTime(interaction.respondedAt)}
            </span>
          )}
        </div>
      )}
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
          <div className="text-[9px] text-gray-600 font-medium mb-0.5 uppercase tracking-wider">Input</div>
          <pre className="text-[11px] text-gray-400 font-mono whitespace-pre-wrap break-all bg-slate-900/50 rounded px-2 py-1 max-h-48 overflow-y-auto">
            {formatJsonSafe(toolInput)}
          </pre>
        </div>
      )}
      {toolOutput && (
        <div>
          <div className="text-[9px] text-gray-600 font-medium mb-0.5 uppercase tracking-wider">Output</div>
          <pre className="text-[11px] text-gray-400 font-mono whitespace-pre-wrap break-all bg-slate-900/50 rounded px-2 py-1 max-h-48 overflow-y-auto">
            {formatJsonSafe(toolOutput)}
          </pre>
        </div>
      )}
      {error && (
        <div>
          <div className="text-[9px] text-red-400 font-medium mb-0.5 uppercase tracking-wider">Error</div>
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

/** Try to pretty-print JSON with depth-limited expansion (2 levels), fall back to raw string. */
function formatJsonSafe(raw: string): string {
  try {
    const parsed = JSON.parse(raw)
    return stringifyDepthLimited(parsed, 2)
  } catch {
    return raw
  }
}

/** Stringify JSON expanding objects/arrays only up to maxDepth levels. Beyond that, inline. */
function stringifyDepthLimited(value: unknown, maxDepth: number, currentDepth = 0): string {
  if (value === null || value === undefined || typeof value !== 'object') {
    return JSON.stringify(value)
  }

  const indent = '  '.repeat(currentDepth + 1)
  const closingIndent = '  '.repeat(currentDepth)

  if (Array.isArray(value)) {
    if (value.length === 0) return '[]'
    if (currentDepth >= maxDepth) return JSON.stringify(value)
    const items = value.map((item) => `${indent}${stringifyDepthLimited(item, maxDepth, currentDepth + 1)}`)
    return `[\n${items.join(',\n')}\n${closingIndent}]`
  }

  const keys = Object.keys(value as Record<string, unknown>)
  if (keys.length === 0) return '{}'
  if (currentDepth >= maxDepth) return JSON.stringify(value)

  const entries = keys.map((key) => {
    const val = (value as Record<string, unknown>)[key]
    return `${indent}${JSON.stringify(key)}: ${stringifyDepthLimited(val, maxDepth, currentDepth + 1)}`
  })
  return `{\n${entries.join(',\n')}\n${closingIndent}}`
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
