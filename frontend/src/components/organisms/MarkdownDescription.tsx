import { useState, useEffect } from 'react'
import { useQuery } from '@connectrpc/connect-query'
import { getTaskImage } from '@taskguild/proto/taskguild/v1/task-TaskService_connectquery.ts'
import { parseWorktreePaths } from '../../lib/worktreePath.ts'

/** Inline image component that fetches and displays an image by task ID and image number. */
function ImageRefInline({ imageNum, taskId }: { imageNum: string; taskId?: string }) {
  const { data } = useQuery(getTaskImage, { taskId: taskId ?? '', imageId: imageNum }, {
    enabled: !!taskId,
  })
  const [objectUrl, setObjectUrl] = useState<string | null>(null)

  useEffect(() => {
    if (data?.data && data.image?.mediaType) {
      const blob = new Blob([data.data as BlobPart], { type: data.image.mediaType })
      const url = URL.createObjectURL(blob)
      setObjectUrl(url)
      return () => URL.revokeObjectURL(url)
    }
  }, [data])

  if (!taskId) {
    return <span className="text-cyan-400 text-[10px] bg-slate-800 px-1 rounded">[Image#{imageNum}]</span>
  }

  if (!objectUrl) {
    return <span className="text-gray-600 text-[10px]">[Image#{imageNum}]</span>
  }

  return (
    <span className="inline-block my-1">
      <img
        src={objectUrl}
        alt={`Image #${imageNum}`}
        className="max-w-xs max-h-48 rounded border border-slate-700"
      />
    </span>
  )
}

type Segment =
  | { type: 'code'; language: string; content: string }
  | { type: 'text'; content: string }

/** Parse a markdown-ish string into code-block and text segments.
 *
 * Supports variable-length backtick fences (```, ````, ````` etc.) so that
 * content containing triple-backtick code fences can be safely wrapped in a
 * longer outer fence without confusing the parser.
 *
 * Opening fence: a line of 3+ backticks optionally followed by a language tag.
 * Closing fence: a line of backticks (no language tag) whose length matches the
 * opening fence exactly.
 */
function parse(raw: string): Segment[] {
  const segments: Segment[] = []
  const openFenceRe = /^(`{3,})(\w*)\s*$/
  let i = 0
  const lines = raw.split('\n')

  while (i < lines.length) {
    const m = openFenceRe.exec(lines[i])
    if (m) {
      const fenceLen = m[1].length
      const lang = m[2] || ''
      // Closing fence: exactly the same number of backticks, no language tag.
      const closeFenceRe = new RegExp('^`{' + fenceLen + '}\\s*$')
      const codeLines: string[] = []
      i++
      while (i < lines.length && !closeFenceRe.test(lines[i])) {
        codeLines.push(lines[i])
        i++
      }
      if (i < lines.length) i++ // skip closing fence
      segments.push({ type: 'code', language: lang, content: codeLines.join('\n') })
    } else {
      const textLines: string[] = []
      while (i < lines.length && !openFenceRe.test(lines[i])) {
        textLines.push(lines[i])
        i++
      }
      const text = textLines.join('\n')
      if (text.trim()) {
        segments.push({ type: 'text', content: text })
      }
    }
  }
  return segments
}

/** Render worktree path segments with $worktree shorthand and hover tooltip. */
function renderWorktreeSegments(text: string, keyPrefix: string): React.ReactNode {
  const segments = parseWorktreePaths(text)
  if (segments.length === 1 && segments[0].kind === 'text') return text
  return segments.map((seg, i) =>
    seg.kind === 'text' ? (
      <span key={`${keyPrefix}-${i}`}>{seg.value}</span>
    ) : (
      <span key={`${keyPrefix}-${i}`} title={seg.fullPath} className="cursor-pointer">
        <span className="text-cyan-500/70">$worktree</span>
        {seg.shortened.slice('$worktree'.length)}
      </span>
    ),
  )
}

/** Render inline markdown: **bold**, `code`, [link](url), and [Image#N]. */
function renderInline(text: string, taskId?: string): React.ReactNode[] {
  const parts: React.ReactNode[] = []
  const re = /(\*\*(.+?)\*\*|`([^`]+)`|\[([^\]]+)\]\(([^)]+)\)|\[Image#(\d+)\])/g
  let last = 0
  let match: RegExpExecArray | null

  while ((match = re.exec(text)) !== null) {
    if (match.index > last) {
      parts.push(renderWorktreeSegments(text.slice(last, match.index), `pre-${match.index}`))
    }
    if (match[2] !== undefined) {
      parts.push(<strong key={match.index} className="text-gray-200 font-semibold">{match[2]}</strong>)
    } else if (match[3] !== undefined) {
      const codeContent = match[3]
      const codeSegments = parseWorktreePaths(codeContent)
      const hasWorktree = codeSegments.some(s => s.kind === 'worktree')
      parts.push(
        <code key={match.index} className="bg-slate-800 text-cyan-300 px-1 py-0.5 rounded text-[11px]">
          {hasWorktree
            ? codeSegments.map((seg, si) =>
                seg.kind === 'text' ? (
                  <span key={si}>{seg.value}</span>
                ) : (
                  <span key={si} title={seg.fullPath} className="cursor-pointer">
                    <span className="text-cyan-500/70">$worktree</span>
                    {seg.shortened.slice('$worktree'.length)}
                  </span>
                ),
              )
            : codeContent}
        </code>,
      )
    } else if (match[4] !== undefined && match[5] !== undefined) {
      parts.push(
        <a key={match.index} href={match[5]} target="_blank" rel="noopener noreferrer" className="text-cyan-400 hover:text-cyan-300 underline">
          {match[4]}
        </a>,
      )
    } else if (match[6] !== undefined) {
      // [Image#N] reference
      parts.push(
        <ImageRefInline key={match.index} imageNum={match[6]} taskId={taskId} />,
      )
    }
    last = match.index + match[0].length
  }
  if (last < text.length) {
    parts.push(renderWorktreeSegments(text.slice(last), 'tail'))
  }
  return parts
}

/* ─── Block-level element types ─── */

type BlockElement =
  | { type: 'header'; level: number; content: string }
  | { type: 'ul'; items: string[] }
  | { type: 'ol'; items: string[] }
  | { type: 'blockquote'; lines: string[] }
  | { type: 'hr' }
  | { type: 'paragraph'; lines: string[] }

const headerRe = /^(#{1,6})\s+(.+)$/
const ulItemRe = /^[-*]\s+(.+)$/
const olItemRe = /^\d+\.\s+(.+)$/
const blockquoteRe = /^>\s?(.*)$/
const hrRe = /^(?:---+|\*\*\*+|___+)\s*$/

/** Parse text content into block-level elements. */
function parseBlocks(text: string): BlockElement[] {
  const blocks: BlockElement[] = []
  const lines = text.split('\n')
  let i = 0

  while (i < lines.length) {
    const line = lines[i]

    // Skip empty lines
    if (line.trim() === '') {
      i++
      continue
    }

    // Horizontal rule
    if (hrRe.test(line)) {
      blocks.push({ type: 'hr' })
      i++
      continue
    }

    // Header
    const hMatch = headerRe.exec(line)
    if (hMatch) {
      blocks.push({ type: 'header', level: hMatch[1].length, content: hMatch[2] })
      i++
      continue
    }

    // Unordered list
    const ulMatch = ulItemRe.exec(line)
    if (ulMatch) {
      const items: string[] = []
      while (i < lines.length && ulItemRe.test(lines[i])) {
        const m = ulItemRe.exec(lines[i])
        if (m) items.push(m[1])
        i++
      }
      blocks.push({ type: 'ul', items })
      continue
    }

    // Ordered list
    const olMatch = olItemRe.exec(line)
    if (olMatch) {
      const items: string[] = []
      while (i < lines.length && olItemRe.test(lines[i])) {
        const m = olItemRe.exec(lines[i])
        if (m) items.push(m[1])
        i++
      }
      blocks.push({ type: 'ol', items })
      continue
    }

    // Blockquote
    const bqMatch = blockquoteRe.exec(line)
    if (bqMatch) {
      const bqLines: string[] = []
      while (i < lines.length && blockquoteRe.test(lines[i])) {
        const m = blockquoteRe.exec(lines[i])
        bqLines.push(m ? m[1] : '')
        i++
      }
      blocks.push({ type: 'blockquote', lines: bqLines })
      continue
    }

    // Paragraph: collect consecutive non-special lines
    const paraLines: string[] = []
    while (
      i < lines.length &&
      lines[i].trim() !== '' &&
      !hrRe.test(lines[i]) &&
      !headerRe.test(lines[i]) &&
      !ulItemRe.test(lines[i]) &&
      !olItemRe.test(lines[i]) &&
      !blockquoteRe.test(lines[i])
    ) {
      paraLines.push(lines[i])
      i++
    }
    if (paraLines.length > 0) {
      blocks.push({ type: 'paragraph', lines: paraLines })
    }
  }

  return blocks
}

/* ─── Block renderers ─── */

function HeaderBlock({ level, content, taskId }: { level: number; content: string; taskId?: string }) {
  const styles: Record<number, string> = {
    1: 'text-base font-bold text-gray-200 mt-3 mb-1.5',
    2: 'text-sm font-bold text-gray-200 mt-2.5 mb-1',
    3: 'text-xs font-semibold text-gray-300 mt-2 mb-1',
    4: 'text-xs font-semibold text-gray-300 mt-1.5 mb-0.5',
    5: 'text-xs font-medium text-gray-400 mt-1 mb-0.5',
    6: 'text-xs font-medium text-gray-400 mt-1 mb-0.5',
  }
  const cn = styles[level] ?? styles[3]
  return <div className={cn}>{renderInline(content, taskId)}</div>
}

function UnorderedListBlock({ items, taskId }: { items: string[]; taskId?: string }) {
  return (
    <ul className="list-disc list-inside space-y-0.5 my-1 ml-2">
      {items.map((item, i) => (
        <li key={i}>{renderInline(item, taskId)}</li>
      ))}
    </ul>
  )
}

function OrderedListBlock({ items, taskId }: { items: string[]; taskId?: string }) {
  return (
    <ol className="list-decimal list-inside space-y-0.5 my-1 ml-2">
      {items.map((item, i) => (
        <li key={i}>{renderInline(item, taskId)}</li>
      ))}
    </ol>
  )
}

function BlockquoteBlock({ lines, taskId }: { lines: string[]; taskId?: string }) {
  return (
    <blockquote className="border-l-2 border-slate-600 pl-3 my-1.5 text-gray-500 italic">
      {lines.map((line, i) => (
        <span key={i}>
          {i > 0 && '\n'}
          {renderInline(line, taskId)}
        </span>
      ))}
    </blockquote>
  )
}

function HorizontalRule() {
  return <hr className="border-slate-700 my-2" />
}

function ParagraphBlock({ lines, taskId }: { lines: string[]; taskId?: string }) {
  return (
    <div className="whitespace-pre-wrap my-0.5">
      {lines.map((line, j) => (
        <span key={j}>
          {j > 0 && '\n'}
          {renderInline(line, taskId)}
        </span>
      ))}
    </div>
  )
}

function TextSegment({ content, taskId }: { content: string; taskId?: string }) {
  const blocks = parseBlocks(content)
  return (
    <>
      {blocks.map((block, i) => {
        switch (block.type) {
          case 'header':
            return <HeaderBlock key={i} level={block.level} content={block.content} taskId={taskId} />
          case 'ul':
            return <UnorderedListBlock key={i} items={block.items} taskId={taskId} />
          case 'ol':
            return <OrderedListBlock key={i} items={block.items} taskId={taskId} />
          case 'blockquote':
            return <BlockquoteBlock key={i} lines={block.lines} taskId={taskId} />
          case 'hr':
            return <HorizontalRule key={i} />
          case 'paragraph':
            return <ParagraphBlock key={i} lines={block.lines} taskId={taskId} />
        }
      })}
    </>
  )
}

/* ─── Code block renderers ─── */

function DiffLine({ line }: { line: string }) {
  if (line.startsWith('+')) {
    return <span className="text-green-400">{line}</span>
  }
  if (line.startsWith('-')) {
    return <span className="text-red-400">{line}</span>
  }
  if (line.startsWith('@@')) {
    return <span className="text-cyan-400">{line}</span>
  }
  return <span>{line}</span>
}

/* ─── Bash syntax highlighting ─── */

type BashTokenType =
  | 'command'
  | 'keyword'
  | 'operator'
  | 'string'
  | 'variable'
  | 'redirect'
  | 'flag'
  | 'comment'
  | 'continuation'
  | 'text'

interface BashToken {
  type: BashTokenType
  value: string
}

const BASH_KEYWORDS = new Set([
  'if', 'then', 'else', 'elif', 'fi',
  'for', 'in', 'do', 'done',
  'while', 'until',
  'case', 'esac',
  'select',
  'function',
  'time',
  'coproc',
])

const bashTokenStyles: Record<BashTokenType, string> = {
  command: 'text-green-400',
  keyword: 'text-blue-400 font-bold',
  operator: 'text-yellow-300 font-bold',
  string: 'text-cyan-300',
  variable: 'text-purple-400',
  redirect: 'text-red-400',
  flag: 'text-blue-300',
  comment: 'text-gray-500',
  continuation: 'text-gray-600',
  text: '',
}

/**
 * Tokenize a single line of bash for syntax highlighting.
 *
 * This is a lightweight, regex-based tokenizer designed for visual
 * highlighting only — not full syntax analysis. It handles the most
 * common patterns found in formatted shell one-liners.
 */
export function tokenizeBashLine(line: string): BashToken[] {
  const tokens: BashToken[] = []
  let pos = 0
  // Track whether we've seen the "command" position on this line.
  // After an operator (&&, ||, |), the next word is a command.
  let expectCommand = true

  function pushToken(type: BashTokenType, value: string) {
    if (value) {
      tokens.push({ type, value })
    }
  }

  function remaining() {
    return line.slice(pos)
  }

  function matchAt(re: RegExp): RegExpMatchArray | null {
    const m = remaining().match(re)
    return m
  }

  while (pos < line.length) {
    const rest = remaining()

    // Leading whitespace
    const wsMatch = rest.match(/^(\s+)/)
    if (wsMatch) {
      pushToken('text', wsMatch[1])
      pos += wsMatch[1].length
      continue
    }

    // Trailing backslash continuation
    if (rest === '\\' || rest.match(/^\\$/)) {
      pushToken('continuation', '\\')
      pos += 1
      continue
    }

    // Comment (# to end of line, but not inside a word like $#)
    if (rest[0] === '#' && (pos === 0 || /\s/.test(line[pos - 1]))) {
      pushToken('comment', rest)
      pos = line.length
      continue
    }

    // Double-quoted string (handle escapes and nested $)
    if (rest[0] === '"') {
      let end = 1
      while (end < rest.length && rest[end] !== '"') {
        if (rest[end] === '\\') end++ // skip escaped char
        end++
      }
      if (end < rest.length) end++ // include closing quote
      pushToken('string', rest.slice(0, end))
      pos += end
      expectCommand = false
      continue
    }

    // Single-quoted string
    if (rest[0] === "'") {
      let end = 1
      while (end < rest.length && rest[end] !== "'") {
        end++
      }
      if (end < rest.length) end++ // include closing quote
      pushToken('string', rest.slice(0, end))
      pos += end
      expectCommand = false
      continue
    }

    // $'...' ANSI-C quoting
    if (rest.startsWith("$'")) {
      let end = 2
      while (end < rest.length && rest[end] !== "'") {
        if (rest[end] === '\\') end++
        end++
      }
      if (end < rest.length) end++
      pushToken('string', rest.slice(0, end))
      pos += end
      expectCommand = false
      continue
    }

    // Variable / parameter expansion ($VAR, ${...}, $(...), $((...))).
    // Also handles $? $! $# $$ $@ $* $0-$9.
    const varMatch = matchAt(/^\$(?:\(\(.*?\)\)|\([^)]*\)|\{[^}]*\}|[A-Za-z_]\w*|[?!#$@*0-9])/)
    if (varMatch) {
      pushToken('variable', varMatch[0])
      pos += varMatch[0].length
      expectCommand = false
      continue
    }

    // Operators: &&, ||, |&, |, ;; , ;&, ;;&, ;
    const opMatch = matchAt(/^(?:&>&?|&&|\|\||;\;&|;;&|;\&|;;|\|&|\||;|&>|&)/)
    if (opMatch) {
      pushToken('operator', opMatch[0])
      pos += opMatch[0].length
      expectCommand = true
      continue
    }

    // Redirections: >>, >&, <&, <<-, <<<, <<, <>, <(, >(, >, <
    // Optionally preceded by a fd number
    const redirMatch = matchAt(/^(?:\d*(?:>>>|>>|>&|<&|<<-|<<<|<<|<>|>\||<\(|>\(|>|<))/)
    if (redirMatch) {
      pushToken('redirect', redirMatch[0])
      pos += redirMatch[0].length
      expectCommand = false
      continue
    }

    // Shell grouping characters: ( ) { }
    // These are shell metacharacters that delimit subshells and blocks.
    if (rest[0] === '(' || rest[0] === '{') {
      pushToken('operator', rest[0])
      pos += 1
      expectCommand = true
      continue
    }
    if (rest[0] === ')' || rest[0] === '}') {
      pushToken('operator', rest[0])
      pos += 1
      expectCommand = false
      continue
    }

    // Word boundary: read a word token (no whitespace, no special chars)
    const wordMatch = matchAt(/^[^\s'"$|&;><\\#(){}]+/)
    if (wordMatch) {
      const word = wordMatch[0]

      if (BASH_KEYWORDS.has(word) && (expectCommand || isKeywordPosition(word))) {
        pushToken('keyword', word)
        // After certain keywords, next word is a command
        expectCommand = word === 'then' || word === 'else' || word === 'elif' ||
                         word === 'do' || word === 'in' || word === '!'
      } else if (expectCommand) {
        // Check for assignment (VAR=value)
        if (word.includes('=') && /^[A-Za-z_]\w*=/.test(word)) {
          pushToken('variable', word)
          // Still expect command after assignment (ENV=val cmd)
        } else {
          pushToken('command', word)
          expectCommand = false
        }
      } else if (word.match(/^-/)) {
        pushToken('flag', word)
      } else {
        pushToken('text', word)
      }
      pos += word.length
      continue
    }

    // Fallback: single character
    pushToken('text', rest[0])
    pos += 1
  }

  return tokens
}

/** Check if a word is in a keyword position (start of statement). */
function isKeywordPosition(word: string): boolean {
  // Standalone keywords that can appear at various positions
  return word === 'fi' || word === 'done' || word === 'esac' ||
         word === 'else' || word === 'elif' || word === 'then' ||
         word === 'do' || word === 'in'
}

function WorktreeAwareToken({ token }: { token: BashToken }) {
  const className = bashTokenStyles[token.type]
  const segments = parseWorktreePaths(token.value)
  const hasWorktree = segments.some(s => s.kind === 'worktree')

  if (!hasWorktree) {
    return className ? <span className={className}>{token.value}</span> : <span>{token.value}</span>
  }

  return (
    <span className={className || undefined}>
      {segments.map((seg, i) =>
        seg.kind === 'text' ? (
          <span key={i}>{seg.value}</span>
        ) : (
          <span key={i} title={seg.fullPath} className="cursor-pointer">
            <span className="text-cyan-500/70">$worktree</span>
            {seg.shortened.slice('$worktree'.length)}
          </span>
        ),
      )}
    </span>
  )
}

function BashLine({ line }: { line: string }) {
  const tokens = tokenizeBashLine(line)
  return (
    <>
      {tokens.map((token, i) => (
        <WorktreeAwareToken key={i} token={token} />
      ))}
    </>
  )
}

function CodeBlock({ language, content }: { language: string; content: string }) {
  const isDiff = language === 'diff'
  const isBash = language === 'bash' || language === 'sh'

  return (
    <pre className="bg-slate-900 border border-slate-700 rounded-lg p-3 overflow-x-auto max-h-96 overflow-y-auto my-2 text-[11px] leading-relaxed">
      <code>
        {isDiff
          ? content.split('\n').map((line, i) => (
              <span key={i}>
                {i > 0 && '\n'}
                <DiffLine line={line} />
              </span>
            ))
          : isBash
            ? content.split('\n').map((line, i) => (
                <span key={i}>
                  {i > 0 && '\n'}
                  <BashLine line={line} />
                </span>
              ))
            : renderWorktreeSegments(content, 'code')}
      </code>
    </pre>
  )
}

/* ─── Main component ─── */

export function MarkdownDescription({
  content,
  className = '',
  taskId,
}: {
  content: string
  className?: string
  taskId?: string
}) {
  const segments = parse(content)

  return (
    <div className={`text-xs text-gray-400 ${className}`}>
      {segments.map((seg, i) => {
        if (seg.type === 'code') {
          return <CodeBlock key={i} language={seg.language} content={seg.content} />
        }
        return <TextSegment key={i} content={seg.content} taskId={taskId} />
      })}
    </div>
  )
}
