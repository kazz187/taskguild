import type { ScriptDefinition, ScriptLogEntry } from '@taskguild/proto/taskguild/v1/script_pb.ts'
import { ScriptLogStream } from '@taskguild/proto/taskguild/v1/script_pb.ts'
import { ScriptDiffType } from '@taskguild/proto/taskguild/v1/agent_manager_pb.ts'
import { parseAnsi, type AnsiSegment } from '@/lib/ansi'

export interface ScriptFormData {
  name: string
  description: string
  filename: string
  content: string
}

export const emptyForm: ScriptFormData = {
  name: '',
  description: '',
  filename: '',
  content: '',
}

export function scriptToForm(s: ScriptDefinition): ScriptFormData {
  return {
    name: s.name,
    description: s.description,
    filename: s.filename,
    content: s.content,
  }
}

export interface LogEntry {
  stream: 'stdout' | 'stderr'
  text: string
}

export interface ExecutionResult {
  scriptId: string
  requestId: string
  completed: boolean
  success?: boolean
  exitCode?: number
  logEntries: LogEntry[]
  errorMessage?: string
  stoppedByUser?: boolean
}

export function protoLogToLocal(entries: ScriptLogEntry[]): LogEntry[] {
  return entries.map(e => ({
    stream: e.stream === ScriptLogStream.STDERR ? 'stderr' : 'stdout',
    text: e.text,
  }))
}

export function diffTypeLabel(dt: ScriptDiffType): string {
  switch (dt) {
    case ScriptDiffType.MODIFIED: return 'Modified'
    case ScriptDiffType.AGENT_ONLY: return 'Agent only'
    case ScriptDiffType.SERVER_ONLY: return 'Server only'
    default: return 'Unknown'
  }
}

export interface LogGroup {
  stream: 'stdout' | 'stderr'
  segments: AnsiSegment[]
}

/**
 * Groups consecutive log entries with the same stream type and parses ANSI
 * escape sequences within each group. This both:
 *   - reduces DOM element count (e.g. 30,000 entries → ~100 groups), and
 *   - enables colored rendering of ANSI sequences emitted by user scripts.
 */
export function groupLogEntries(entries: LogEntry[]): LogGroup[] {
  if (entries.length === 0) return []
  const merged: { stream: 'stdout' | 'stderr'; text: string }[] = []
  let current = { stream: entries[0].stream, text: entries[0].text }
  for (let i = 1; i < entries.length; i++) {
    if (entries[i].stream === current.stream) {
      current.text += entries[i].text
    } else {
      merged.push(current)
      current = { stream: entries[i].stream, text: entries[i].text }
    }
  }
  merged.push(current)
  return merged.map(g => ({ stream: g.stream, segments: parseAnsi(g.text) }))
}
