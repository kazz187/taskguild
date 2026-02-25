type Segment =
  | { type: 'code'; language: string; content: string }
  | { type: 'text'; content: string }

/** Parse a markdown-ish string into code-block and text segments. */
function parse(raw: string): Segment[] {
  const segments: Segment[] = []
  const fenceRe = /^```(\w*)\s*$/
  let i = 0
  const lines = raw.split('\n')

  while (i < lines.length) {
    const m = fenceRe.exec(lines[i])
    if (m) {
      const lang = m[1] || ''
      const codeLines: string[] = []
      i++
      while (i < lines.length && !fenceRe.test(lines[i])) {
        codeLines.push(lines[i])
        i++
      }
      if (i < lines.length) i++ // skip closing ```
      segments.push({ type: 'code', language: lang, content: codeLines.join('\n') })
    } else {
      const textLines: string[] = []
      while (i < lines.length && !fenceRe.test(lines[i])) {
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

/** Render inline markdown: **bold**, `code`, and [link](url). */
function renderInline(text: string): React.ReactNode[] {
  const parts: React.ReactNode[] = []
  const re = /(\*\*(.+?)\*\*|`([^`]+)`|\[([^\]]+)\]\(([^)]+)\))/g
  let last = 0
  let match: RegExpExecArray | null

  while ((match = re.exec(text)) !== null) {
    if (match.index > last) {
      parts.push(text.slice(last, match.index))
    }
    if (match[2] !== undefined) {
      parts.push(<strong key={match.index} className="text-gray-200 font-semibold">{match[2]}</strong>)
    } else if (match[3] !== undefined) {
      parts.push(
        <code key={match.index} className="bg-slate-800 text-cyan-300 px-1 py-0.5 rounded text-[11px]">
          {match[3]}
        </code>,
      )
    } else if (match[4] !== undefined && match[5] !== undefined) {
      parts.push(
        <a key={match.index} href={match[5]} target="_blank" rel="noopener noreferrer" className="text-cyan-400 hover:text-cyan-300 underline">
          {match[4]}
        </a>,
      )
    }
    last = match.index + match[0].length
  }
  if (last < text.length) {
    parts.push(text.slice(last))
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

function HeaderBlock({ level, content }: { level: number; content: string }) {
  const styles: Record<number, string> = {
    1: 'text-base font-bold text-gray-200 mt-3 mb-1.5',
    2: 'text-sm font-bold text-gray-200 mt-2.5 mb-1',
    3: 'text-xs font-semibold text-gray-300 mt-2 mb-1',
    4: 'text-xs font-semibold text-gray-300 mt-1.5 mb-0.5',
    5: 'text-xs font-medium text-gray-400 mt-1 mb-0.5',
    6: 'text-xs font-medium text-gray-400 mt-1 mb-0.5',
  }
  const cn = styles[level] ?? styles[3]
  return <div className={cn}>{renderInline(content)}</div>
}

function UnorderedListBlock({ items }: { items: string[] }) {
  return (
    <ul className="list-disc list-inside space-y-0.5 my-1 ml-2">
      {items.map((item, i) => (
        <li key={i}>{renderInline(item)}</li>
      ))}
    </ul>
  )
}

function OrderedListBlock({ items }: { items: string[] }) {
  return (
    <ol className="list-decimal list-inside space-y-0.5 my-1 ml-2">
      {items.map((item, i) => (
        <li key={i}>{renderInline(item)}</li>
      ))}
    </ol>
  )
}

function BlockquoteBlock({ lines }: { lines: string[] }) {
  return (
    <blockquote className="border-l-2 border-slate-600 pl-3 my-1.5 text-gray-500 italic">
      {lines.map((line, i) => (
        <span key={i}>
          {i > 0 && '\n'}
          {renderInline(line)}
        </span>
      ))}
    </blockquote>
  )
}

function HorizontalRule() {
  return <hr className="border-slate-700 my-2" />
}

function ParagraphBlock({ lines }: { lines: string[] }) {
  return (
    <div className="whitespace-pre-wrap my-0.5">
      {lines.map((line, j) => (
        <span key={j}>
          {j > 0 && '\n'}
          {renderInline(line)}
        </span>
      ))}
    </div>
  )
}

function TextSegment({ content }: { content: string }) {
  const blocks = parseBlocks(content)
  return (
    <>
      {blocks.map((block, i) => {
        switch (block.type) {
          case 'header':
            return <HeaderBlock key={i} level={block.level} content={block.content} />
          case 'ul':
            return <UnorderedListBlock key={i} items={block.items} />
          case 'ol':
            return <OrderedListBlock key={i} items={block.items} />
          case 'blockquote':
            return <BlockquoteBlock key={i} lines={block.lines} />
          case 'hr':
            return <HorizontalRule key={i} />
          case 'paragraph':
            return <ParagraphBlock key={i} lines={block.lines} />
        }
      })}
    </>
  )
}

/* ─── Code block renderers (unchanged) ─── */

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

function CodeBlock({ language, content }: { language: string; content: string }) {
  const isDiff = language === 'diff'

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
          : content}
      </code>
    </pre>
  )
}

/* ─── Main component ─── */

export function MarkdownDescription({
  content,
  className = '',
}: {
  content: string
  className?: string
}) {
  const segments = parse(content)

  return (
    <div className={`text-xs text-gray-400 ${className}`}>
      {segments.map((seg, i) => {
        if (seg.type === 'code') {
          return <CodeBlock key={i} language={seg.language} content={seg.content} />
        }
        return <TextSegment key={i} content={seg.content} />
      })}
    </div>
  )
}
