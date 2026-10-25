package types2

import (
	"github.com/matjam/plutonium/internal/syntax/ast"
	. "github.com/matjam/plutonium/internal/types2/errors"
)

// Plutonium: the checker recurses once per level of nesting in expressions
// and type expressions. A compiler with a growing stack (Go's) can recurse
// as deep as the parser allows; one on a fixed stack (the self-hosted
// compiler, language spec section 20.3's //pu:stack) must stop before the stack
// runs out. enter and leave bracket each level: past Config.MaxNesting, or
// when Config.StackCheck says the stack is running low, the construct is
// reported once and not checked.

// enter records one more level of nesting at e and reports whether the
// checker may descend into it. Every call that returns true must be matched
// by a call of leave.
func (check *Checker) enter(e ast.Expr) bool {
	limit := check.conf.MaxNesting
	if limit <= 0 {
		limit = DefaultMaxNesting
	}
	tooDeep := check.nesting >= limit
	if !tooDeep && check.conf.StackCheck != nil && check.nesting%stackCheckEvery == stackCheckEvery-1 {
		tooDeep = !check.conf.StackCheck.StackOK()
	}
	if tooDeep {
		if !check.nestingAt {
			check.nestingAt = true
			check.errorf(e, UnsupportedFeature, "expression or type nested too deeply (%d levels) for the type checker", check.nesting)
		}
		return false
	}
	check.nesting++
	return true
}

// leave ends a level of nesting begun by enter.
func (check *Checker) leave() { check.nesting-- }
