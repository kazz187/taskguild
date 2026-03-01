// Package shellformat provides shell one-liner formatting for improved readability.
//
// It parses shell commands using mvdan.cc/sh/v3/syntax (the shfmt parser) and
// reformats them with proper indentation and line breaks. The formatted output
// uses backslash continuations, making it valid shell that can be copy-pasted
// and executed directly.
package shellformat

import (
	"bytes"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Option configures the formatter.
type Option func(*config)

// Variant represents a shell language variant.
type Variant int

const (
	// Bash is the default shell variant (GNU Bash).
	Bash Variant = iota
	// POSIX is the POSIX-compliant shell variant.
	POSIX
	// MkSH is the MirBSD Korn Shell variant.
	MkSH
)

type config struct {
	indent   int
	maxWidth int
	variant  Variant
}

func defaultConfig() *config {
	return &config{
		indent:   2,
		maxWidth: 80,
		variant:  Bash,
	}
}

// WithIndent sets the indentation width in spaces (default: 2).
func WithIndent(n int) Option {
	return func(c *config) { c.indent = n }
}

// WithMaxWidth sets the maximum line width threshold (default: 80).
// Statements shorter than this threshold are kept on a single line.
func WithMaxWidth(n int) Option {
	return func(c *config) { c.maxWidth = n }
}

// WithVariant sets the shell language variant (default: Bash).
func WithVariant(v Variant) Option {
	return func(c *config) { c.variant = v }
}

func (c *config) syntaxVariant() syntax.LangVariant {
	switch c.variant {
	case POSIX:
		return syntax.LangPOSIX
	case MkSH:
		return syntax.LangMirBSDKorn
	default:
		return syntax.LangBash
	}
}

// Format parses a shell one-liner and formats it with proper indentation
// and line breaks for readability. The formatted output uses backslash
// continuations and is valid shell that can be copy-pasted and executed.
//
// Short statements that fit within the configured max width are kept
// on a single line. Longer statements have their binary operators
// (&&, ||, |) placed at the beginning of continuation lines.
//
// On parse error, the original input is returned unchanged with a nil error.
func Format(input string, opts ...Option) (string, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return "", nil
	}

	parser := syntax.NewParser(
		syntax.Variant(cfg.syntaxVariant()),
		syntax.KeepComments(true),
	)

	prog, err := parser.Parse(strings.NewReader(input), "")
	if err != nil {
		return input, nil
	}

	f := &formatter{
		width:   cfg.indent,
		maxW:    cfg.maxWidth,
		printer: syntax.NewPrinter(syntax.Indent(uint(cfg.indent)), syntax.SpaceRedirects(true)),
	}

	f.file(prog)
	return strings.TrimRight(f.buf.String(), "\n"), nil
}

// formatter walks the AST and produces formatted output.
type formatter struct {
	buf     bytes.Buffer
	indent  int             // current indent level
	width   int             // spaces per indent level
	maxW    int             // max line width
	printer *syntax.Printer // for rendering leaf nodes
}

// nodeStr renders a syntax node to a compact string using the standard printer.
func (f *formatter) nodeStr(node syntax.Node) string {
	var buf bytes.Buffer
	f.printer.Print(&buf, node)
	return strings.TrimRight(buf.String(), "\n")
}

// indentStr returns the current indentation as a string.
func (f *formatter) indentStr() string {
	return strings.Repeat(" ", f.indent*f.width)
}

// writeIndent writes the current indentation to the buffer.
func (f *formatter) writeIndent() {
	f.buf.WriteString(f.indentStr())
}

// availWidth returns the available width at the current indentation.
func (f *formatter) availWidth() int {
	w := f.maxW - f.indent*f.width
	if w < 20 {
		w = 20
	}
	return w
}

// file formats a File node (list of statements).
func (f *formatter) file(prog *syntax.File) {
	for i, stmt := range prog.Stmts {
		if i > 0 {
			f.buf.WriteByte('\n')
		}
		f.stmt(stmt)
	}
}

// stmt formats a Stmt node. It tries the inline form first.
// If the inline form doesn't fit within the max width, or the statement
// contains a compound command (BinaryCmd, if, for, while, case, etc.)
// that benefits from expansion, it formats with proper line breaks.
func (f *formatter) stmt(s *syntax.Stmt) {
	// For compound commands that always benefit from expansion,
	// skip the inline check and go straight to expansion.
	needsExpansion := false
	switch s.Cmd.(type) {
	case *syntax.BinaryCmd:
		needsExpansion = true
	case *syntax.IfClause, *syntax.ForClause, *syntax.WhileClause, *syntax.CaseClause:
		needsExpansion = true
	case *syntax.FuncDecl:
		needsExpansion = true
	}

	if !needsExpansion {
		// Try inline form for simple commands.
		flat := f.nodeStr(s)
		if !strings.Contains(flat, "\n") && len(flat) <= f.availWidth() {
			f.buf.WriteString(flat)
			return
		}
	}

	// Expand based on command type.
	if s.Negated {
		f.buf.WriteString("! ")
	}

	if s.Cmd != nil {
		switch cmd := s.Cmd.(type) {
		case *syntax.BinaryCmd:
			f.binaryCmd(cmd)
		case *syntax.IfClause:
			f.ifClause(cmd)
		case *syntax.ForClause:
			f.forClause(cmd)
		case *syntax.WhileClause:
			f.whileClause(cmd)
		case *syntax.CaseClause:
			f.caseClause(cmd)
		case *syntax.Block:
			f.block(cmd)
		case *syntax.Subshell:
			f.subshell(cmd)
		case *syntax.FuncDecl:
			f.funcDecl(cmd)
		default:
			f.buf.WriteString(f.nodeStr(s.Cmd))
		}
	}

	// For non-BinaryCmd, handle outer redirects.
	if _, ok := s.Cmd.(*syntax.BinaryCmd); !ok {
		for _, r := range s.Redirs {
			f.buf.WriteByte(' ')
			f.writeRedirect(r)
		}
	}

	if s.Background {
		f.buf.WriteString(" &")
	}
}

// chainElem represents one element in a flattened binary command chain.
type chainElem struct {
	op   string       // operator before this element ("" for first)
	stmt *syntax.Stmt // the statement
}

// flattenBinaryCmd flattens a left-associative binary command tree
// into a linear chain of (operator, statement) pairs.
func flattenBinaryCmd(cmd *syntax.BinaryCmd) []chainElem {
	var chain []chainElem
	collectBinary(cmd, &chain)
	return chain
}

func collectBinary(cmd *syntax.BinaryCmd, chain *[]chainElem) {
	// Flatten the left side if it's also a bare BinaryCmd.
	if leftBin, ok := cmd.X.Cmd.(*syntax.BinaryCmd); ok && isBareBinaryStmt(cmd.X) {
		collectBinary(leftBin, chain)
	} else {
		*chain = append(*chain, chainElem{stmt: cmd.X})
	}

	op := cmd.Op.String()

	// Flatten the right side if it's also a bare BinaryCmd.
	if rightBin, ok := cmd.Y.Cmd.(*syntax.BinaryCmd); ok && isBareBinaryStmt(cmd.Y) {
		var rightChain []chainElem
		collectBinary(rightBin, &rightChain)
		if len(rightChain) > 0 {
			rightChain[0].op = op
			*chain = append(*chain, rightChain...)
		}
	} else {
		*chain = append(*chain, chainElem{op: op, stmt: cmd.Y})
	}
}

// isBareBinaryStmt returns true if the Stmt is a "bare" wrapper around
// a BinaryCmd with no extra attributes (negation, redirects, background).
func isBareBinaryStmt(s *syntax.Stmt) bool {
	return !s.Negated && !s.Background && len(s.Redirs) == 0
}

// binaryCmd formats a BinaryCmd node by flattening the chain and
// rendering each element on its own line with the operator as prefix.
func (f *formatter) binaryCmd(cmd *syntax.BinaryCmd) {
	chain := flattenBinaryCmd(cmd)

	// Estimate inline length.
	totalLen := 0
	for i, elem := range chain {
		if i > 0 {
			totalLen += 1 + len(elem.op) + 1 // " OP "
		}
		totalLen += len(f.nodeStr(elem.stmt))
	}

	// Keep inline if: chain has only 2 elements AND fits within width.
	// Chains with 3+ elements are always expanded for readability.
	if len(chain) <= 2 && totalLen <= f.availWidth() {
		// Render inline.
		for i, elem := range chain {
			if i > 0 {
				f.buf.WriteByte(' ')
				f.buf.WriteString(elem.op)
				f.buf.WriteByte(' ')
			}
			f.buf.WriteString(f.nodeStr(elem.stmt))
		}
		return
	}

	// Multi-line rendering with operators at the start of continuation lines.
	for i, elem := range chain {
		if i > 0 {
			f.buf.WriteString(" \\\n")
			f.writeIndent()
			f.buf.WriteString(strings.Repeat(" ", f.width)) // sub-indent for the operator
			f.buf.WriteString(elem.op)
			f.buf.WriteByte(' ')
		}
		// Render the element's statement.
		// Use nodeStr for simple stmts; for compound stmts that need expansion
		// we render them recursively.
		f.stmtInChain(elem.stmt)
	}
}

// stmtInChain renders a statement that is part of a binary command chain.
// Simple statements are rendered inline; compound statements that would
// benefit from expansion are rendered with proper indentation.
func (f *formatter) stmtInChain(s *syntax.Stmt) {
	flat := f.nodeStr(s)
	if !strings.Contains(flat, "\n") {
		f.buf.WriteString(flat)
		return
	}

	// Compound statement needs expansion. Increase indent for the body.
	if s.Negated {
		f.buf.WriteString("! ")
	}
	if s.Cmd != nil {
		switch cmd := s.Cmd.(type) {
		case *syntax.Subshell:
			f.subshell(cmd)
		case *syntax.Block:
			f.block(cmd)
		case *syntax.IfClause:
			f.ifClause(cmd)
		case *syntax.ForClause:
			f.forClause(cmd)
		case *syntax.WhileClause:
			f.whileClause(cmd)
		case *syntax.CaseClause:
			f.caseClause(cmd)
		default:
			f.buf.WriteString(f.nodeStr(s.Cmd))
		}
	}
	for _, r := range s.Redirs {
		f.buf.WriteByte(' ')
		f.writeRedirect(r)
	}
	if s.Background {
		f.buf.WriteString(" &")
	}
}

// writeRedirect writes a redirect to the buffer.
func (f *formatter) writeRedirect(r *syntax.Redirect) {
	if r.N != nil {
		f.buf.WriteString(r.N.Value)
	}
	f.buf.WriteString(r.Op.String())
	f.buf.WriteByte(' ')
	if r.Word != nil {
		f.buf.WriteString(f.nodeStr(r.Word))
	}
	if r.Hdoc != nil {
		f.buf.WriteString(f.nodeStr(r.Hdoc))
	}
}

// stmtList formats a list of statements, each on its own line.
func (f *formatter) stmtList(stmts []*syntax.Stmt) {
	for i, s := range stmts {
		if i > 0 {
			f.buf.WriteByte('\n')
		}
		f.writeIndent()
		f.stmt(s)
	}
}

// ifClause formats an if/elif/else clause with proper indentation.
func (f *formatter) ifClause(cmd *syntax.IfClause) {
	f.ifClauseInner(cmd, "if")
}

func (f *formatter) ifClauseInner(cmd *syntax.IfClause, keyword string) {
	if keyword == "else" {
		// "else" block (no condition)
		f.buf.WriteString("else")
	} else {
		// "if" or "elif" block
		f.buf.WriteString(keyword)
		f.buf.WriteByte(' ')
		// Render condition statements inline.
		for i, s := range cmd.Cond {
			if i > 0 {
				f.buf.WriteString("; ")
			}
			f.buf.WriteString(f.nodeStr(s))
		}
		f.buf.WriteString("; then")
	}
	f.buf.WriteByte('\n')

	// Body
	f.indent++
	f.stmtList(cmd.Then)
	f.indent--

	// Else/elif
	if cmd.Else != nil {
		f.buf.WriteByte('\n')
		f.writeIndent()
		if len(cmd.Else.Cond) > 0 {
			f.ifClauseInner(cmd.Else, "elif")
		} else {
			f.ifClauseInner(cmd.Else, "else")
		}
	}

	// Only write "fi" at the outermost level (non-elif/else).
	if keyword == "if" {
		f.buf.WriteByte('\n')
		f.writeIndent()
		f.buf.WriteString("fi")
	}
}

// forClause formats a for/select loop.
func (f *formatter) forClause(cmd *syntax.ForClause) {
	if cmd.Select {
		f.buf.WriteString("select ")
	} else {
		f.buf.WriteString("for ")
	}

	switch loop := cmd.Loop.(type) {
	case *syntax.WordIter:
		f.buf.WriteString(loop.Name.Value)
		if loop.InPos.IsValid() {
			f.buf.WriteString(" in")
			for _, w := range loop.Items {
				f.buf.WriteByte(' ')
				f.buf.WriteString(f.nodeStr(w))
			}
		}
	case *syntax.CStyleLoop:
		f.buf.WriteString(f.nodeStr(loop))
	}

	f.buf.WriteString("; do")
	f.buf.WriteByte('\n')

	f.indent++
	f.stmtList(cmd.Do)
	f.indent--

	f.buf.WriteByte('\n')
	f.writeIndent()
	f.buf.WriteString("done")
}

// whileClause formats a while/until loop.
func (f *formatter) whileClause(cmd *syntax.WhileClause) {
	if cmd.Until {
		f.buf.WriteString("until ")
	} else {
		f.buf.WriteString("while ")
	}

	// Render condition.
	for i, s := range cmd.Cond {
		if i > 0 {
			f.buf.WriteString("; ")
		}
		f.buf.WriteString(f.nodeStr(s))
	}
	f.buf.WriteString("; do")
	f.buf.WriteByte('\n')

	f.indent++
	f.stmtList(cmd.Do)
	f.indent--

	f.buf.WriteByte('\n')
	f.writeIndent()
	f.buf.WriteString("done")
}

// caseClause formats a case statement.
func (f *formatter) caseClause(cmd *syntax.CaseClause) {
	f.buf.WriteString("case ")
	f.buf.WriteString(f.nodeStr(cmd.Word))
	f.buf.WriteString(" in")

	for _, item := range cmd.Items {
		f.buf.WriteByte('\n')
		f.writeIndent()
		// Pattern
		for i, pat := range item.Patterns {
			if i > 0 {
				f.buf.WriteString(" | ")
			}
			f.buf.WriteString(f.nodeStr(pat))
		}
		f.buf.WriteByte(')')

		if len(item.Stmts) > 0 {
			f.buf.WriteByte('\n')
			f.indent++
			f.stmtList(item.Stmts)
			f.buf.WriteByte('\n')
			f.writeIndent()
			f.buf.WriteString(item.Op.String())
			f.indent--
		}
	}

	f.buf.WriteByte('\n')
	f.writeIndent()
	f.buf.WriteString("esac")
}

// block formats a { ... } block.
func (f *formatter) block(cmd *syntax.Block) {
	f.buf.WriteString("{")

	if len(cmd.Stmts) > 0 {
		f.buf.WriteByte('\n')
		f.indent++
		f.stmtList(cmd.Stmts)
		f.indent--
		f.buf.WriteByte('\n')
		f.writeIndent()
	}
	f.buf.WriteString("}")
}

// subshell formats a ( ... ) subshell.
func (f *formatter) subshell(cmd *syntax.Subshell) {
	// Try inline first.
	flat := f.nodeStr(cmd)
	if !strings.Contains(flat, "\n") && len(flat) <= f.availWidth() {
		f.buf.WriteString(flat)
		return
	}

	f.buf.WriteString("(")

	if len(cmd.Stmts) > 0 {
		f.buf.WriteByte('\n')
		f.indent++
		f.stmtList(cmd.Stmts)
		f.indent--
		f.buf.WriteByte('\n')
		f.writeIndent()
	}
	f.buf.WriteString(")")
}

// funcDecl formats a function declaration.
func (f *formatter) funcDecl(cmd *syntax.FuncDecl) {
	if cmd.RsrvWord {
		f.buf.WriteString("function ")
	}
	f.buf.WriteString(cmd.Name.Value)
	if cmd.Parens {
		f.buf.WriteString("()")
	}
	f.buf.WriteByte(' ')

	// The body is a *Stmt which usually contains a Block.
	if cmd.Body != nil && cmd.Body.Cmd != nil {
		if blk, ok := cmd.Body.Cmd.(*syntax.Block); ok {
			f.block(blk)
		} else {
			f.buf.WriteString(f.nodeStr(cmd.Body))
		}
		for _, r := range cmd.Body.Redirs {
			f.buf.WriteByte(' ')
			f.writeRedirect(r)
		}
	}
}
