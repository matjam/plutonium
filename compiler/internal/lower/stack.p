// Translated by go2pu from internal/lower/stack.go at the go-cutover tag; edited by hand since.

package lower

import (
	"compiler/compat"
	"compiler/internal/diag"
	"compiler/internal/syntax/ast"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/strconv"
	"pluto/strings"
)

// The //pu:stack directive on main.main sets the size of the stack the
// hosted runtime runs the program on (language spec section 20.3): it reserves
// that much address space, maps it lazily and puts a guard region below it,
// instead of using the stack the system gave the process (usually 8 MiB).
// A compiler or anything else that recurses deeply asks for a large one:
//
//	//pu:stack 256M
//	func main() { ... }

const (
	minMainStack = 64 << 10 // smallest //pu:stack size
	maxMainStack = 64 << 30 // largest
	pageSize     = 4096
)

// isStackDirective reports whether d (a directive without "//pu:") is
// //pu:stack.
func isStackDirective(d string) bool { return d == "stack" || strings.HasPrefix(d, "stack ") }

// mainStackSize returns the stack size main's //pu:stack directive asks for,
// rounded up to whole pages, or 0 if it has none. It reports a malformed
// directive (E2603), a size outside 64K to 64G (E2561), and a directive in a
// freestanding build, which has no runtime to reserve the stack (E2560).
func (l *Lowerer) mainStackSize(mainFn *types2.Func) uint64 {
	d := l.decls[mainFn]
	if d == nil || d.decl.Doc == nil {
		return 0
	}
	for _, c := range d.decl.Doc.List {
		dir, ok := strings.CutPrefix(c.Text, "//pu:")
		if !ok || !isStackDirective(strings.TrimSpace(dir)) {
			continue
		}
		if l.cfg.Freestanding {
			l.errorf(c.Pos(), diag.StackDirective, "//pu:stack is not supported in freestanding builds (the program sets up its own stack)")
			return 0
		}
		size, err, outside := parseStackSize(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(dir), "stack")))
		if err != "" {
			code := diag.DirectiveArgument
			if outside {
				code = diag.StackSize
			}
			l.errorf(c.Pos(), code, "//pu:stack: %s", pu.Box(err))
			return 0
		}
		return size
	}
	return 0
}

// parseStackSize parses the size of a //pu:stack directive: a decimal number
// of bytes with an optional K, M or G suffix (powers of 1024), between 64K
// and 64G. It returns the size rounded up to a whole page, or an error
// message and whether the error is a well-formed size outside that range.
func parseStackSize(s string) (uint64, string, bool) {
	if s == "" {
		return 0, "missing size (for example //pu:stack 256M)", false
	}
	mult := uint64(1)
	switch s[len(s)-1] {
	case 'K', 'k':
		mult = 1 << 10
	case 'M', 'm':
		mult = 1 << 20
	case 'G', 'g':
		mult = 1 << 30
	}
	digits := s
	if mult != 1 {
		digits = strings.Slice(s, 0, len(s)-1)
	}
	n, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return 0, compat.Concat("invalid size ", strconv.Quote(s), " (want a number of bytes with an optional K, M or G suffix)"), false
	}
	if n > maxMainStack/mult || n*mult > maxMainStack {
		return 0, compat.Concat("size ", s, " is larger than 64G"), true
	}
	size := n * mult
	if size < minMainStack {
		return 0, compat.Concat("size ", s, " is smaller than 64K"), true
	}
	return (size + pageSize - 1) &^ (pageSize - 1), "", false
}

// checkStackDirective reports a //pu:stack directive on a function other
// than main.main, where it would have no effect.
func (s *fnState) checkStackDirective(doc *ast.CommentGroup) {
	if doc == nil {
		return
	}
	for _, c := range doc.List {
		dir, ok := strings.CutPrefix(c.Text, "//pu:")
		if !ok || !isStackDirective(strings.TrimSpace(dir)) {
			continue
		}
		mainFn, _ := s.l.prog.Main.Types.Scope().Lookup("main").(*types2.Func)
		if mainFn == nil || s.f.Name != funcSymName(mainFn, nil) {
			s.l.errorf(c.Pos(), diag.MisplacedDirective, "//pu:stack applies only to func main in package main")
		}
	}
}
