import { describe, it, expect } from 'vitest'
import { tokenizeBashLine } from './MarkdownDescription'

describe('tokenizeBashLine', () => {
  /** Helper to extract [type, value] pairs from tokens. */
  function tokenTypes(line: string) {
    return tokenizeBashLine(line).map((t) => [t.type, t.value] as const)
  }

  /** Helper to get the types of non-whitespace tokens. */
  function nonWsTypes(line: string) {
    return tokenizeBashLine(line)
      .filter((t) => t.value.trim() !== '')
      .map((t) => [t.type, t.value] as const)
  }

  it('tokenizes a simple command', () => {
    const tokens = nonWsTypes('echo hello')
    expect(tokens).toEqual([
      ['command', 'echo'],
      ['text', 'hello'],
    ])
  })

  it('tokenizes command with flags', () => {
    const tokens = nonWsTypes('ls -la --color=auto /tmp')
    expect(tokens).toEqual([
      ['command', 'ls'],
      ['flag', '-la'],
      ['flag', '--color=auto'],
      ['text', '/tmp'],
    ])
  })

  it('tokenizes && operator', () => {
    const tokens = nonWsTypes('echo a && echo b')
    expect(tokens).toEqual([
      ['command', 'echo'],
      ['text', 'a'],
      ['operator', '&&'],
      ['command', 'echo'],
      ['text', 'b'],
    ])
  })

  it('tokenizes || operator', () => {
    const tokens = nonWsTypes('cmd1 || cmd2')
    expect(tokens).toEqual([
      ['command', 'cmd1'],
      ['operator', '||'],
      ['command', 'cmd2'],
    ])
  })

  it('tokenizes pipe operator', () => {
    const tokens = nonWsTypes('cat file | grep pattern')
    expect(tokens).toEqual([
      ['command', 'cat'],
      ['text', 'file'],
      ['operator', '|'],
      ['command', 'grep'],
      ['text', 'pattern'],
    ])
  })

  it('tokenizes semicolon operator', () => {
    const tokens = nonWsTypes('cd /tmp; ls')
    expect(tokens).toEqual([
      ['command', 'cd'],
      ['text', '/tmp'],
      ['operator', ';'],
      ['command', 'ls'],
    ])
  })

  it('tokenizes double-quoted strings', () => {
    const tokens = nonWsTypes('echo "hello world"')
    expect(tokens).toEqual([
      ['command', 'echo'],
      ['string', '"hello world"'],
    ])
  })

  it('tokenizes single-quoted strings', () => {
    const tokens = nonWsTypes("echo 'hello world'")
    expect(tokens).toEqual([
      ['command', 'echo'],
      ['string', "'hello world'"],
    ])
  })

  it('tokenizes double-quoted strings with escapes', () => {
    const tokens = nonWsTypes('echo "hello \\"world\\""')
    expect(tokens).toEqual([
      ['command', 'echo'],
      ['string', '"hello \\"world\\""'],
    ])
  })

  it('tokenizes variables', () => {
    const tokens = nonWsTypes('echo $HOME ${PATH} $(whoami)')
    expect(tokens).toEqual([
      ['command', 'echo'],
      ['variable', '$HOME'],
      ['variable', '${PATH}'],
      ['variable', '$(whoami)'],
    ])
  })

  it('tokenizes special variables', () => {
    const tokens = nonWsTypes('echo $? $! $# $$ $@')
    expect(tokens).toEqual([
      ['command', 'echo'],
      ['variable', '$?'],
      ['variable', '$!'],
      ['variable', '$#'],
      ['variable', '$$'],
      ['variable', '$@'],
    ])
  })

  it('tokenizes redirections', () => {
    const tokens = nonWsTypes('echo hello > /tmp/out 2>&1')
    expect(tokens).toEqual([
      ['command', 'echo'],
      ['text', 'hello'],
      ['redirect', '>'],
      ['text', '/tmp/out'],
      ['redirect', '2>&'],
      ['text', '1'],
    ])
  })

  it('tokenizes append redirection', () => {
    const tokens = nonWsTypes('echo hello >> /tmp/out')
    expect(tokens).toEqual([
      ['command', 'echo'],
      ['text', 'hello'],
      ['redirect', '>>'],
      ['text', '/tmp/out'],
    ])
  })

  it('tokenizes here-string redirect', () => {
    const tokens = nonWsTypes('cat <<<')
    expect(tokens).toEqual([
      ['command', 'cat'],
      ['redirect', '<<<'],
    ])
  })

  it('tokenizes keywords', () => {
    const tokens = nonWsTypes('if then else fi')
    expect(tokens).toEqual([
      ['keyword', 'if'],
      ['keyword', 'then'],
      ['keyword', 'else'],
      ['keyword', 'fi'],
    ])
  })

  it('tokenizes for loop keywords', () => {
    const tokens = nonWsTypes('for do done')
    expect(tokens).toEqual([
      ['keyword', 'for'],
      ['keyword', 'do'],
      ['keyword', 'done'],
    ])
  })

  it('tokenizes backslash continuation', () => {
    const tokens = tokenTypes('echo hello \\')
    expect(tokens).toEqual([
      ['command', 'echo'],
      ['text', ' '],
      ['text', 'hello'],
      ['text', ' '],
      ['continuation', '\\'],
    ])
  })

  it('tokenizes comments', () => {
    const tokens = nonWsTypes('# this is a comment')
    expect(tokens).toEqual([['comment', '# this is a comment']])
  })

  it('tokenizes inline comment', () => {
    const tokens = nonWsTypes('echo hello # comment')
    expect(tokens).toEqual([
      ['command', 'echo'],
      ['text', 'hello'],
      ['comment', '# comment'],
    ])
  })

  it('tokenizes variable assignments', () => {
    const tokens = nonWsTypes('VAR=value cmd')
    expect(tokens).toEqual([
      ['variable', 'VAR=value'],
      ['command', 'cmd'],
    ])
  })

  it('tokenizes ANSI-C quoted strings', () => {
    const tokens = nonWsTypes("echo $'hello\\nworld'")
    expect(tokens).toEqual([
      ['command', 'echo'],
      ['string', "$'hello\\nworld'"],
    ])
  })

  it('tokenizes empty line', () => {
    const tokens = tokenizeBashLine('')
    expect(tokens).toEqual([])
  })

  it('tokenizes complex formatted line', () => {
    const tokens = nonWsTypes('  && docker compose build --no-cache')
    expect(tokens).toEqual([
      ['operator', '&&'],
      ['command', 'docker'],
      ['text', 'compose'],
      ['text', 'build'],
      ['flag', '--no-cache'],
    ])
  })

  it('tokenizes pipe at start of continuation line', () => {
    const tokens = nonWsTypes('  | grep pattern')
    expect(tokens).toEqual([
      ['operator', '|'],
      ['command', 'grep'],
      ['text', 'pattern'],
    ])
  })

  it('tokenizes case pattern terminators', () => {
    const tokens = nonWsTypes(';;')
    expect(tokens).toEqual([['operator', ';;']])
  })
})
