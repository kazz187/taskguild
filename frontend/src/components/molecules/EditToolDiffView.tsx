import { useState, useMemo } from 'react'
import { diffLines } from 'diff'
import { Checkbox } from '../atoms/index.ts'
import { WorktreePath } from '../atoms/WorktreePath.tsx'

export interface EditToolDiffViewProps {
  filePath: string
  oldString: string
  newString: string
  replaceAll?: boolean
  /**
   * Original (pre-edit) full file content. When provided, the user can
   * toggle "Full File" mode to see the diff applied to the entire file for
   * extra context. Typically supplied by the Edit tool's output payload.
   */
  originalFile?: string
  /**
   * Header label / semantic origin of this diff view.
   * - 'input'  (default): rendering the Edit tool's input payload.
   * - 'output': rendering the Edit tool's output payload (post-apply).
   *   When combined with `originalFile`, defaults to Full File mode.
   */
  variant?: 'input' | 'output'
}

export function EditToolDiffView({
  filePath,
  oldString,
  newString,
  replaceAll,
  originalFile,
  variant = 'input',
}: EditToolDiffViewProps) {
  const [ignoreWhitespace, setIgnoreWhitespace] = useState(false)
  // Full File mode is only meaningful when we have the original file. Default
  // to ON for the 'output' variant so that the output section gives extra
  // surrounding context rather than duplicating the input snippet diff.
  const [fullFile, setFullFile] = useState<boolean>(variant === 'output' && !!originalFile)

  const headerLabel = variant === 'output' ? 'Output' : 'Input'

  const computedLines = useMemo(() => {
    const useFull = fullFile && originalFile !== undefined
    const { left, right } = useFull
      ? applyEditToFile(originalFile as string, oldString, newString, replaceAll)
      : { left: oldString, right: newString }
    const changes = diffLines(left, right, { ignoreWhitespace })
    // Split each Change into individual lines for rendering
    const result: DiffLine[] = []
    for (const change of changes) {
      // Remove trailing newline to avoid an empty trailing line after split
      const text = change.value.endsWith('\n') ? change.value.slice(0, -1) : change.value
      const splitLines = text.split('\n')
      for (const line of splitLines) {
        if (change.added) {
          result.push({ type: 'added', text: line })
        } else if (change.removed) {
          result.push({ type: 'removed', text: line })
        } else {
          result.push({ type: 'context', text: line })
        }
      }
    }
    return result
  }, [oldString, newString, ignoreWhitespace, fullFile, originalFile, replaceAll])

  const isEmpty = !fullFile && oldString === '' && newString === ''

  return (
    <div className="space-y-1.5">
      {/* Metadata: file_path and replace_all */}
      <div>
        <div className="text-[9px] text-gray-600 font-medium mb-0.5 uppercase tracking-wider">{headerLabel}</div>
        <div className="bg-slate-900/50 rounded px-2 py-1 space-y-1">
          <div>
            <div className="text-[10px] text-gray-500 font-medium">file_path</div>
            <div className="text-[11px] text-gray-400 font-mono">
              <WorktreePath text={filePath} />
            </div>
          </div>
          {replaceAll !== undefined && (
            <div>
              <div className="text-[10px] text-gray-500 font-medium">replace_all</div>
              <div className="text-[11px] text-gray-400 font-mono">{String(replaceAll)}</div>
            </div>
          )}
        </div>
      </div>

      {/* Diff section */}
      <div>
        <div className="flex items-center justify-between mb-0.5">
          <div className="text-[9px] text-gray-600 font-medium uppercase tracking-wider">Diff</div>
          <div className="flex items-center gap-2">
            {originalFile !== undefined && (
              <Checkbox
                color="purple"
                label="Full File"
                checked={fullFile}
                onChange={(e) => setFullFile(e.target.checked)}
                className="!text-[10px] !text-gray-500 !gap-1"
              />
            )}
            <Checkbox
              color="purple"
              label="-w"
              checked={ignoreWhitespace}
              onChange={(e) => setIgnoreWhitespace(e.target.checked)}
              className="!text-[10px] !text-gray-500 !gap-1"
            />
          </div>
        </div>
        <div className="bg-slate-900/50 rounded max-h-64 overflow-y-auto">
          {isEmpty ? (
            <div className="px-2 py-1 text-[11px] text-gray-500 italic">No changes</div>
          ) : (
            <pre className="font-mono text-[11px] leading-relaxed">
              {computedLines.map((line, i) => (
                <div key={i} className={lineClassName(line.type)}>
                  <span className="select-none w-3 inline-block shrink-0">{linePrefix(line.type)}</span>
                  {line.text}
                </div>
              ))}
            </pre>
          )}
        </div>
      </div>
    </div>
  )
}

interface DiffLine {
  type: 'added' | 'removed' | 'context'
  text: string
}

function linePrefix(type: DiffLine['type']): string {
  switch (type) {
    case 'added':
      return '+'
    case 'removed':
      return '-'
    case 'context':
      return ' '
  }
}

function lineClassName(type: DiffLine['type']): string {
  switch (type) {
    case 'added':
      return 'bg-green-900/30 text-green-300 px-2'
    case 'removed':
      return 'bg-red-900/30 text-red-300 px-2'
    case 'context':
      return 'text-gray-500 px-2'
  }
}

/**
 * Apply an Edit-tool style replacement to the full file content.
 * Returns the original and patched content to feed into diffLines.
 *
 * Matches the Edit tool semantics: when replaceAll is true, replace every
 * occurrence of oldString; otherwise replace only the first occurrence. If
 * oldString is empty, the file is treated as unchanged (we cannot infer an
 * insertion point).
 */
function applyEditToFile(
  original: string,
  oldString: string,
  newString: string,
  replaceAll: boolean | undefined,
): { left: string; right: string } {
  if (oldString === '') {
    return { left: original, right: original }
  }
  if (replaceAll) {
    // Split/join avoids RegExp escaping issues.
    return { left: original, right: original.split(oldString).join(newString) }
  }
  const idx = original.indexOf(oldString)
  if (idx === -1) {
    return { left: original, right: original }
  }
  const right = original.slice(0, idx) + newString + original.slice(idx + oldString.length)
  return { left: original, right }
}
