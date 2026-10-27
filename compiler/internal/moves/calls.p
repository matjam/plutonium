// Translated by go2pu from internal/escape/facts.go at the go-cutover tag; edited by hand since.

package moves

import (
	"compiler/compat"
	"compiler/internal/syntax/ast"
	"compiler/pu"
)

// Calls is what the escape analysis found about the arguments of calls,
// for the move checker: which views a call keeps past its return, and
// where. A view of a local owner that no call keeps lasts only for the
// call, so the owner's variable can be followed along the control flow as
// if its address had not been taken (language spec sections 18.3 and
// 18.6). The escape analysis (internal/escape2, CallFacts) fills it.
type Calls struct {
	args map[ast.Expr]*ArgFacts
}

// ArgFacts says what a call does with the pointers in one argument, given
// by its expression (for a method call, the receiver expression x of
// x.M(...), whose address the call takes if the method has a pointer
// receiver).
type ArgFacts struct {
	// Self: the callee may keep a pointer to the memory the argument
	// points to (not merely to memory loaded through it) past the call:
	// it stores it somewhere, or returns it.
	Self bool
	// Returned: a result may hold pointers derived from the argument.
	Returned bool
	// Into are the arguments into whose memory the callee may store
	// pointers derived from this one. A nil entry is a destination that
	// is not an argument expression of the call (unknown).
	Into []Into
}

// Into is a destination of a store: the argument expression, and whether
// the pointers land in the memory it points to itself (Direct) rather than
// in memory loaded through it.
type Into struct {
	Arg    ast.Expr
	Direct bool
}

// Arg returns what the analysis knows of the call argument e, or nil if
// nothing is known (assume the worst).
func (c *Calls) Arg(e ast.Expr) *ArgFacts {
	if c == nil {
		return nil
	}
	return c.args[e]
}

// NewCalls returns an empty Calls, for the escape analysis to fill with Add.
func NewCalls() *Calls { return pu.New(Calls{args: map[ast.Expr]*ArgFacts{}}) }

// Clone returns a deep copy of c, from the current allocator: the build
// runs the escape analysis as a scratch step and keeps only this.
func (c *Calls) Clone() *Calls {
	if c == nil {
		return nil
	}
	n := pu.New(Calls{args: make(map[ast.Expr]*ArgFacts, len(c.args))})
	for e, af := range c.args {
		cp := pu.New(*af)
		cp.Into = compat.Append([]Into(nil), af.Into...)
		n.args[e] = cp
	}
	return n
}

// Add merges af into what c knows of the call argument e.
func (c *Calls) Add(e ast.Expr, af *ArgFacts) {
	if c.args[e] == nil {
		c.args[e] = pu.New(ArgFacts{})
	}
	c.args[e].merge(af)
}

// merge adds what o says to af.
func (af *ArgFacts) merge(o *ArgFacts) {
	af.Self = af.Self || o.Self
	af.Returned = af.Returned || o.Returned
	af.Into = compat.Append(af.Into, o.Into...)
}
