/**
 * Detect whether a string likely contains ASCII art or a diagram.
 *
 * Returns true when the text contains code fences, Unicode box-drawing
 * characters, ASCII box patterns, or significant whitespace formatting
 * that suggests a visual diagram rather than plain prose.
 */
export function isAsciiArt(text: string): boolean {
  if (!text || !text.includes('\n')) return false

  // Code fences (triple backticks or more)
  if (/^`{3,}/m.test(text)) return true

  // Unicode box-drawing characters (U+2500–U+257F, U+2580–U+259F)
  if (/[\u2500-\u257F\u2580-\u259F]/.test(text)) return true

  const lines = text.split('\n')
  if (lines.length < 3) return false

  // ASCII box patterns: +---+, |...|
  const boxLineCount = lines.filter(
    (l) => /[+]-{2,}[+]/.test(l) || /\|.*\|/.test(l),
  ).length
  if (boxLineCount >= 2) return true

  // Significant interior whitespace (formatted/tabular text)
  const formattedCount = lines.filter((l) => /\S {2,}\S/.test(l)).length
  if (formattedCount >= lines.length * 0.4) return true

  return false
}
