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

/** Render inline markdown: **bold** and `code`. */
function renderInline(text: string): React.ReactNode[] {
  const parts: React.ReactNode[] = []
  const re = /(\*\*(.+?)\*\*|`([^`]+)`)/g
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
    }
    last = match.index + match[0].length
  }
  if (last < text.length) {
    parts.push(text.slice(last))
  }
  return parts
}

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
        // Text segment: render line by line with inline markdown.
        return (
          <div key={i} className="whitespace-pre-wrap">
            {seg.content.split('\n').map((line, j) => (
              <span key={j}>
                {j > 0 && '\n'}
                {renderInline(line)}
              </span>
            ))}
          </div>
        )
      })}
    </div>
  )
}
