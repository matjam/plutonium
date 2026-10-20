package lower

import (
	"github.com/matjam/plutonium/internal/scoped"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Where counted values are released (strcount.go): the cleanup stack.
//
// Every statement pushes an entry for the temporaries it makes, and every
// block, and every statement with an implicit block (if, for, switch,
// range, a case clause), an entry that also holds the variables declared in
// it. An entry is released when its statement or block ends, and on every
// jump out of it: return releases them all, break and continue those
// inside the loop or switch, goto those inside the label's block. Frame
// slots (interface boxes, &T{...} literals, saved defer arguments) and the
// parameters a function changes are released when it returns, after its
// deferred calls have run.
//
// A released slot is zeroed, and every slot is zeroed when the function is
// entered, so a release on a path where the slot was never filled (an early
// return before a declaration, a temporary of an operand that && skipped)
// does nothing.

// A countedSlot is storage that holds references: base (a slot address in
// the entry block) plus off.
type countedSlot struct {
	base *ssa.Value
	off  int64
	T    types2.Type
	tmp  *ownedTemp // a temporary, which needs no release once consumed
	pos  token.Pos  // where a variable is declared, for goto
	// first says the slot holds a sync.Scope variable, which its block
	// drops before its other variables: the Scope waits for the tasks it
	// started, which may use them (language spec section 19.4).
	first bool
	// v is the owner variable the slot holds, which is not dropped where
	// the move checker knows it holds nothing (scope.go).
	v *types2.Var
}

// A cleanupEntry is one level of the cleanup stack.
type cleanupEntry struct {
	node  ast.Node // the statement or block, for goto
	scope bool     // variables declared here are released with it
	slots []countedSlot
}

// pushCleanup opens an entry for node.
func (s *fnState) pushCleanup(node ast.Node, scope bool) *cleanupEntry {
	e := &cleanupEntry{node: node, scope: scope}
	s.cleanups = append(s.cleanups, e)
	return e
}

// popCleanup closes the innermost entry, e, releasing what it holds if the
// code that falls out of it is reachable.
func (s *fnState) popCleanup(e *cleanupEntry) {
	if n := len(s.cleanups); n == 0 || s.cleanups[n-1] != e {
		s.l.internalf(s.pos, "unbalanced cleanup stack")
		return
	}
	if s.b != nil {
		s.leavingThrough(s.emptyAt(e.node), func() { s.releaseEntry(e) })
	}
	s.closeHolds(e.slots)
	s.cleanups = s.cleanups[:len(s.cleanups)-1]
}

// releaseEntry releases the slots of e, the last registered first.
func (s *fnState) releaseEntry(e *cleanupEntry) {
	s.releaseSlots(e.slots, true)
}

// releaseSlots releases slots, the last first. zero says to zero them
// after, for code that may run again and fill them on some paths only;
// a return need not.
func (s *fnState) releaseSlots(slots []countedSlot, zero bool) {
	defer s.collectCleanupGoexit()()
	for _, scopes := range [2]bool{true, false} {
		s.releaseSome(slots, zero, scopes)
	}
}

// releaseSome releases the slots that hold sync.Scope variables (scopes)
// or the others, the last first.
func (s *fnState) releaseSome(slots []countedSlot, zero, scopes bool) {
	for i := len(slots) - 1; i >= 0; i-- {
		c := slots[i]
		if c.first != scopes || c.tmp != nil && c.tmp.consumed {
			continue
		}
		held, known := s.leaving[c.v]
		if c.v != nil && known && !held.Box && len(held.Parts) == 0 {
			continue // it holds nothing (scope.go)
		}
		p := s.offPtr(c.base, c.off)
		from := len(s.callLog)
		restore := func() {}
		if c.pos.IsValid() {
			restore = s.at(c.pos) // errors name where the value was made
		}
		if c.v != nil && known {
			s.destroyHeld(p, c.T, held)
		} else {
			s.destroyAt(p, c.T) // an owner's drop glue, or a release (scope.go)
		}
		restore()
		if c.tmp != nil {
			s.noteRelease(c.tmp, from)
		}
		if zero {
			s.zero(p, s.l.sizeof(c.T))
		}
	}
}

// releaseAbove releases the entries above depth, for a jump out of them.
func (s *fnState) releaseAbove(depth int) {
	if s.b == nil {
		return
	}
	for i := len(s.cleanups) - 1; i >= depth; i-- {
		s.releaseEntry(s.cleanups[i])
	}
}

// releaseScopes releases, for a return, every open entry.
func (s *fnState) releaseScopes() {
	if s.b == nil {
		return
	}
	for i := len(s.cleanups) - 1; i >= 0; i-- {
		s.releaseSlots(s.cleanups[i].slots, false)
	}
}

// releaseFrame releases, for a return, what the function holds as a whole:
// the frame slots, the parameters it retained and the regions it reserved.
func (s *fnState) releaseFrame() {
	if s.b == nil {
		return
	}
	defer s.collectCleanupGoexit()()
	s.releaseSlots(s.frameSlots, false)
	s.releaseSlots(s.ownedParams, false)
	s.dropLocalRegions()
}

// addTemp registers a temporary with the innermost entry.
func (s *fnState) addTemp(t *ownedTemp) {
	c := countedSlot{base: t.addr, T: t.T, tmp: t, pos: s.pos}
	s.zeroAtEntry(c)
	if n := len(s.cleanups); n > 0 {
		s.cleanups[n-1].slots = append(s.cleanups[n-1].slots, c)
		return
	}
	s.l.internalf(s.pos, "counted temporary outside any statement")
}

// addVar registers the storage of local variable v with the innermost
// scope.
func (s *fnState) addVar(addr *ssa.Value, T types2.Type, v *types2.Var) {
	c := countedSlot{base: addr, T: T, pos: s.pos, first: scoped.IsScopeType(T), v: v}
	s.zeroAtEntry(c)
	for i := len(s.cleanups) - 1; i >= 0; i-- {
		if s.cleanups[i].scope {
			s.cleanups[i].slots = append(s.cleanups[i].slots, c)
			return
		}
	}
	s.l.internalf(s.pos, "counted variable outside any scope")
}

// frameSlot returns a new slot for a counted T that lives until the
// function returns.
func (s *fnState) frameSlot(T types2.Type, name string) *ssa.Value {
	if o := s.lastingFrame; o != nil && o != s {
		// A defer statement's argument in a range-over-func loop body:
		// it lives until the function the deferred call runs in returns.
		return s.outerPtr(o.frameSlot(T, name), o)
	}
	addr := s.temp(T, name)
	s.addFrameSlot(addr, 0, T)
	return addr
}

// addFrameSlot registers base+off, holding a T, to be released when the
// function returns.
func (s *fnState) addFrameSlot(base *ssa.Value, off int64, T types2.Type) {
	c := countedSlot{base: base, off: off, T: T, pos: s.pos}
	s.zeroAtEntry(c)
	s.frameSlots = append(s.frameSlots, c)
}

// zeroAtEntry arranges for a slot to be zeroed when the function is
// entered (see finishEntry).
func (s *fnState) zeroAtEntry(c countedSlot) {
	if c.base.Op != ssa.OpSlotAddr {
		s.l.internalf(s.pos, "counted storage is not a stack slot")
		return
	}
	s.entryZeros = append(s.entryZeros, c)
}

// startBody ends the entry block, which holds the function's setup, with a
// jump to the block where its body is lowered. The body block stays
// unsealed until finishEntry has added the zeroing of counted slots to the
// entry block, so that the body's first memory state includes it.
func (s *fnState) startBody() {
	body := s.newBlock(ssa.BlockInvalid)
	s.entryEnd = s.b
	s.endPlain(body)
	s.startBlock(body)
	s.bodyBlock = body
}

// finishEntry zeroes, in the entry block, every slot that holds references,
// then seals the body block.
func (s *fnState) finishEntry() {
	if s.entryEnd == nil {
		return
	}
	cur := s.b
	s.b = s.entryEnd
	for _, c := range s.entryZeros {
		s.zero(s.offPtr(c.base, c.off), s.l.sizeof(c.T))
	}
	s.zeroRegions()
	s.b = cur
	s.seal(s.bodyBlock)
}

// ----------------------------------------------------------------------------
// What a function does with its variables

// varFacts records what a function body does with a local variable.
type varFacts struct {
	assigned  bool // assigned after its declaration
	addrTaken bool // its address is taken, so others can change it
	captured  bool // a function literal refers to it
}

// analyzeVars scans a function body for the facts about its variables
// that string counting needs.
func analyzeVars(body ast.Node, info *types2.Info) map[*types2.Var]*varFacts {
	facts := map[*types2.Var]*varFacts{}
	get := func(v *types2.Var) *varFacts {
		f := facts[v]
		if f == nil {
			f = &varFacts{}
			facts[v] = f
		}
		return f
	}
	var bodies []ast.Node // function literals and range-over-func loops
	// Reading x through *(*T)(unsafe.Pointer(&x)) uses the address only to
	// reinterpret the value, so it does not let anything change x. Such
	// reads are found at the * (seen before the & inside it), unless the
	// expression is assigned to.
	readOnly := map[*ast.UnaryExpr]bool{}
	written := map[ast.Expr]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			bodies = append(bodies, n)
		case *ast.StarExpr:
			if !written[n] {
				if amp := reinterpretedAddr(n, info); amp != nil {
					readOnly[amp] = true
				}
			}
		case *ast.UnaryExpr:
			if n.Op == token.AND && !readOnly[n] {
				if v := rootVar(n.X, info); v != nil {
					get(v).addrTaken = true
				}
			}
		case *ast.SliceExpr:
			if tv, ok := info.Types[n.X]; ok && tv.Type != nil {
				if _, isArr := tv.Type.Underlying().(*types2.Array); isArr {
					if v := rootVar(n.X, info); v != nil {
						get(v).addrTaken = true
					}
				}
			}
		case *ast.SelectorExpr:
			// A method with a pointer receiver called on (or taken as a
			// value of) an addressable variable takes its address.
			if sel := info.Selections[n]; sel != nil && sel.Kind() == types2.MethodVal {
				if fn, ok := sel.Obj().(*types2.Func); ok {
					if recv := fn.Type().(*types2.Signature).Recv(); recv != nil {
						if _, ptr := recv.Type().(*types2.Pointer); ptr {
							if v := rootVar(n.X, info); v != nil {
								get(v).addrTaken = true
							}
						}
					}
				}
			}
		case *ast.AssignStmt:
			for _, lhs := range n.Lhs {
				written[ast.Unparen(lhs)] = true
				if id, ok := lhs.(*ast.Ident); ok && n.Tok == token.DEFINE {
					if _, isDef := info.Defs[id]; isDef {
						continue
					}
				}
				if v := rootVar(lhs, info); v != nil {
					get(v).assigned = true
				}
			}
		case *ast.OrStmt:
			for _, lhs := range n.Lhs {
				written[ast.Unparen(lhs)] = true
				if id, ok := lhs.(*ast.Ident); ok && n.Tok == token.DEFINE {
					if _, isDef := info.Defs[id]; isDef {
						continue
					}
				}
				if v := rootVar(lhs, info); v != nil {
					get(v).assigned = true
				}
			}
		case *ast.IncDecStmt:
			if v := rootVar(n.X, info); v != nil {
				get(v).assigned = true
			}
		case *ast.RangeStmt:
			if tv, ok := info.Types[n.X]; ok && tv.Type != nil {
				if _, isFunc := tv.Type.Underlying().(*types2.Signature); isFunc || types2.RangesByMethod(tv.Type) {
					// The body of a range over a function, or over a
					// container's All method, is a yield function that
					// reaches the variables around it.
					bodies = append(bodies, n)
				}
			}
			if n.Tok == token.ASSIGN {
				for _, e := range []ast.Expr{n.Key, n.Value} {
					if e != nil {
						if v := rootVar(e, info); v != nil {
							get(v).assigned = true
						}
					}
				}
			}
		}
		return true
	})
	for _, b := range bodies {
		ast.Inspect(b, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			if v, ok := info.Uses[id].(*types2.Var); ok && !v.IsField() && (v.Pos() < b.Pos() || v.Pos() >= b.End()) {
				get(v).captured = true
			}
			return true
		})
	}
	return facts
}

// reinterpretedAddr returns the &x of a read *(*T)(unsafe.Pointer(&x)), or
// nil if e is not one.
func reinterpretedAddr(e *ast.StarExpr, info *types2.Info) *ast.UnaryExpr {
	conv, ok := ast.Unparen(e.X).(*ast.CallExpr)
	if !ok || len(conv.Args) != 1 || !info.Types[conv.Fun].IsType() {
		return nil
	}
	ptr, ok := ast.Unparen(conv.Args[0]).(*ast.CallExpr)
	if !ok || len(ptr.Args) != 1 || !info.Types[ptr.Fun].IsType() {
		return nil
	}
	if b, ok := info.Types[ptr.Fun].Type.Underlying().(*types2.Basic); !ok || b.Kind() != types2.UnsafePointer {
		return nil
	}
	amp, ok := ast.Unparen(ptr.Args[0]).(*ast.UnaryExpr)
	if !ok || amp.Op != token.AND {
		return nil
	}
	return amp
}

// rootVar returns the variable whose storage an expression denotes part
// of, following fields and array elements of values but not pointers, or
// nil.
func rootVar(e ast.Expr, info *types2.Info) *types2.Var {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
		case *ast.Ident:
			v, _ := info.Uses[x].(*types2.Var)
			if v == nil {
				v, _ = info.Defs[x].(*types2.Var)
			}
			return v
		case *ast.SelectorExpr:
			sel := info.Selections[x]
			if sel == nil || sel.Kind() != types2.FieldVal || sel.Indirect() {
				return nil
			}
			e = x.X
		case *ast.IndexExpr:
			tv, ok := info.Types[x.X]
			if !ok || tv.Type == nil {
				return nil
			}
			if _, isArr := tv.Type.Underlying().(*types2.Array); !isArr {
				return nil
			}
			e = x.X
		default:
			return nil
		}
	}
}

// stableVar reports whether nothing but the current function's own
// assignments can change local variable v while a statement runs: its
// address is not taken and no function literal refers to it. A variable
// captured from an enclosing function is never stable.
func (s *fnState) stableVar(v *types2.Var) bool {
	for _, c := range s.job.captures {
		if c == v {
			return false
		}
	}
	f := s.varFacts[v]
	return f == nil || !f.addrTaken && !f.captured
}

// ownsParam reports whether the function changes parameter v (or lets
// others change it), so that it must hold references of its own: then it
// retains the parameter on entry and releases it when it returns.
func (s *fnState) ownsParam(v *types2.Var) bool {
	f := s.varFacts[v]
	return f != nil && (f.assigned || f.addrTaken || f.captured)
}

// labelBlocks maps each label in a function body to the block or case
// clause that contains its statement, the scope a goto to it stays in.
func labelBlocks(body *ast.BlockStmt, info *types2.Info) map[*types2.Label]ast.Node {
	out := map[*types2.Label]ast.Node{}
	var walk func(n ast.Node, list []ast.Stmt)
	visit := func(st ast.Stmt) {
		ast.Inspect(st, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncLit:
				return false
			case *ast.BlockStmt:
				walk(n, n.List)
				return false
			case *ast.CaseClause:
				walk(n, n.Body)
				return false
			}
			return true
		})
	}
	walk = func(n ast.Node, list []ast.Stmt) {
		for _, st := range list {
			for {
				ls, ok := st.(*ast.LabeledStmt)
				if !ok {
					break
				}
				if l, ok := info.Defs[ls.Label].(*types2.Label); ok {
					out[l] = n
				}
				st = ls.Stmt
			}
			visit(st)
		}
	}
	walk(body, body.List)
	return out
}

// makesTemps reports whether evaluating e may make counted temporaries:
// it calls a function, converts, concatenates strings or builds a
// composite literal.
func (s *fnState) makesTemps(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			found = s.info.Types[n].Value.IsNil()
		case *ast.CompositeLit:
			found = true
		case *ast.BinaryExpr:
			if tv := s.info.Types[n]; tv.Value.IsNil() && tv.Type != nil && isString(tv.Type) {
				found = true
			}
		}
		return !found
	})
	return found
}

// releaseForGoto releases the entries a goto to label leaves, and in the
// block that holds the label, the variables declared after it, whose
// declarations run again.
func (s *fnState) releaseForGoto(label *types2.Label) {
	node := s.labelScopes[label]
	for i := len(s.cleanups) - 1; i >= 0; i-- {
		if e := s.cleanups[i]; e.node == node && e.scope {
			s.releaseAbove(i + 1)
			var after []countedSlot
			for _, c := range e.slots {
				if c.tmp == nil && c.pos >= label.Pos() {
					after = append(after, c)
				}
			}
			if s.b != nil {
				s.releaseSlots(after, true)
			}
			return
		}
	}
	s.l.internalf(s.pos, "goto target outside the enclosing blocks")
}
