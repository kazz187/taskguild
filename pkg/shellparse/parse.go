// Package shellparse extracts individual commands from shell one-liners.
//
// It uses mvdan.cc/sh/v3/syntax to parse shell scripts into an AST, then
// walks the tree to collect every simple command along with its redirects.
// Compound constructs (&&, ||, ;, |, for, while, if, case, subshells, command
// substitutions) are recursively decomposed so that every leaf command is
// reported individually.
package shellparse

import (
	"bytes"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// ParsedCommand represents a single simple command extracted from a one-liner.
type ParsedCommand struct {
	// Raw is the reconstructed command string (e.g. "git status").
	Raw string
	// Executable is the command name (e.g. "git").
	Executable string
	// Args contains the arguments after the executable (e.g. ["status"]).
	Args []string
	// Redirects lists file redirections attached to this command.
	Redirects []Redirect
}

// Redirect describes a single I/O redirection.
type Redirect struct {
	// Op is the redirection operator (e.g. ">", ">>", "2>", "&>").
	Op string
	// Path is the target file path (e.g. "/dev/null", "./out.txt").
	Path string
}

// ParseResult holds the complete result of parsing a shell one-liner.
type ParseResult struct {
	// Commands contains all individual commands found in the input.
	Commands []ParsedCommand
	// Original is the input string that was parsed.
	Original string
}

// Parse decomposes a shell one-liner into individual commands.
//
// It handles:
//   - Command separators: &&, ||, ;, &, | (pipe)
//   - Control structures: for, while, until, if/elif/else, case
//   - Grouping: (...) subshells, {...} blocks
//   - Command substitution: $(...) and backticks (recursively)
//   - Redirections: >, >>, 2>, &>, 2>&1, <, etc.
//
// On parse failure the entire input is returned as a single command (fallback).
func Parse(input string) *ParseResult {
	input = strings.TrimSpace(input)
	if input == "" {
		return &ParseResult{Original: input}
	}

	parser := syntax.NewParser(
		syntax.Variant(syntax.LangBash),
		syntax.KeepComments(false),
	)

	prog, err := parser.Parse(strings.NewReader(input), "")
	if err != nil {
		// Fallback: treat the entire input as one opaque command.
		return fallbackResult(input)
	}

	p := &extractor{
		printer: syntax.NewPrinter(syntax.Indent(0)),
	}

	for _, stmt := range prog.Stmts {
		p.walkStmt(stmt)
	}

	if len(p.commands) == 0 {
		return fallbackResult(input)
	}

	return &ParseResult{
		Commands: p.commands,
		Original: input,
	}
}

// fallbackResult returns a ParseResult treating the whole input as one command.
func fallbackResult(input string) *ParseResult {
	parts := strings.Fields(input)
	cmd := ParsedCommand{
		Raw:        input,
		Executable: "",
		Args:       nil,
	}
	if len(parts) > 0 {
		cmd.Executable = parts[0]
		if len(parts) > 1 {
			cmd.Args = parts[1:]
		}
	}
	return &ParseResult{
		Commands: []ParsedCommand{cmd},
		Original: input,
	}
}

// extractor walks the AST and collects simple commands.
type extractor struct {
	printer  *syntax.Printer
	commands []ParsedCommand
}

// nodeStr renders a syntax node back to its string representation.
func (e *extractor) nodeStr(node syntax.Node) string {
	var buf bytes.Buffer
	e.printer.Print(&buf, node)
	return strings.TrimRight(buf.String(), "\n")
}

// wordStr renders a syntax.Word to a string.
func (e *extractor) wordStr(w *syntax.Word) string {
	return e.nodeStr(w)
}

// walkStmt processes a single statement node.
func (e *extractor) walkStmt(stmt *syntax.Stmt) {
	if stmt == nil || stmt.Cmd == nil {
		return
	}

	// Collect redirects attached to the statement.
	redirs := e.extractRedirects(stmt.Redirs)

	switch cmd := stmt.Cmd.(type) {
	case *syntax.CallExpr:
		e.handleCallExpr(cmd, redirs)

	case *syntax.BinaryCmd:
		// For binary commands (&&, ||, |), walk both sides recursively.
		// Redirects on the outer Stmt apply to the whole pipeline,
		// but we attach them only to simple commands found inside.
		e.walkStmt(cmd.X)
		e.walkStmt(cmd.Y)

	case *syntax.IfClause:
		e.walkIfClause(cmd)

	case *syntax.ForClause:
		for _, s := range cmd.Do {
			e.walkStmt(s)
		}

	case *syntax.WhileClause:
		// Walk condition statements too (they may contain commands).
		for _, s := range cmd.Cond {
			e.walkStmt(s)
		}
		for _, s := range cmd.Do {
			e.walkStmt(s)
		}

	case *syntax.CaseClause:
		for _, item := range cmd.Items {
			for _, s := range item.Stmts {
				e.walkStmt(s)
			}
		}

	case *syntax.Block:
		for _, s := range cmd.Stmts {
			e.walkStmt(s)
		}

	case *syntax.Subshell:
		for _, s := range cmd.Stmts {
			e.walkStmt(s)
		}

	case *syntax.FuncDecl:
		// Walk the function body.
		if cmd.Body != nil {
			e.walkStmt(cmd.Body)
		}

	case *syntax.DeclClause:
		// Builtins like export, declare, local, readonly, typeset.
		e.handleDeclClause(cmd, redirs)

	case *syntax.LetClause:
		// let expressions: treat as a single command.
		e.addCommand("let", nil, e.nodeStr(cmd), redirs)

	case *syntax.TestClause:
		// [[ ... ]] test expressions.
		e.addCommand("[[", nil, e.nodeStr(cmd), redirs)

	case *syntax.ArithmCmd:
		// (( ... )) arithmetic.
		e.addCommand("((", nil, e.nodeStr(cmd), redirs)

	case *syntax.TimeClause:
		// time command: walk the inner statement.
		if cmd.Stmt != nil {
			e.walkStmt(cmd.Stmt)
		}

	case *syntax.CoprocClause:
		if cmd.Stmt != nil {
			e.walkStmt(cmd.Stmt)
		}

	default:
		// Unknown command type: render as-is.
		raw := e.nodeStr(stmt.Cmd)
		if raw != "" {
			e.addCommand(raw, nil, raw, redirs)
		}
	}
}

// walkIfClause recursively walks if/elif/else clauses.
func (e *extractor) walkIfClause(cmd *syntax.IfClause) {
	// Walk condition commands.
	for _, s := range cmd.Cond {
		e.walkStmt(s)
	}
	// Walk then body.
	for _, s := range cmd.Then {
		e.walkStmt(s)
	}
	// Walk else/elif.
	if cmd.Else != nil {
		e.walkIfClause(cmd.Else)
	}
}

// handleCallExpr processes a simple command (CallExpr) and also
// recursively walks any command substitutions found in arguments.
func (e *extractor) handleCallExpr(cmd *syntax.CallExpr, outerRedirs []Redirect) {
	if len(cmd.Args) == 0 {
		// Possible variable assignment only (e.g. FOO=bar).
		if len(cmd.Assigns) > 0 {
			// Render the assignment as the command.
			parts := make([]string, 0, len(cmd.Assigns))
			for _, a := range cmd.Assigns {
				parts = append(parts, e.nodeStr(a))
			}
			raw := strings.Join(parts, " ")
			e.addCommand(parts[0], parts[1:], raw, outerRedirs)
		}
		return
	}

	// CallExpr has no Redirs field; redirects are on the parent Stmt.
	allRedirs := outerRedirs

	// Build the command string from words.
	words := make([]string, 0, len(cmd.Args))
	for _, arg := range cmd.Args {
		words = append(words, e.wordStr(arg))
	}

	// Prepend variable assignments if any.
	var prefixes []string
	for _, a := range cmd.Assigns {
		prefixes = append(prefixes, e.nodeStr(a))
	}

	var raw string
	if len(prefixes) > 0 {
		raw = strings.Join(prefixes, " ") + " " + strings.Join(words, " ")
	} else {
		raw = strings.Join(words, " ")
	}

	executable := words[0]
	var args []string
	if len(words) > 1 {
		args = words[1:]
	}

	e.addCommand(executable, args, raw, allRedirs)

	// Recursively walk command substitutions inside all words.
	for _, arg := range cmd.Args {
		e.walkWordForCmdSubst(arg)
	}
}

// handleDeclClause processes declaration builtins (export, declare, local, etc.).
func (e *extractor) handleDeclClause(cmd *syntax.DeclClause, redirs []Redirect) {
	words := []string{cmd.Variant.Value}
	for _, a := range cmd.Args {
		words = append(words, e.nodeStr(a))
	}
	raw := strings.Join(words, " ")

	var args []string
	if len(words) > 1 {
		args = words[1:]
	}
	e.addCommand(cmd.Variant.Value, args, raw, redirs)
}

// walkWordForCmdSubst recursively finds command substitutions within a Word
// and walks their inner commands.
func (e *extractor) walkWordForCmdSubst(w *syntax.Word) {
	if w == nil {
		return
	}
	syntax.Walk(w, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.CmdSubst:
			for _, s := range n.Stmts {
				e.walkStmt(s)
			}
			return false // Don't descend further; we've already walked the stmts.
		}
		return true
	})
}

// extractRedirects converts syntax redirections to our Redirect type.
func (e *extractor) extractRedirects(redirs []*syntax.Redirect) []Redirect {
	if len(redirs) == 0 {
		return nil
	}
	result := make([]Redirect, 0, len(redirs))
	for _, r := range redirs {
		op := ""
		if r.N != nil {
			op = r.N.Value
		}
		op += r.Op.String()

		path := ""
		if r.Word != nil {
			path = e.wordStr(r.Word)
		}

		result = append(result, Redirect{
			Op:   op,
			Path: path,
		})
	}
	return result
}

// addCommand appends a ParsedCommand to the collected list.
func (e *extractor) addCommand(executable string, args []string, raw string, redirs []Redirect) {
	e.commands = append(e.commands, ParsedCommand{
		Raw:        raw,
		Executable: executable,
		Args:       args,
		Redirects:  redirs,
	})
}
