import Anser from 'anser'

/**
 * A normalized segment of text after ANSI parsing.
 * Color fields are pre-formatted CSS color strings (e.g. "rgb(0, 187, 187)")
 * so that callers can drop them directly into `style.color` / `style.backgroundColor`.
 */
export interface AnsiSegment {
  content: string
  fg?: string
  bg?: string
  bold?: boolean
  italic?: boolean
  underline?: boolean
}

/**
 * Parses ANSI SGR escape sequences in `text` and returns a list of styled
 * segments. Plain text (no ANSI) is returned as a single segment with no
 * styling fields set.
 *
 * Uses the `anser` library under the hood. We extract only the fields we
 * actually render and convert color triples ("R, G, B") into full CSS
 * `rgb(...)` strings.
 */
export function parseAnsi(text: string): AnsiSegment[] {
  if (text.length === 0) return []
  const entries = Anser.ansiToJson(text, { json: true, remove_empty: false, use_classes: false })
  const segments: AnsiSegment[] = []
  for (const e of entries) {
    if (e.content.length === 0) continue
    const seg: AnsiSegment = { content: e.content }
    if (e.fg) seg.fg = `rgb(${e.fg})`
    if (e.bg) seg.bg = `rgb(${e.bg})`
    const decorations = e.decorations ?? (e.decoration ? [e.decoration] : [])
    if (decorations.includes('bold')) seg.bold = true
    if (decorations.includes('italic')) seg.italic = true
    if (decorations.includes('underline')) seg.underline = true
    segments.push(seg)
  }
  return segments
}
