import { useState, useRef, useEffect, useMemo, useCallback } from 'react'
import { InteractionType, InteractionStatus } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'
import type { Interaction } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'
import { Shield, MessageSquare, Bell, CheckCircle, X, Check, XCircle, FileText } from 'lucide-react'
import { formatTime } from './InputBar.tsx'
import { MarkdownDescription } from './MarkdownDescription.tsx'
import { Button, Input, Checkbox, AsciiArtPopover } from '../atoms/index.ts'
import { isAsciiArt } from '../../lib/asciiArt.ts'

// --- Bash Permission Metadata Types ---

interface CommandCheckResult {
  command: string
  matched: boolean
  matched_pattern?: string
  suggested_pattern?: string
}

interface RedirectCheckResult {
  operator: string
  path: string
  matched: boolean
  matched_pattern?: string
  suggested_pattern?: string
}

interface BashPermissionMetadata {
  parsed_commands: CommandCheckResult[]
  redirects: RedirectCheckResult[]
}

// --- Pattern row state for editable form ---

interface PatternRow {
  key: string
  type: 'command' | 'redirect'
  matched: boolean
  pattern: string
  checked: boolean
}

function parseBashMetadata(metadata: string): BashPermissionMetadata | null {
  if (!metadata) return null
  try {
    const parsed = JSON.parse(metadata)
    if (parsed && Array.isArray(parsed.parsed_commands)) {
      return parsed as BashPermissionMetadata
    }
  } catch {
    // not bash metadata
  }
  return null
}

function buildPatternRows(meta: BashPermissionMetadata): PatternRow[] {
  const rows: PatternRow[] = []

  for (let i = 0; i < meta.parsed_commands.length; i++) {
    const cmd = meta.parsed_commands[i]
    rows.push({
      key: `cmd-${i}`,
      type: 'command',
      matched: cmd.matched,
      pattern: cmd.matched ? (cmd.matched_pattern ?? cmd.command) : (cmd.suggested_pattern ?? cmd.command),
      checked: !cmd.matched,
    })
  }

  for (let i = 0; i < (meta.redirects?.length ?? 0); i++) {
    const redir = meta.redirects[i]
    rows.push({
      key: `redir-${i}`,
      type: 'redirect',
      matched: redir.matched,
      pattern: redir.matched ? (redir.matched_pattern ?? redir.path) : (redir.suggested_pattern ?? redir.path),
      checked: !redir.matched,
    })
  }

  return rows
}

function getPermissionShortcutLabel(value: string, isBash: boolean): string | null {
  switch (value) {
    case 'allow':
      return 'y'
    case 'always_allow':
      return isBash ? null : null // hide for both bash and non-bash
    case 'always_allow_command':
      return 'a'
    case 'deny':
      return 'n'
    default:
      return null
  }
}

// Build the JSON response for "always_allow_command"
function buildAlwaysAllowCommandResponse(rows: PatternRow[]): string {
  const rules = rows
    .filter((r) => r.checked)
    .map((r) => ({
      pattern: r.pattern,
      type: r.type === 'command' ? 'command' : 'redirect',
    }))

  return JSON.stringify({
    action: 'always_allow_command',
    rules,
  })
}

// --- Bash Command Pattern Editor ---

function BashPatternEditor({
  rows,
  onUpdatePattern,
  onToggleCheck,
}: {
  rows: PatternRow[]
  onUpdatePattern: (key: string, pattern: string) => void
  onToggleCheck: (key: string) => void
}) {
  return (
    <div className="mt-2 ml-6 space-y-1.5">
      <div className="text-[10px] text-gray-500 uppercase tracking-wide mb-1">
        Command Patterns
      </div>
      {rows.map((row) => (
        <div
          key={row.key}
          className="flex items-center gap-2 group"
        >
          {/* Match status icon */}
          <span className="shrink-0 w-4 flex justify-center" title={row.matched ? 'Matched existing rule' : 'New pattern'}>
            {row.matched ? (
              <Check className="w-3.5 h-3.5 text-green-400" />
            ) : (
              <XCircle className="w-3.5 h-3.5 text-amber-400" />
            )}
          </span>

          {/* Checkbox */}
          <Checkbox
            checked={row.checked}
            onChange={() => onToggleCheck(row.key)}
            color="cyan"
            className="shrink-0"
          />

          {/* Type badge */}
          {row.type === 'redirect' && (
            <span className="text-[11px] text-amber-400 shrink-0 font-mono">redir</span>
          )}

          {/* Editable pattern */}
          <input
            type="text"
            value={row.pattern}
            onChange={(e) => onUpdatePattern(row.key, e.target.value)}
            className="flex-1 min-w-0 px-2 py-0.5 text-[11px] font-mono bg-slate-900 border border-slate-600 rounded text-gray-200 focus:border-cyan-500 focus:outline-none transition-colors"
            onClick={(e) => e.stopPropagation()}
            onKeyDown={(e) => e.stopPropagation()}
          />
        </div>
      ))}
    </div>
  )
}

// --- Main Component ---

export function RequestItem({
  interaction,
  onRespond,
  isRespondPending,
  isSelected = false,
  onSelect,
  onDismiss,
  isDismissPending,
}: {
  interaction: Interaction
  onRespond: (id: string, response: string) => void
  isRespondPending: boolean
  isSelected?: boolean
  onSelect?: () => void
  onDismiss?: (id: string) => void
  isDismissPending?: boolean
}) {
  const [freeText, setFreeText] = useState('')
  const itemRef = useRef<HTMLDivElement>(null)
  const isPending = interaction.status === InteractionStatus.PENDING
  const isResponded = interaction.status === InteractionStatus.RESPONDED

  // Parse bash metadata
  const bashMeta = useMemo(() => parseBashMetadata(interaction.metadata), [interaction.metadata])
  const isBash = bashMeta !== null

  // Pattern rows state for editable form
  const [patternRows, setPatternRows] = useState<PatternRow[]>(() =>
    bashMeta ? buildPatternRows(bashMeta) : [],
  )

  // Re-initialize pattern rows when metadata changes
  useEffect(() => {
    if (bashMeta) {
      setPatternRows(buildPatternRows(bashMeta))
    }
  }, [bashMeta])

  const handleUpdatePattern = useCallback((key: string, pattern: string) => {
    setPatternRows((prev) =>
      prev.map((r) => (r.key === key ? { ...r, pattern } : r)),
    )
  }, [])

  const handleToggleCheck = useCallback((key: string) => {
    setPatternRows((prev) =>
      prev.map((r) => (r.key === key ? { ...r, checked: !r.checked } : r)),
    )
  }, [])

  // Build options list — backend already provides the correct options per tool type.
  // Only filter out legacy "always_allow" if present.
  const displayOptions = useMemo(() => {
    if (!isPending) return interaction.options
    return interaction.options.filter((opt) => opt.value !== 'always_allow')
  }, [interaction.options, isPending])

  // Handle respond with special logic for always_allow_command
  const handleRespond = useCallback(
    (id: string, value: string) => {
      if (value === 'always_allow_command') {
        const jsonResponse = buildAlwaysAllowCommandResponse(patternRows)
        onRespond(id, jsonResponse)
      } else {
        onRespond(id, value)
      }
    },
    [onRespond, patternRows],
  )

  // Auto-scroll into view when selected
  useEffect(() => {
    if (isSelected && itemRef.current) {
      itemRef.current.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
    }
  }, [isSelected])

  const icon =
    interaction.type === InteractionType.PERMISSION_REQUEST ? (
      <Shield className="w-4 h-4 text-amber-400" />
    ) : interaction.type === InteractionType.QUESTION ? (
      <MessageSquare className="w-4 h-4 text-blue-400" />
    ) : (
      <Bell className="w-4 h-4 text-gray-400" />
    )

  const showHints = isSelected && isPending

  return (
    <div
      ref={itemRef}
      onClick={() => onSelect?.()}
      className={`border rounded-lg p-3 transition-colors ${
        isPending
          ? isSelected
            ? 'bg-slate-800 border-cyan-500 ring-1 ring-cyan-500/50 cursor-pointer'
            : 'bg-slate-800 border-amber-500/30 cursor-pointer'
          : 'bg-slate-800/40 border-slate-700/50'
      }`}
    >
      {/* Header row: icon + title + timestamp */}
      <div className="flex items-start gap-2">
        <span className="shrink-0 mt-0.5">{icon}</span>
        <span className="text-sm font-medium text-white flex-1 min-w-0 break-words">
          {interaction.title}
        </span>
        {isPending && onDismiss && (
          <button
            onClick={(e) => { e.stopPropagation(); onDismiss(interaction.id) }}
            disabled={isDismissPending}
            className="shrink-0 p-0.5 text-gray-500 hover:text-red-400 transition-colors disabled:opacity-50"
            title="Dismiss (x)"
          >
            <X className="w-3.5 h-3.5" />
          </button>
        )}
        {interaction.createdAt && (
          <span className="text-[10px] text-gray-600 shrink-0">
            {formatTime(interaction.createdAt)}
          </span>
        )}
      </div>

      {/* Description — only shown for pending */}
      {isPending && interaction.description && (
        <div className="mt-1.5 ml-6">
          <MarkdownDescription content={interaction.description} className="text-xs" />
        </div>
      )}

      {/* Bash command pattern editor */}
      {isPending && isBash && patternRows.length > 0 && (
        <BashPatternEditor
          rows={patternRows}
          onUpdatePattern={handleUpdatePattern}
          onToggleCheck={handleToggleCheck}
        />
      )}

      {/* Action buttons for pending */}
      {isPending && displayOptions.length > 0 && (
        interaction.type === InteractionType.QUESTION ? (
          <div className="flex flex-col gap-1.5 mt-2 ml-6">
            {interaction.options.map((opt, idx) => {
              const hasArt = opt.description ? isAsciiArt(opt.description) : false
              return (
                <button
                  key={opt.value}
                  onClick={(e) => { e.stopPropagation(); onRespond(interaction.id, opt.value) }}
                  disabled={isRespondPending}
                  className="flex flex-col items-start gap-0.5 px-3 py-2 text-left bg-slate-700/60 border border-slate-600 rounded-lg hover:border-blue-500/50 hover:bg-slate-700 transition-colors disabled:opacity-50"
                >
                  <span className="text-xs font-medium text-gray-200 flex items-center gap-1">
                    {showHints && (
                      <span className="text-cyan-400 font-mono mr-1">{idx + 1}.</span>
                    )}
                    {opt.label}
                    {hasArt && (
                      <AsciiArtPopover content={opt.description}>
                        <FileText className="w-3 h-3 text-gray-500 hover:text-blue-400 shrink-0" />
                      </AsciiArtPopover>
                    )}
                  </span>
                  {opt.description && !hasArt && (
                    <span className="text-[11px] text-gray-400">{opt.description}</span>
                  )}
                </button>
              )
            })}
          </div>
        ) : (
          <div className="flex gap-2 flex-wrap mt-2 ml-6">
            {displayOptions.map((opt) => {
              const shortcut = showHints ? getPermissionShortcutLabel(opt.value, isBash) : null
              return (
                <Button
                  key={opt.value}
                  variant="secondary"
                  size="sm"
                  onClick={(e) => { e.stopPropagation(); handleRespond(interaction.id, opt.value) }}
                  disabled={isRespondPending}
                  title={opt.description}
                  className="bg-slate-700 border border-slate-600 text-gray-200 hover:border-cyan-500/50 hover:text-white"
                >
                  {opt.label}
                  {shortcut && (
                    <span className="ml-1 text-[10px] text-cyan-400 font-mono">
                      ({shortcut})
                    </span>
                  )}
                </Button>
              )
            })}
          </div>
        )
      )}

      {/* Free text input for pending with no options */}
      {isPending && interaction.options.length === 0 && (
        <div className="flex gap-2 mt-2 ml-6">
          <Input
            value={freeText}
            onChange={(e) => setFreeText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.nativeEvent.isComposing && freeText.trim()) {
                onRespond(interaction.id, freeText.trim())
                setFreeText('')
              }
            }}
            inputSize="xs"
            className="flex-1 bg-slate-900"
            placeholder="Type your response..."
          />
          <Button
            variant="primary"
            size="xs"
            onClick={() => {
              if (freeText.trim()) {
                onRespond(interaction.id, freeText.trim())
                setFreeText('')
              }
            }}
            disabled={isRespondPending || !freeText.trim()}
          >
            Send
          </Button>
        </div>
      )}

      {/* Responded inline */}
      {isResponded && interaction.response && (
        <div className="flex items-start gap-1.5 mt-1.5 ml-6">
          <CheckCircle className="w-3 h-3 text-green-400 shrink-0 mt-0.5" />
          <span className="text-xs text-green-400 break-words min-w-0">{interaction.response}</span>
        </div>
      )}
    </div>
  )
}
