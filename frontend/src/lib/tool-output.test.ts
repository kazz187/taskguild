import { describe, it, expect } from 'vitest'
import { safeParseToolJSON, PARSE_FAILED } from './tool-output'

describe('safeParseToolJSON', () => {
  it('parses a plain JSON object', () => {
    const raw = '{"file_path":"/a/b.ts","old_string":"x"}'
    expect(safeParseToolJSON(raw)).toEqual({
      file_path: '/a/b.ts',
      old_string: 'x',
    })
  })

  it('unwraps a legacy double-encoded JSON object', () => {
    // Outer JSON string whose content is itself JSON (the shape that
    // the old backend produced before the double-encode fix).
    const inner = '{"filePath":"/a.ts","newString":"<div>\\n</div>"}'
    const raw = JSON.stringify(inner) // => "\"{\\\"filePath\\\":...}\""
    expect(safeParseToolJSON(raw)).toEqual({
      filePath: '/a.ts',
      newString: '<div>\n</div>',
    })
  })

  it('returns PARSE_FAILED for non-JSON input', () => {
    expect(safeParseToolJSON('not json at all')).toBe(PARSE_FAILED)
  })

  it('returns the first string when it is not itself JSON', () => {
    // JSON.parse('"hello"') => 'hello' (a plain string, not nested JSON).
    expect(safeParseToolJSON('"hello"')).toBe('hello')
  })

  it('preserves a legitimate null result', () => {
    // null parses successfully and must be distinguishable from PARSE_FAILED.
    expect(safeParseToolJSON('null')).toBeNull()
  })

  it('preserves arrays', () => {
    expect(safeParseToolJSON('[1,2,3]')).toEqual([1, 2, 3])
  })

  it('does not unescape characters in already-single-encoded JSON', () => {
    // New (fixed) backend path: tool_output stored as a valid JSON object
    // whose string fields retain raw `<` / newlines unchanged.
    const raw = '{"newString":"<div>\\n</div>"}'
    const parsed = safeParseToolJSON(raw) as { newString: string }
    expect(parsed.newString).toBe('<div>\n</div>')
    expect(parsed.newString).not.toContain('\\u003c')
  })
})
