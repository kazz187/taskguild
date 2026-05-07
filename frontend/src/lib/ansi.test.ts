import { describe, it, expect } from 'vitest'
import { parseAnsi } from './ansi'

describe('parseAnsi', () => {
  it('returns an empty list for an empty string', () => {
    expect(parseAnsi('')).toEqual([])
  })

  it('returns a single un-styled segment for plain text', () => {
    const segs = parseAnsi('hello world')
    expect(segs).toHaveLength(1)
    expect(segs[0].content).toBe('hello world')
    expect(segs[0].fg).toBeUndefined()
    expect(segs[0].bg).toBeUndefined()
    expect(segs[0].bold).toBeUndefined()
  })

  it('parses a simple cyan SGR sequence', () => {
    // \x1b[0;36m === reset + cyan foreground
    const segs = parseAnsi('\x1b[0;36m===> hello\x1b[0m')
    // anser may emit a leading empty segment for the reset; we filter empties
    // so we expect only the colored segment to remain.
    expect(segs).toHaveLength(1)
    expect(segs[0].content).toBe('===> hello')
    expect(segs[0].fg).toBe('rgb(0, 187, 187)')
  })

  it('separates colored and plain runs', () => {
    const segs = parseAnsi('plain \x1b[0;31mred\x1b[0m tail')
    expect(segs.map(s => s.content)).toEqual(['plain ', 'red', ' tail'])
    expect(segs[0].fg).toBeUndefined()
    expect(segs[1].fg).toBe('rgb(187, 0, 0)')
    expect(segs[2].fg).toBeUndefined()
  })

  it('detects bold decoration', () => {
    const segs = parseAnsi('\x1b[1mbold\x1b[0m')
    expect(segs).toHaveLength(1)
    expect(segs[0].bold).toBe(true)
  })

  it('preserves newlines inside the content', () => {
    const segs = parseAnsi('\x1b[0;32mfirst\nsecond\x1b[0m')
    expect(segs).toHaveLength(1)
    expect(segs[0].content).toBe('first\nsecond')
    expect(segs[0].fg).toBe('rgb(0, 187, 0)')
  })

  it('keeps text on incomplete sequences without crashing', () => {
    // Trailing escape with no closing 'm'. Should not throw and should
    // surface the readable portion in some form.
    const segs = parseAnsi('hello \x1b[31')
    const joined = segs.map(s => s.content).join('')
    expect(joined).toContain('hello')
  })
})
